package main

// End-to-end tests: a real httptest upstream behind the real reverse proxy,
// with faults armed through the control channel exactly as a user would arm
// them with curl. The store is fixed-seeded (newStore(1)) so the random faults
// (jitter/flaky/lossy) assert deterministically.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/alpaca"
	"github.com/mikefsq/goalpaca/client"
)

const jsonReply = `{"Value":42,"ClientTransactionID":1,"ServerTransactionID":1,"ErrorNumber":0,"ErrorMessage":""}`

// envelope mirrors the Alpaca JSON reply fields the faults touch.
type envelope struct {
	Value        json.RawMessage `json:"Value"`
	ErrorNumber  int             `json:"ErrorNumber"`
	ErrorMessage string          `json:"ErrorMessage"`
}

func jsonUpstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonReply)
	})
}

func binaryUpstream(body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/imagebytes")
		_, _ = w.Write(body)
	})
}

// newTestProxy starts upstream and the fault proxy in front of it, returning
// the proxy's base URL.
func newTestProxy(t *testing.T, upstream http.Handler) string {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	target, err := url.Parse(up.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	ps := httptest.NewServer(newFaultProxy(target, newStore(1)))
	t.Cleanup(ps.Close)
	return ps.URL
}

func ctlStatus(t *testing.T, base, path string) int {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func arm(t *testing.T, base, query string) {
	t.Helper()
	if code := ctlStatus(t, base, "/_ctl/set?"+query); code != http.StatusOK {
		t.Fatalf("arm %q: status %d", query, code)
	}
}

func getMember(t *testing.T, base, member string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(base + "/api/v1/telescope/0/" + member)
	if err != nil {
		t.Fatalf("GET %s: %v", member, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", member, err)
	}
	return resp, b
}

func getEnvelope(t *testing.T, base, member string) envelope {
	t.Helper()
	_, b := getMember(t, base, member)
	var e envelope
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("decode %s: %v (body %q)", member, err, b)
	}
	return e
}

func putForm(t *testing.T, base, member, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/api/v1/camera/0/"+member, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", member, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// expectBroken asserts the transfer fails: either the request errors outright
// (reset before headers) or the body cannot be read to the full length.
func expectBroken(t *testing.T, base, member string, full int) {
	t.Helper()
	c := &http.Client{Transport: &http.Transport{}}
	defer c.CloseIdleConnections()
	resp, err := c.Get(base + "/api/v1/camera/0/" + member)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err == nil && len(b) >= full {
		t.Fatalf("%s: full body delivered intact; want a broken transfer", member)
	}
}

func TestPassthrough(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	resp, b := getMember(t, base, "name")
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("Content-Type %q, want json", ct)
	}
	if string(b) != jsonReply {
		t.Errorf("body altered with no fault armed:\n got %s\nwant %s", b, jsonReply)
	}

	bin := make([]byte, 200)
	for i := range bin {
		bin[i] = byte(i)
	}
	bbase := newTestProxy(t, binaryUpstream(bin))
	resp, b = getMember(t, bbase, "imagearray")
	if resp.Header.Get("Content-Type") != "application/imagebytes" {
		t.Errorf("binary Content-Type %q", resp.Header.Get("Content-Type"))
	}
	if !bytes.Equal(b, bin) {
		t.Error("binary body altered with no fault armed")
	}
}

func TestJSONErrorFaults(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	cases := []struct {
		kind string
		num  int
		msg  string
	}{
		{"fail", alpaca.ErrNumDriverBase, "injected driver error"},
		{"notimpl", alpaca.ErrNumNotImplemented, "not implemented (injected)"},
		{"emptyerr", alpaca.ErrNumInvalidOperation, ""},
	}
	for _, c := range cases {
		member := "m" + c.kind
		arm(t, base, "fault="+c.kind+"&member="+member)
		e := getEnvelope(t, base, member)
		if e.ErrorNumber != c.num || e.ErrorMessage != c.msg {
			t.Errorf("%s: ErrorNumber %#x msg %q, want %#x %q", c.kind, e.ErrorNumber, e.ErrorMessage, c.num, c.msg)
		}
	}
	// An unarmed member is untouched.
	if e := getEnvelope(t, base, "name"); e.ErrorNumber != 0 {
		t.Errorf("unarmed member got ErrorNumber %#x", e.ErrorNumber)
	}
}

func TestValueOverride(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	cases := []struct{ raw, want string }{
		{"true", "true"},   // JSON bool passes through
		{"0.05", "0.05"},   // JSON number passes through
		{"east", `"east"`}, // bare word wrapped as a JSON string
	}
	for i, c := range cases {
		member := "v" + string(rune('a'+i))
		arm(t, base, "fault=value&member="+member+"&value="+c.raw)
		e := getEnvelope(t, base, member)
		if string(e.Value) != c.want {
			t.Errorf("value=%s: Value %s, want %s", c.raw, e.Value, c.want)
		}
		if e.ErrorNumber != 0 {
			t.Errorf("value=%s: ErrorNumber %#x, want 0", c.raw, e.ErrorNumber)
		}
	}
}

func TestNoValueAndMalform(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())

	arm(t, base, "fault=novalue&member=connected")
	e := getEnvelope(t, base, "connected")
	if e.Value != nil {
		t.Errorf("novalue: Value %s still present", e.Value)
	}
	if e.ErrorNumber != 0 {
		t.Errorf("novalue: ErrorNumber %#x, want 0 (only the key is dropped)", e.ErrorNumber)
	}

	arm(t, base, "fault=malform&member=slewing")
	resp, b := getMember(t, base, "slewing")
	if json.Valid(b) {
		t.Errorf("malform: body still valid JSON: %s", b)
	}
	if resp.ContentLength != int64(len(b)) {
		t.Errorf("malform: Content-Length %d, body %d", resp.ContentLength, len(b))
	}
}

func TestContentTypeOverride(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=contenttype&member=connected&value=text/csv")
	resp, b := getMember(t, base, "connected")
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv" {
		t.Errorf("Content-Type %q, want text/csv", ct)
	}
	if string(b) != jsonReply {
		t.Error("contenttype must not alter the body")
	}
	arm(t, base, "fault=contenttype&member=name") // no value -> default
	resp, _ = getMember(t, base, "name")
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("default Content-Type %q, want text/plain", ct)
	}
}

func TestHTTPStatusFault(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=http&member=imageready&value=503")
	resp, _ := getMember(t, base, "imageready")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", resp.StatusCode)
	}
	arm(t, base, "fault=http&member=name&value=99") // out of range -> 500
	resp, _ = getMember(t, base, "name")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("out-of-range code: status %d, want 500", resp.StatusCode)
	}
}

func TestHang(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=hang&member=slewing")
	c := &http.Client{Timeout: 150 * time.Millisecond}
	if _, err := c.Get(base + "/api/v1/telescope/0/slewing"); err == nil {
		t.Fatal("hang: request completed, want client timeout")
	}
	// Other members are unaffected while one hangs.
	if e := getEnvelope(t, base, "name"); e.ErrorNumber != 0 {
		t.Errorf("unarmed member affected by hang: %#x", e.ErrorNumber)
	}
}

func TestLatencyAndJitter(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=latency&value=80")
	start := time.Now()
	getMember(t, base, "name")
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Errorf("latency: request took %v, want >= 80ms", d)
	}

	arm(t, base, "fault=latency&value=0")
	arm(t, base, "fault=jitter&value=30-60")
	start = time.Now()
	getMember(t, base, "name")
	if d := time.Since(start); d < 30*time.Millisecond {
		t.Errorf("jitter: request took %v, want >= 30ms", d)
	}
}

func TestDropAndLossy(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=drop")
	c := &http.Client{Transport: &http.Transport{}}
	defer c.CloseIdleConnections()
	if _, err := c.Get(base + "/api/v1/telescope/0/name"); err == nil {
		t.Fatal("drop: request succeeded, want connection error")
	}
	// The control channel is exempt from every fault.
	if code := ctlStatus(t, base, "/_ctl/list"); code != http.StatusOK {
		t.Fatalf("control channel affected by drop: %d", code)
	}
	if code := ctlStatus(t, base, "/_ctl/clear"); code != http.StatusOK {
		t.Fatalf("clear: %d", code)
	}
	if e := getEnvelope(t, base, "name"); e.ErrorNumber != 0 {
		t.Errorf("after clear: ErrorNumber %#x", e.ErrorNumber)
	}

	arm(t, base, "fault=lossy&value=100")
	if _, err := c.Get(base + "/api/v1/telescope/0/name"); err == nil {
		t.Fatal("lossy=100: request succeeded, want connection error")
	}
	arm(t, base, "fault=lossy&value=0")
	if e := getEnvelope(t, base, "name"); e.ErrorNumber != 0 {
		t.Errorf("lossy=0: ErrorNumber %#x", e.ErrorNumber)
	}
}

func TestSwapBin(t *testing.T) {
	var mu capturedForms
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.set(lastSegment(r.URL.Path), r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonReply)
	})
	base := newTestProxy(t, up)

	putForm(t, base, "binx", "BinX=1&ClientID=7")
	if got := mu.get("binx").Get("BinX"); got != "1" {
		t.Errorf("pre-arm BinX = %q, want 1 (passthrough)", got)
	}

	arm(t, base, "fault=swapbin")
	putForm(t, base, "binx", "BinX=1&ClientID=7")
	if f := mu.get("binx"); f.Get("BinX") != "2" || f.Get("ClientID") != "7" {
		t.Errorf("swapbin binx: BinX=%q ClientID=%q, want 2 and 7", f.Get("BinX"), f.Get("ClientID"))
	}
	putForm(t, base, "biny", "BinY=2")
	if got := mu.get("biny").Get("BinY"); got != "1" {
		t.Errorf("swapbin biny: BinY = %q, want 1", got)
	}
	// Only binx/biny PUTs are rewritten.
	putForm(t, base, "startexposure", "BinX=1&Duration=2")
	if got := mu.get("startexposure").Get("BinX"); got != "1" {
		t.Errorf("swapbin startexposure: BinX = %q, want 1 (untouched)", got)
	}
}

func TestByteFaults(t *testing.T) {
	orig := make([]byte, 200)
	for i := range orig {
		orig[i] = byte(i)
	}
	base := newTestProxy(t, binaryUpstream(orig))

	arm(t, base, "fault=truncate&member=trunc&value=50")
	resp, b := getMember(t, base, "trunc")
	if len(b) != 100 || !bytes.Equal(b, orig[:100]) || resp.ContentLength != 100 {
		t.Errorf("truncate 50%%: got %d bytes (Content-Length %d), want the leading 100", len(b), resp.ContentLength)
	}

	arm(t, base, "fault=inject&member=spliced&value=10")
	_, b = getMember(t, base, "spliced")
	switch {
	case len(b) != 220:
		t.Errorf("inject 10%%: got %d bytes, want 220", len(b))
	case !bytes.Equal(b[:100], orig[:100]) || !bytes.Equal(b[120:], orig[100:]):
		t.Error("inject: surrounding bytes altered")
	default:
		for _, c := range b[100:120] {
			if c != 0xEE {
				t.Error("inject: junk region not 0xEE")
				break
			}
		}
	}

	arm(t, base, "fault=corrupthead&member=head&value=44")
	_, b = getMember(t, base, "head")
	for i, c := range b {
		want := orig[i]
		if i < 44 {
			want ^= 0xFF
		}
		if c != want {
			t.Fatalf("corrupthead: byte %d = %#x, want %#x", i, c, want)
		}
	}

	arm(t, base, "fault=corrupttail&member=tail&value=64")
	_, b = getMember(t, base, "tail")
	for i, c := range b {
		want := orig[i]
		if i >= len(orig)-64 {
			want ^= 0xFF
		}
		if c != want {
			t.Fatalf("corrupttail: byte %d = %#x, want %#x", i, c, want)
		}
	}

	// JSON faults must not touch a non-JSON body.
	arm(t, base, "fault=fail&member=binfail")
	_, b = getMember(t, base, "binfail")
	if !bytes.Equal(b, orig) {
		t.Error("JSON fault mutated a binary body")
	}
}

func TestImgFieldAndPixels(t *testing.T) {
	// Synthetic ImageBytes reply: 44-byte header (version 1, dataStart 44)
	// followed by 16 pixel bytes of 0x11.
	frame := make([]byte, 60)
	binary.LittleEndian.PutUint32(frame[0:], 1)
	binary.LittleEndian.PutUint32(frame[16:], 44)
	for i := 44; i < 60; i++ {
		frame[i] = 0x11
	}
	base := newTestProxy(t, binaryUpstream(frame))

	arm(t, base, "fault=imgfield&member=imagearray&value=datastart:-16")
	_, b := getMember(t, base, "imagearray")
	if got := int32(binary.LittleEndian.Uint32(b[16:])); got != -16 {
		t.Errorf("imgfield datastart = %d, want -16", got)
	}
	if binary.LittleEndian.Uint32(b[0:]) != 1 || !bytes.Equal(b[44:], frame[44:]) {
		t.Error("imgfield touched bytes outside the target field")
	}

	arm(t, base, "fault=pixels&member=imagearray&value=sat")
	_, b = getMember(t, base, "imagearray")
	if !bytes.Equal(b[:44], frame[:44]) {
		t.Error("pixels=sat mutated the header")
	}
	for i := 44; i < len(b); i++ {
		if b[i] != 0xFF {
			t.Fatalf("pixels=sat: byte %d = %#x, want 0xFF", i, b[i])
		}
	}

	arm(t, base, "fault=pixels&member=imagearray&value=zero")
	_, b = getMember(t, base, "imagearray")
	for i := 44; i < len(b); i++ {
		if b[i] != 0 {
			t.Fatalf("pixels=zero: byte %d = %#x, want 0", i, b[i])
		}
	}
}

func TestSwapDims(t *testing.T) {
	// A 2x3 (w=2, h=3) UInt16 ImageBytes frame, column-major (y fastest): the pixel at
	// (x,y) holds x*10+y. After swapdims the client should get a 3x2 frame with the
	// pixels transposed and the dim1/dim2 header fields exchanged.
	frame := make([]byte, 44+2*6)
	binary.LittleEndian.PutUint32(frame[0:], 1)   // metadata version
	binary.LittleEndian.PutUint32(frame[16:], 44) // dataStart
	binary.LittleEndian.PutUint32(frame[24:], 8)  // txtype = UInt16
	binary.LittleEndian.PutUint32(frame[28:], 2)  // rank
	binary.LittleEndian.PutUint32(frame[32:], 2)  // dim1 (w)
	binary.LittleEndian.PutUint32(frame[36:], 3)  // dim2 (h)
	for x := 0; x < 2; x++ {
		for y := 0; y < 3; y++ {
			binary.LittleEndian.PutUint16(frame[44+2*(x*3+y):], uint16(x*10+y))
		}
	}
	base := newTestProxy(t, binaryUpstream(frame))
	arm(t, base, "fault=swapdims")
	_, b := getMember(t, base, "imagearray")
	if d1, d2 := binary.LittleEndian.Uint32(b[32:]), binary.LittleEndian.Uint32(b[36:]); d1 != 3 || d2 != 2 {
		t.Fatalf("swapdims header dims = %dx%d, want 3x2", d1, d2)
	}
	want := []uint16{0, 10, 1, 11, 2, 12} // transposed column-major (new y=w fastest)
	for i, wv := range want {
		if got := binary.LittleEndian.Uint16(b[44+2*i:]); got != wv {
			t.Errorf("swapdims pixel %d = %d, want %d", i, got, wv)
		}
	}
}

func TestForceJSON(t *testing.T) {
	// An upstream that mimics a real Alpaca camera: ImageBytes when the client asks for
	// it, JSON ImageArray otherwise. forcejson should strip the Accept header so the
	// client's request reaches the upstream as a JSON one.
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "imagebytes") {
			w.Header().Set("Content-Type", "application/imagebytes")
			_, _ = w.Write([]byte("BINARYFRAME"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Value":[[1,2],[3,4]],"Rank":2,"ErrorNumber":0}`)
	})
	base := newTestProxy(t, up)

	// Pre-arm: an ImageBytes Accept gets the binary transport through untouched.
	req, _ := http.NewRequest("GET", base+"/api/v1/camera/0/imagearray", nil)
	req.Header.Set("Accept", "application/imagebytes")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pre-arm GET: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Type"), "imagebytes") || string(b) != "BINARYFRAME" {
		t.Fatalf("pre-arm: got %q (%s), want the binary frame", b, resp.Header.Get("Content-Type"))
	}

	// Armed: the same request comes back as JSON because forcejson dropped the Accept.
	arm(t, base, "fault=forcejson")
	req, _ = http.NewRequest("GET", base+"/api/v1/camera/0/imagearray", nil)
	req.Header.Set("Accept", "application/imagebytes")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("armed GET: %v", err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Type"), "json") || !strings.Contains(string(b), "Value") {
		t.Fatalf("forcejson: got %q (%s), want JSON ImageArray", b, resp.Header.Get("Content-Type"))
	}
}

func TestPartialDrop(t *testing.T) {
	big := make([]byte, 4096)
	base := newTestProxy(t, binaryUpstream(big))
	arm(t, base, "fault=partial-drop&member=imagearray&value=40")
	expectBroken(t, base, "imagearray", len(big))
}

func TestDropack(t *testing.T) {
	var hits atomic.Int32
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonReply)
	})
	base := newTestProxy(t, up)
	arm(t, base, "fault=dropack&member=pulseguide")
	expectBroken(t, base, "pulseguide", len(jsonReply))
	if hits.Load() != 1 {
		t.Errorf("dropack: upstream hit %d times, want exactly 1 (the request must execute)", hits.Load())
	}
}

func TestThrottle(t *testing.T) {
	body := make([]byte, 300)
	for i := range body {
		body[i] = byte(i)
	}
	base := newTestProxy(t, binaryUpstream(body))
	arm(t, base, "fault=throttle&member=imagearray&value=1000") // ~0.3s for 300 bytes
	start := time.Now()
	_, b := getMember(t, base, "imagearray")
	if d := time.Since(start); d < 250*time.Millisecond {
		t.Errorf("throttle: transfer took %v, want >= 250ms", d)
	}
	if !bytes.Equal(b, body) {
		t.Error("throttle corrupted the body (must only slow it down)")
	}
}

func TestFailFirst(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=failfirst&member=connected&value=2")
	for i := 1; i <= 4; i++ {
		e := getEnvelope(t, base, "connected")
		wantErr := i <= 2
		if gotErr := e.ErrorNumber != 0; gotErr != wantErr {
			t.Errorf("failfirst request %d: error=%v, want %v", i, gotErr, wantErr)
		}
	}
}

func TestEveryNth(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=everynth&member=slewing&value=3")
	for i := 1; i <= 6; i++ {
		e := getEnvelope(t, base, "slewing")
		wantErr := i%3 == 0
		if gotErr := e.ErrorNumber != 0; gotErr != wantErr {
			t.Errorf("everynth request %d: error=%v, want %v", i, gotErr, wantErr)
		}
	}
}

func TestFlaky(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=flaky&member=slewing&value=50")
	errs := 0
	for i := 0; i < 20; i++ {
		if getEnvelope(t, base, "slewing").ErrorNumber != 0 {
			errs++
		}
	}
	// Seeded PRNG: the exact count is stable, but assert only the property.
	if errs == 0 || errs == 20 {
		t.Errorf("flaky=50: %d/20 errors, want intermittent", errs)
	}
}

func TestControlValidation(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	bad := []string{
		"fault=bogus&member=x",
		"fault=fail",    // member faults need member=
		"fault=hang",    // hang needs member=
		"fault=dropack", // dropack needs member=
		"fault=contenttype",
		"fault=latency&value=abc",
		"fault=latency&value=-5",
		"fault=truncate&member=x&value=150",
		"fault=partial-drop&member=x&value=101",
		"fault=corrupthead&member=x&value=-1",
		"fault=imgfield&member=x&value=bogusfield:1",
		"fault=imgfield&member=x&value=datastart", // missing :int
		"fault=imgfield&member=x&value=datastart:xyz",
		"fault=pixels&member=x&value=purple",
		"fault=throttle&member=x&value=0",
		"fault=flaky&member=x&value=101",
		"fault=failfirst&member=x&value=0",
		"fault=everynth&member=x", // missing value
		"fault=jitter&value=50",   // needs min-max
		"fault=jitter&value=60-50",
		"fault=lossy&value=101",
		"fault=http&member=x&value=abc",
	}
	for _, q := range bad {
		if code := ctlStatus(t, base, "/_ctl/set?"+q); code != http.StatusBadRequest {
			t.Errorf("set?%s: status %d, want 400", q, code)
		}
	}
	if code := ctlStatus(t, base, "/_ctl/frobnicate"); code != http.StatusBadRequest {
		t.Errorf("unknown action: status %d, want 400", code)
	}
}

type ctlSnapshot struct {
	MemberFaults map[string]string `json:"member_faults"`
	LatencyMS    int64             `json:"latency_ms"`
	Drop         bool              `json:"drop"`
	SwapBin      bool              `json:"swap_bin"`
	JitterMS     []int64           `json:"jitter_ms"`
	LossyPct     int               `json:"lossy_pct"`
	Chaos        bool              `json:"chaos"`
}

func list(t *testing.T, base string) ctlSnapshot {
	t.Helper()
	resp, err := http.Get(base + "/_ctl/list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	var s ctlSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return s
}

func TestControlListAndClear(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=fail&member=slewing")
	arm(t, base, "fault=value&member=connected&value=0.05")
	arm(t, base, "fault=hang&member=pulseguide")
	arm(t, base, "fault=latency&value=5")
	arm(t, base, "fault=jitter&value=10-20")
	arm(t, base, "fault=lossy&value=5")
	arm(t, base, "fault=chaos")
	arm(t, base, "fault=swapbin")

	s := list(t, base)
	if s.MemberFaults["slewing"] != "fail" || s.MemberFaults["connected"] != "value(0.05)" {
		t.Errorf("member_faults = %v", s.MemberFaults)
	}
	if s.MemberFaults["pulseguide"] != "hang" {
		t.Errorf("hang not listed as a member fault: %v", s.MemberFaults)
	}
	if s.LatencyMS != 5 || len(s.JitterMS) != 2 || s.JitterMS[0] != 10 || s.JitterMS[1] != 20 ||
		s.LossyPct != 5 || !s.Chaos || !s.SwapBin {
		t.Errorf("globals = %+v", s)
	}

	// Clearing one member leaves everything else armed.
	if code := ctlStatus(t, base, "/_ctl/clear?member=slewing"); code != http.StatusOK {
		t.Fatalf("clear member: %d", code)
	}
	s = list(t, base)
	if _, ok := s.MemberFaults["slewing"]; ok {
		t.Error("clear?member=slewing left the fault armed")
	}
	if _, ok := s.MemberFaults["connected"]; !ok {
		t.Error("clear?member=slewing removed another member's fault")
	}
	if s.LatencyMS != 5 || !s.Chaos {
		t.Error("clear?member= touched the globals")
	}

	// Full clear resets every fault and counter.
	if code := ctlStatus(t, base, "/_ctl/clear"); code != http.StatusOK {
		t.Fatalf("clear: %d", code)
	}
	s = list(t, base)
	if len(s.MemberFaults) != 0 || s.LatencyMS != 0 || s.Drop || s.SwapBin ||
		s.JitterMS[0] != 0 || s.JitterMS[1] != 0 || s.LossyPct != 0 || s.Chaos {
		t.Errorf("clear left state armed: %+v", s)
	}
	if e := getEnvelope(t, base, "name"); e.ErrorNumber != 0 || string(e.Value) != "42" {
		t.Errorf("after clear: Value %s ErrorNumber %#x", e.Value, e.ErrorNumber)
	}
}

func TestAdvertiseResponder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	conn := pc.(*net.UDPConn)
	addr := conn.LocalAddr().(*net.UDPAddr)
	go serveAdvertise(ctx, conn, 11599)

	probe, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer probe.Close()

	// A non-probe datagram draws no reply.
	_, _ = probe.Write([]byte("not-a-probe"))
	_ = probe.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, err := probe.Read(make([]byte, 64)); err == nil {
		t.Error("responder replied to a non-discovery datagram")
	}

	// A discovery probe draws the advertised port.
	if _, err := probe.Write([]byte("alpacadiscovery1")); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	_ = probe.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 256)
	n, err := probe.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var got struct {
		AlpacaPort int `json:"AlpacaPort"`
	}
	if err := json.Unmarshal(buf[:n], &got); err != nil {
		t.Fatalf("decode %q: %v", buf[:n], err)
	}
	if got.AlpacaPort != 11599 {
		t.Errorf("advertised AlpacaPort = %d, want 11599", got.AlpacaPort)
	}
}

func TestChooseServer(t *testing.T) {
	loopback := net.IPv4(127, 0, 0, 1)
	lan := net.IPv4(10, 0, 1, 20)
	srv := func(ip net.IP, port int) client.DiscoveredServer {
		return client.DiscoveredServer{IP: ip, AlpacaPort: port, Address: net.JoinHostPort(ip.String(), strconv.Itoa(port))}
	}
	// devices returns a fixed device type for a given address.
	devicesReturning := func(byAddr map[string]string) func(client.DiscoveredServer) ([]client.ConfiguredDevice, error) {
		return func(s client.DiscoveredServer) ([]client.ConfiguredDevice, error) {
			if t := byAddr[s.Address]; t != "" {
				return []client.ConfiguredDevice{{DeviceType: t, DeviceName: t + "-sim"}}, nil
			}
			return nil, nil
		}
	}

	t.Run("excludes own loopback port then picks first", func(t *testing.T) {
		self := srv(loopback, 11510)
		up := srv(lan, 11114)
		got, err := chooseServer([]client.DiscoveredServer{self, up}, "", 11510, devicesReturning(nil))
		if err != nil || got.Address != up.Address {
			t.Fatalf("got %v, err %v; want %s", got.Address, err, up.Address)
		}
	})

	t.Run("a LAN server on the proxy's port is not excluded", func(t *testing.T) {
		up := srv(lan, 11510) // same port, but not loopback -> a real remote upstream
		got, err := chooseServer([]client.DiscoveredServer{up}, "", 11510, devicesReturning(nil))
		if err != nil || got.Address != up.Address {
			t.Fatalf("got %v, err %v; want %s", got.Address, err, up.Address)
		}
	})

	t.Run("nothing left after excluding self", func(t *testing.T) {
		self := srv(loopback, 11510)
		if _, err := chooseServer([]client.DiscoveredServer{self}, "", 11510, devicesReturning(nil)); err == nil {
			t.Fatal("want an error when only the proxy itself is discovered")
		}
	})

	t.Run("match selects the server hosting that device type", func(t *testing.T) {
		mount := srv(lan, 11110)
		cam := srv(lan, 11114)
		devs := devicesReturning(map[string]string{mount.Address: "Telescope", cam.Address: "Camera"})
		got, err := chooseServer([]client.DiscoveredServer{mount, cam}, "camera", 11510, devs)
		if err != nil || got.Address != cam.Address {
			t.Fatalf("got %v, err %v; want the camera server %s", got.Address, err, cam.Address)
		}
	})

	t.Run("match with no hosting server errors", func(t *testing.T) {
		mount := srv(lan, 11110)
		devs := devicesReturning(map[string]string{mount.Address: "Telescope"})
		if _, err := chooseServer([]client.DiscoveredServer{mount}, "camera", 11510, devs); err == nil {
			t.Fatal("want an error when no server hosts the matched type")
		}
	})
}

func TestResolveUpstreamNoDiscover(t *testing.T) {
	u, err := resolveUpstream(context.Background(), false, "10.0.1.20:11114", "", 11510, time.Second)
	if err != nil {
		t.Fatalf("resolveUpstream: %v", err)
	}
	if u.Host != "10.0.1.20:11114" || u.Scheme != "http" {
		t.Errorf("upstream = %s, want http://10.0.1.20:11114", u)
	}
}

func TestLastSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api/v1/telescope/0/slewing", "slewing"},
		{"/api/v1/telescope/0/slewing/", "slewing"},
		{"/api/v1/camera/0/ImageArray", "imagearray"}, // lowercased for case-insensitive matching
		{"/management/apiversions", "apiversions"},
		{"slewing", "slewing"},
		{"", ""},
	}
	for _, c := range cases {
		if got := lastSegment(c.in); got != c.want {
			t.Errorf("lastSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMemberCaseInsensitive(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=fail&member=slewing")
	resp, err := http.Get(base + "/api/v1/telescope/0/Slewing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var e envelope
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e.ErrorNumber != alpaca.ErrNumDriverBase {
		t.Errorf("mixed-case member: ErrorNumber %#x, want the injected fault", e.ErrorNumber)
	}
}

func TestMethodScoping(t *testing.T) {
	var puts atomic.Int32
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonReply)
	})
	base := newTestProxy(t, up)

	arm(t, base, "fault=fail&member=cooleron&method=PUT")
	// GET is healthy...
	if e := getEnvelope(t, base, "cooleron"); e.ErrorNumber != 0 {
		t.Errorf("GET cooleron: ErrorNumber %#x, want 0 (PUT-scoped fault)", e.ErrorNumber)
	}
	// ...PUT fails.
	putForm(t, base, "cooleron", "CoolerOn=true")
	// (verified via the response below rather than the upstream, which still runs)

	// hang scoped to PUT: GET passes, PUT hangs.
	arm(t, base, "fault=hang&member=slewing&method=PUT")
	if e := getEnvelope(t, base, "slewing"); e.ErrorNumber != 0 {
		t.Errorf("GET slewing under PUT-scoped hang: ErrorNumber %#x, want 0", e.ErrorNumber)
	}
	c := &http.Client{Timeout: 150 * time.Millisecond}
	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/telescope/0/slewing", strings.NewReader(""))
	if _, err := c.Do(req); err == nil {
		t.Error("PUT slewing under PUT-scoped hang: completed, want timeout")
	}

	// method= on a global fault is rejected (nothing to scope).
	if code := ctlStatus(t, base, "/_ctl/set?fault=drop&method=PUT"); code != http.StatusBadRequest {
		t.Errorf("method= on a global fault: status %d, want 400", code)
	}
}

func TestMethodScopedPutFails(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=fail&member=cooleron&method=PUT")
	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/camera/0/cooleron", strings.NewReader("CoolerOn=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	var e envelope
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e.ErrorNumber != alpaca.ErrNumDriverBase {
		t.Errorf("PUT cooleron: ErrorNumber %#x, want the injected fault", e.ErrorNumber)
	}
}

func TestManagementExcluded(t *testing.T) {
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonReply)
	})
	base := newTestProxy(t, up)
	arm(t, base, "fault=novalue&member=description")

	// The device member is faulted...
	if e := getEnvelope(t, base, "description"); e.Value != nil {
		t.Errorf("device description: Value %s still present", e.Value)
	}
	// ...but the same-named management endpoint is not.
	resp, err := http.Get(base + "/management/v1/description")
	if err != nil {
		t.Fatalf("GET management: %v", err)
	}
	defer resp.Body.Close()
	var e envelope
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e.Value == nil {
		t.Error("management description lost its Value — member fault leaked past /api/")
	}
}

func TestChaosRespectsExplicitZero(t *testing.T) {
	base := newTestProxy(t, jsonUpstream())
	arm(t, base, "fault=chaos")
	arm(t, base, "fault=lossy&value=0")
	arm(t, base, "fault=jitter&value=0-0")
	c := &http.Client{Transport: &http.Transport{}}
	defer c.CloseIdleConnections()
	for i := 0; i < 40; i++ {
		resp, err := c.Get(base + "/api/v1/telescope/0/name")
		if err != nil {
			t.Fatalf("chaos+lossy=0 reset a connection on request %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func TestChaosDefersToExplicitFault(t *testing.T) {
	frame := make([]byte, 400)
	for i := range frame {
		frame[i] = byte(i)
	}
	base := newTestProxy(t, binaryUpstream(frame))
	arm(t, base, "fault=chaos")
	arm(t, base, "fault=lossy&value=0")
	arm(t, base, "fault=jitter&value=0-0")
	arm(t, base, "fault=truncate&member=imagearray&value=50")
	// Every frame must be the deterministic 50% truncation, not a random chaos drop.
	for i := 0; i < 30; i++ {
		_, b := getMember(t, base, "imagearray")
		if len(b) != 200 {
			t.Fatalf("request %d: got %d bytes, want the explicit 200-byte truncation", i, len(b))
		}
	}
}

func TestContentFamilyMismatchNoOp(t *testing.T) {
	frame := make([]byte, 100)
	for i := range frame {
		frame[i] = byte(i)
	}
	base := newTestProxy(t, binaryUpstream(frame))
	arm(t, base, "fault=flaky&member=imagearray&value=100") // JSON fault on binary member
	_, b := getMember(t, base, "imagearray")
	if !bytes.Equal(b, frame) {
		t.Error("JSON fault on a binary member altered the frame; want an untouched no-op")
	}
}

func TestGlobalDisarm(t *testing.T) {
	var mu capturedForms
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.set(lastSegment(r.URL.Path), r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonReply)
	})
	base := newTestProxy(t, up)

	arm(t, base, "fault=swapbin")
	if !list(t, base).SwapBin {
		t.Fatal("swapbin did not arm")
	}
	arm(t, base, "fault=swapbin&value=off")
	if list(t, base).SwapBin {
		t.Error("swapbin&value=off did not disarm")
	}
	putForm(t, base, "binx", "BinX=1")
	if got := mu.get("binx").Get("BinX"); got != "1" {
		t.Errorf("after disarm BinX = %q, want 1 (passthrough)", got)
	}
}

func TestPartialDropFull(t *testing.T) {
	big := make([]byte, 4096)
	base := newTestProxy(t, binaryUpstream(big))
	arm(t, base, "fault=partial-drop&member=imagearray&value=100")
	expectBroken(t, base, "imagearray", len(big))
}

// capturedForms is a mutex-guarded member->form map for upstream capture.
type capturedForms struct {
	mu    sync.Mutex
	forms map[string]url.Values
}

func (c *capturedForms) set(k string, v url.Values) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.forms == nil {
		c.forms = map[string]url.Values{}
	}
	c.forms[k] = v
}

func (c *capturedForms) get(k string) url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.forms[k]
}
