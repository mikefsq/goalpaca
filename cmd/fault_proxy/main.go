// Command fault_proxy is a transparent Alpaca HTTP reverse proxy that injects
// faults into individual device members at runtime, so an Alpaca client's error
// and recovery paths can be exercised on demand against an otherwise-correct sim.
//
// Point the client's device address at the proxy (host:port of -listen) and the
// proxy forwards every request to -upstream unchanged -- including the binary
// ImageBytes camera transport -- until a fault is armed via the control channel:
//
//	# invalid guide rate (client should reject it and alert):
//	curl 'localhost:11510/_ctl/set?fault=value&member=guideraterightascension&value=0.05'
//	# force a slew during guiding:
//	curl 'localhost:11510/_ctl/set?fault=value&member=slewing&value=true'
//	# fail a pulse guide:
//	curl 'localhost:11510/_ctl/set?fault=fail&member=pulseguide'
//	# report a member as not implemented (tests capability probes):
//	curl 'localhost:11510/_ctl/set?fault=notimpl&member=ispulseguiding'
//	# device error with a blank ErrorMessage (tests error-number synthesis):
//	curl 'localhost:11510/_ctl/set?fault=emptyerr&member=startexposure'
//	# HTTP 500 on a member:
//	curl 'localhost:11510/_ctl/set?fault=http&member=imageready&value=500'
//	# never respond to a member (tests client timeout / cancel):
//	curl 'localhost:11510/_ctl/set?fault=hang&member=imagearray'
//	# scope a member fault to one verb (fail the write, keep the status read):
//	curl 'localhost:11510/_ctl/set?fault=fail&member=cooleron&method=PUT'
//	# global request latency (ms) and hard connection drop:
//	curl 'localhost:11510/_ctl/set?fault=latency&value=35000'
//	curl 'localhost:11510/_ctl/set?fault=drop'
//	# binary faults on the camera frame (and swap the requested binning):
//	curl 'localhost:11511/_ctl/set?fault=swapbin'                             # flip BinX/BinY 1<->2 on the PUT
//	curl 'localhost:11511/_ctl/set?fault=truncate&member=imagearray&value=90' # send 90% of the frame
//	curl 'localhost:11511/_ctl/set?fault=inject&member=imagearray&value=10'   # splice 10% junk into the middle
//	curl 'localhost:11511/_ctl/set?fault=corrupthead&member=imagearray&value=44' # corrupt the ImageBytes header
//	curl 'localhost:11511/_ctl/set?fault=corrupttail&member=imagearray&value=64' # corrupt the frame tail
//	# set an exact ImageBytes header field (deterministic decode-branch coverage):
//	curl 'localhost:11511/_ctl/set?fault=imgfield&member=imagearray&value=datastart:-16'
//	curl 'localhost:11511/_ctl/set?fault=pixels&member=imagearray&value=sat'  # saturate the frame
//	curl 'localhost:11511/_ctl/set?fault=swapdims'                           # transpose the frame (axes flipped)
//	curl 'localhost:11511/_ctl/set?fault=forcejson'                         # strip ImageBytes Accept -> JSON ImageArray
//	# transport / structure faults:
//	curl 'localhost:11511/_ctl/set?fault=partial-drop&member=imagearray&value=40' # deliver 40% then RST
//	curl 'localhost:11510/_ctl/set?fault=dropack&member=pulseguide'  # sim runs it, client loses the ack
//	curl 'localhost:11511/_ctl/set?fault=contenttype&member=imagearray&value=text/plain'
//	curl 'localhost:11510/_ctl/set?fault=malform&member=slewing'    # unparseable JSON
//	curl 'localhost:11510/_ctl/set?fault=novalue&member=slewing'    # drop the Value key
//	curl 'localhost:11511/_ctl/set?fault=throttle&member=imagearray&value=20000' # 20 KB/s slow drip
//	# realistic degraded network (random; use -seed to replay):
//	curl 'localhost:11510/_ctl/set?fault=jitter&value=50-800'      # per-request delay in [50,800] ms
//	curl 'localhost:11510/_ctl/set?fault=flaky&member=slewing&value=25' # 25% of reads error
//	curl 'localhost:11510/_ctl/set?fault=lossy&value=10'           # 10% of requests RST
//	curl 'localhost:11510/_ctl/set?fault=chaos'                    # jitter + lossy + partial frame drops
//	# reproducible transient patterns:
//	curl 'localhost:11510/_ctl/set?fault=failfirst&member=connected&value=2' # fail first 2, then succeed
//	curl 'localhost:11510/_ctl/set?fault=everynth&member=slewing&value=5'    # fail every 5th read
//	# inspect / clear:
//	curl 'localhost:11510/_ctl/list'
//	curl 'localhost:11510/_ctl/clear'                        # all
//	curl 'localhost:11510/_ctl/clear?member=slewing'         # one member
//	curl 'localhost:11510/_ctl/set?fault=swapbin&value=off'  # disarm one global toggle
//
// Run one proxy per upstream device (a camera and a mount are separate servers):
//
//	fault_proxy -listen :11510 -upstream 127.0.0.1:11110    # mount
//	fault_proxy -listen :11511 -upstream 10.0.1.20:11114    # camera
//	fault_proxy -listen :11511 -discover -discover-match camera  # find the upstream by discovery
//
// With -advertise the proxy also answers Alpaca UDP discovery (:32227) with its
// own port, co-binding via SO_REUSEPORT so it appears in a Discover list next to
// the real device -- pick the proxy's port to route a session through the faults.
// With -discover it finds its own upstream by discovery instead of -upstream
// (optionally -discover-match <type> to choose among several servers).
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mikefsq/goalpaca/alpaca"
	"github.com/mikefsq/goalpaca/client"
	"github.com/mikefsq/goalpaca/server"
)

// alpacaDiscoveryPort is the fixed ASCOM Alpaca UDP discovery port.
const alpacaDiscoveryPort = 32227

// runAdvertise answers Alpaca UDP discovery with the proxy's own HTTP port, so
// the proxy shows up in a discovery tool as a selectable server. It co-binds the
// discovery port via SO_REUSEPORT (server.ReuseControl) rather than owning it
// exclusively, so the upstream device's own discovery responder keeps working and
// both appear in the list -- the operator picks the proxy's port to route a
// session through the fault injector, or the real port to bypass it.
//
// Caveat: SO_REUSEPORT delivers a broadcast probe to every co-bound responder (so
// both answer), but a directed unicast probe reaches only one of them.
func runAdvertise(ctx context.Context, alpacaPort int) {
	lc := net.ListenConfig{Control: server.ReuseControl}
	pc, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf("0.0.0.0:%d", alpacaDiscoveryPort))
	if err != nil {
		log.Printf("advertise: discovery listen failed: %v", err)
		return
	}
	serveAdvertise(ctx, pc.(*net.UDPConn), alpacaPort)
}

// serveAdvertise replies to Alpaca discovery probes on conn with alpacaPort until
// ctx is cancelled, then closes conn.
func serveAdvertise(ctx context.Context, conn *net.UDPConn, alpacaPort int) {
	go func() { <-ctx.Done(); _ = conn.Close() }()

	resp, _ := json.Marshal(struct {
		AlpacaPort int `json:"AlpacaPort"`
	}{alpacaPort})
	buf := make([]byte, 1024)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
			time.Sleep(100 * time.Millisecond) // don't busy-spin on a broken socket
			continue
		}
		if strings.HasPrefix(strings.ToLower(string(buf[:n])), "alpacadiscovery") {
			_, _ = conn.WriteToUDP(resp, from)
		}
	}
}

// imgFields maps an ImageBytes metadata field name to its little-endian int32
// offset in the alpaca.ImageBytesHeaderLen binary header (ASCOM ImageBytes v1).
// Used by the imgfield fault to poke an exact value into one field and hit a
// specific decode branch.
var imgFields = map[string]int{
	"version":   0,  // metadata version
	"errnum":    4,  // error number
	"clienttxn": 8,  // client transaction id
	"servertxn": 12, // server transaction id
	"datastart": 16, // offset where pixel data begins
	"elemtype":  20, // image element type
	"txtype":    24, // transmission element type
	"rank":      28, // 2 or 3
	"dim1":      32, // dimension 1
	"dim2":      36, // dimension 2
	"dim3":      40, // dimension 3 (colour planes)
}

// fault is a per-member injection. kind selects the behaviour; arg carries the
// override JSON (value), status code (http), percent/byte/rate operand, or the
// "field:int" operand for imgfield. method, when set (GET|PUT), limits the fault to
// that HTTP verb -- so a read+write member (e.g. cooleron) can fail only the write and
// leave the status read healthy. count tracks requests seen, for the failfirst/everynth
// deterministic patterns.
type fault struct {
	kind   string
	arg    string
	method string
	count  int
}

// store holds the armed faults; every field is guarded by mu so the control
// channel can mutate it while requests are in flight.
type store struct {
	mu        sync.Mutex
	member    map[string]fault // last-path-segment member -> fault (hang is a kind)
	latency   time.Duration    // global pre-forward delay
	drop      bool             // global: close the connection without responding
	swapBin   bool             // global: flip BinX/BinY 1<->2 on binx/biny PUTs
	jitterMin time.Duration    // global: random per-request delay lower bound
	jitterMax time.Duration    // global: random per-request delay upper bound
	jitterSet bool             // global: jitter was explicitly armed (distinguishes 0-0 from unset)
	lossy     int              // global: percent of requests to RST before forwarding
	lossySet  bool             // global: lossy was explicitly armed (distinguishes 0 from unset)
	chaos     bool             // global: degraded-link combo (jitter + lossy + partial frame drops)
	forceJSON bool             // global: strip Accept: application/imagebytes so the server returns JSON ImageArray
	swapDims  bool             // global: transpose the returned ImageBytes frame (dim1<->dim2 + pixels)
	rng       *rand.Rand       // seeded PRNG for jitter/flaky/lossy/chaos (guarded by mu)
	warned    map[string]bool  // one-time warning dedup for faults that can't apply
}

func newStore(seed int64) *store {
	return &store{
		member: map[string]fault{},
		rng:    rand.New(rand.NewSource(seed)),
	}
}

func (s *store) memberFault(m, reqMethod string) (fault, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.member[m]
	if !ok {
		return fault{}, false
	}
	// A method-scoped fault applies only to that HTTP verb; an unscoped one applies to any.
	if f.method != "" && !strings.EqualFold(f.method, reqMethod) {
		return fault{}, false
	}
	return f, true
}

// warnOnce logs msg the first time a given key surfaces, so an armed fault that
// silently can't apply (wrong content family, compressed body) is observable
// without spamming a line per frame.
func (s *store) warnOnce(key, msg string) {
	s.mu.Lock()
	if s.warned == nil {
		s.warned = map[string]bool{}
	}
	seen := s.warned[key]
	s.warned[key] = true
	s.mu.Unlock()
	if !seen {
		log.Printf("fault_proxy: %s", msg)
	}
}

// preForward computes the global pre-forwarding behaviour for one request: the
// fixed latency, a hard drop, a randomised jitter delay, and whether a lossy/
// chaos RST fires this time. chaos supplies default jitter and loss rates only
// where none were armed explicitly, so an explicit jitter=0-0 / lossy=0 wins.
func (s *store) preForward() (lat, jit time.Duration, rst bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lat = s.latency
	rst = s.drop
	jmin, jmax := s.jitterMin, s.jitterMax
	loss := s.lossy
	if s.chaos {
		if !s.jitterSet {
			jmin, jmax = 50*time.Millisecond, 800*time.Millisecond
		}
		if !s.lossySet {
			loss = 15
		}
	}
	if jmax > jmin {
		jit = jmin + time.Duration(s.rng.Int63n(int64(jmax-jmin)))
	} else {
		jit = jmin
	}
	if loss > 0 && s.rng.Intn(100) < loss {
		rst = true
	}
	return lat, jit, rst
}

func (s *store) swappingBin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.swapBin
}

func (s *store) chaosOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chaos
}

func (s *store) forcingJSON() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forceJSON
}

func (s *store) swappingDims() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.swapDims
}

// roll returns true with probability pct/100.
func (s *store) roll(pct int) bool {
	if pct <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rng.Intn(100) < pct
}

// nextCount increments and returns the per-member request counter, for the
// failfirst/everynth patterns. A member cleared while its request was in
// flight stays cleared (returns 0) rather than being re-inserted empty.
func (s *store) nextCount(m string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.member[m]
	if !ok {
		return 0
	}
	f.count++
	s.member[m] = f
	return f.count
}

// snapshot renders the armed faults for /_ctl/list.
func (s *store) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	members := map[string]string{}
	for m, f := range s.member {
		label := f.kind
		if f.arg != "" {
			label += "(" + f.arg + ")"
		}
		if f.method != "" {
			label = f.method + " " + label
		}
		members[m] = label
	}
	return map[string]any{
		"member_faults": members,
		"latency_ms":    s.latency.Milliseconds(),
		"drop":          s.drop,
		"swap_bin":      s.swapBin,
		"jitter_ms":     []int64{s.jitterMin.Milliseconds(), s.jitterMax.Milliseconds()},
		"lossy_pct":     s.lossy,
		"chaos":         s.chaos,
		"force_json":    s.forceJSON,
		"swap_dims":     s.swapDims,
	}
}

// lastSegment returns the Alpaca member from an /api/v1/<type>/<n>/<member> path,
// lowercased. Alpaca member names are case-insensitive (the goalpaca server
// lowercases them too), so a client requesting /ImageArray must hit the same
// armed fault as one requesting /imagearray.
func lastSegment(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	return strings.ToLower(p)
}

type proxy struct {
	store *store
	rp    *httputil.ReverseProxy
}

// resetConn hijacks the connection and closes it so the client sees a transport
// reset rather than a clean HTTP error -- the failure it must tolerate when a
// server dies or the link drops mid-session. Returns true if it reset.
func resetConn(w http.ResponseWriter) bool {
	if hj, ok := w.(http.Hijacker); ok {
		if c, _, err := hj.Hijack(); err == nil {
			_ = c.Close()
			return true
		}
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	return true
}

// deviceAPI reports whether a path is a device API request (/api/...), the only
// namespace member faults target. Management/setup endpoints share member names
// (e.g. "description" lives at both /api/v1/telescope/0/description and
// /management/v1/description), so faulting by member name alone must not reach them.
func deviceAPI(path string) bool { return strings.HasPrefix(path, "/api/") }

func (px *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/_ctl") {
		px.control(w, r)
		return
	}

	lat, jit, rst := px.store.preForward()
	if jit > 0 {
		time.Sleep(jit)
	}
	if lat > 0 {
		time.Sleep(lat)
	}
	if rst {
		resetConn(w)
		return
	}

	// Pre-forward member faults (hang, http) apply only to the device API.
	if deviceAPI(r.URL.Path) {
		if f, ok := px.store.memberFault(lastSegment(r.URL.Path), r.Method); ok {
			switch f.kind {
			case "hang":
				// Block until the client gives up (its own timeout cancels the
				// request), capped so a stray armed hang can't wedge the proxy
				// forever. On the cap, reset rather than fall through to an
				// implicit empty 200, which a client would misread as success.
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Minute):
					resetConn(w)
				}
				return
			case "http":
				code, err := strconv.Atoi(f.arg)
				if err != nil || code < 100 || code > 599 {
					code = http.StatusInternalServerError
				}
				w.WriteHeader(code)
				return
			}
		}
	}

	px.rp.ServeHTTP(w, r)
}

// modifyResponse rewrites an Alpaca reply for an armed member. Faults fall into
// three families: raw-body mutations (truncate/inject/corrupt*/imgfield/pixels/
// malform) that rewrite the bytes and so also apply to the binary ImageBytes
// frame; streaming faults (partial-drop/dropack/throttle) that wrap the body
// reader; and JSON-object edits (fail/notimpl/emptyerr/value/novalue/flaky/
// failfirst/everynth) on the reply object.
func (px *proxy) modifyResponse(resp *http.Response) error {
	// Member faults (and the imagearray-specific globals) target the device API only.
	if !deviceAPI(resp.Request.URL.Path) {
		return nil
	}
	member := lastSegment(resp.Request.URL.Path)
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isImageBytes := strings.Contains(ct, alpaca.ImageBytesMIME)

	f, hasFault := px.store.memberFault(member, resp.Request.Method)

	// swapdims: transpose the ImageBytes frame (only meaningful on the binary transport).
	if px.store.swappingDims() && member == "imagearray" && isImageBytes {
		return transposeFrame(resp)
	}

	// chaos: occasionally drop an image frame mid-download over the degraded link,
	// but never override an explicit fault a user armed on the frame for a repro.
	if !hasFault && px.store.chaosOn() && member == "imagearray" && px.store.roll(20) {
		return haltBody(resp, 60)
	}

	if !hasFault || f.kind == "http" {
		return nil
	}

	switch f.kind {
	case "contenttype":
		ct := f.arg
		if ct == "" {
			ct = "text/plain"
		}
		resp.Header.Set("Content-Type", ct)
		return nil
	case "partial-drop":
		pct, _ := strconv.Atoi(f.arg)
		return haltBody(resp, pct)
	case "dropack":
		// The upstream already executed the request; deliver none of the reply so
		// the client sees a reset and treats the (completed) command as failed.
		return haltBody(resp, 0)
	case "throttle":
		bps, _ := strconv.Atoi(f.arg)
		if bps < 1 {
			bps = 1
		}
		resp.Body = &throttleReader{r: resp.Body, bps: bps}
		return nil
	case "imgfield", "pixels":
		// These read/write the ImageBytes header layout; on a non-ImageBytes reply
		// they would scribble on arbitrary bytes, so skip and say so once.
		if !isImageBytes {
			px.store.warnOnce(member+"/"+f.kind,
				fmt.Sprintf("%s armed on %q but the reply is %s, not ImageBytes -- not applied",
					f.kind, member, ctLabel(ct)))
			return nil
		}
		return mutateBytes(resp, f)
	case "truncate", "inject", "corrupthead", "corrupttail", "malform":
		// Content-agnostic raw-byte faults apply to any body.
		return mutateBytes(resp, f)
	}

	// Remaining kinds are JSON-object edits: they need a JSON reply and an
	// undisturbed (identity-encoded) body.
	if !strings.Contains(ct, "json") {
		px.store.warnOnce(member+"/"+f.kind,
			fmt.Sprintf("%s armed on %q but the reply is %s, not JSON -- not applied",
				f.kind, member, ctLabel(ct)))
		return nil
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		px.store.warnOnce(member+"/enc",
			fmt.Sprintf("%s armed on %q but the reply is %s-encoded -- not applied", f.kind, member, enc))
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}

	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		// Not a JSON object -- leave the original bytes in place.
		restoreBody(resp, body)
		return nil
	}

	switch f.kind {
	case "fail":
		setErr(m, alpaca.ErrNumDriverBase, "injected driver error")
	case "notimpl":
		setErr(m, alpaca.ErrNumNotImplemented, "not implemented (injected)")
	case "emptyerr":
		setErr(m, alpaca.ErrNumInvalidOperation, "")
	case "value":
		m["Value"] = json.RawMessage(f.arg)
	case "novalue":
		delete(m, "Value")
	case "flaky":
		rate, _ := strconv.Atoi(f.arg)
		if px.store.roll(rate) {
			setErr(m, alpaca.ErrNumDriverBase, "injected flaky error")
		}
	case "failfirst":
		n, _ := strconv.Atoi(f.arg)
		if c := px.store.nextCount(member); c > 0 && c <= n {
			setErr(m, alpaca.ErrNumDriverBase, "injected transient error")
		}
	case "everynth":
		n, _ := strconv.Atoi(f.arg)
		if c := px.store.nextCount(member); n > 0 && c > 0 && c%n == 0 {
			setErr(m, alpaca.ErrNumDriverBase, "injected periodic error")
		}
	}

	nb, err := json.Marshal(m)
	if err != nil {
		restoreBody(resp, body)
		return nil
	}
	restoreBody(resp, nb)
	return nil
}

// ctLabel renders a Content-Type for a log message, mapping the empty string to
// a readable placeholder.
func ctLabel(ct string) string {
	if ct == "" {
		return "(none)"
	}
	return ct
}

// mutateBytes corrupts the raw response body -- used against the ImageBytes
// camera frame to exercise the client's frame decode/validation. truncate/inject
// take a percent of the body; corrupthead/corrupttail take a byte count; imgfield
// sets an exact header field; pixels fills the pixel region; malform replaces the
// body with unparseable JSON.
func mutateBytes(resp *http.Response, f fault) error {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	n := len(body)
	arg, _ := strconv.Atoi(f.arg)
	switch f.kind {
	case "truncate": // keep the leading arg% of the body (drop the tail)
		body = body[:clamp(pctOf(n, arg), 0, n)]
	case "inject": // splice arg% junk bytes into the middle
		extra := pctOf(n, arg)
		mid := n / 2
		b := make([]byte, 0, n+extra)
		b = append(b, body[:mid]...)
		b = append(b, bytes.Repeat([]byte{0xEE}, extra)...)
		body = append(b, body[mid:]...)
	case "corrupthead": // flip the first arg bytes (the ImageBytes header)
		for i := 0; i < clamp(arg, 0, n); i++ {
			body[i] ^= 0xFF
		}
	case "corrupttail": // flip the last arg bytes
		for i := n - clamp(arg, 0, n); i < n; i++ {
			body[i] ^= 0xFF
		}
	case "imgfield": // set one ImageBytes header field to an exact int32
		field, valStr, _ := strings.Cut(f.arg, ":")
		if off, known := imgFields[field]; known {
			v, _ := strconv.Atoi(valStr)
			putI32(body, off, int32(v))
		}
	case "pixels": // fill the pixel region (past dataStart) with a constant
		start := clamp(int(getI32(body, imgFields["datastart"])), 0, n)
		fill := byte(0x00)
		if f.arg == "sat" {
			fill = 0xFF
		}
		for i := start; i < n; i++ {
			body[i] = fill
		}
	case "malform": // replace with an unterminated JSON object
		body = []byte(`{"ClientTransactionID":0,"Value":`)
	}
	restoreBody(resp, body)
	return nil
}

// transposeFrame rewrites a binary ImageBytes response so its axes are swapped: it
// exchanges the dim1/dim2 header fields and transposes the column-major pixel block, so
// a client that requested a W x H ROI receives a valid H x W frame -- exactly what a
// driver that emits its ImageArray transposed would send. A client with axis-swap
// handling must detect the flip and undo it. rank != 2 or an unknown element size is
// left unchanged.
func transposeFrame(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	const hdr = alpaca.ImageBytesHeaderLen
	if len(body) < hdr {
		restoreBody(resp, body)
		return nil
	}
	dataStart := int(getI32(body, imgFields["datastart"]))
	rank := int(getI32(body, imgFields["rank"]))
	w := int(getI32(body, imgFields["dim1"]))
	h := int(getI32(body, imgFields["dim2"]))
	elem := alpaca.ElementSize(alpaca.ImageElementType(getI32(body, imgFields["txtype"])))
	if elem <= 0 {
		restoreBody(resp, body)
		return nil
	}
	if rank != 2 || w <= 0 || h <= 0 || dataStart < hdr || dataStart+w*h*elem > len(body) {
		restoreBody(resp, body)
		return nil
	}
	// The wire order is column-major (dim2/y fastest): src index for (x,y) is (x*h+y)*elem.
	// The transposed frame is h x w, also column-major (new dim2 = w fastest): dst index
	// for (x'=0..h, y'=0..w) is (x'*w+y')*elem, taken from src (x=y', y=x').
	src := body[dataStart:]
	out := make([]byte, len(body))
	copy(out, body[:dataStart])
	dst := out[dataStart:]
	for xp := 0; xp < h; xp++ {
		for yp := 0; yp < w; yp++ {
			si := (yp*h + xp) * elem
			di := (xp*w + yp) * elem
			copy(dst[di:di+elem], src[si:si+elem])
		}
	}
	putI32(out, imgFields["dim1"], int32(h)) // swap the advertised dimensions
	putI32(out, imgFields["dim2"], int32(w))
	restoreBody(resp, out)
	return nil
}

// haltReader delivers up to remaining bytes from the underlying body then returns
// an unexpected EOF, so the client sees fewer bytes than the advertised
// Content-Length and the connection aborts mid-transfer.
type haltReader struct {
	r         io.ReadCloser
	remaining int64
}

func (h *haltReader) Read(p []byte) (int, error) {
	if h.remaining <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if int64(len(p)) > h.remaining {
		p = p[:h.remaining]
	}
	n, err := h.r.Read(p)
	h.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	if h.remaining <= 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func (h *haltReader) Close() error { return h.r.Close() }

// haltBody advertises the full Content-Length but delivers only pct of the body
// before aborting the transfer, so the client sees a mid-download reset. The body
// is buffered so the delivered fraction is exact regardless of whether the
// upstream used Content-Length or chunked encoding.
func haltBody(resp *http.Response, pct int) error {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	keep := pctOf(len(body), clamp(pct, 0, 100))
	// The whole point is a short transfer that aborts; even pct=100 must leave the
	// client at least one byte shy of the advertised length so it observes the reset.
	if len(body) > 0 && keep >= len(body) {
		keep = len(body) - 1
	}
	// Keep the full advertised length so the client expects more than it gets.
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Body = &haltReader{r: io.NopCloser(bytes.NewReader(body[:keep])), remaining: int64(keep)}
	return nil
}

// pctOf returns n*pct/100 computed in int64 to avoid overflow on 32-bit builds
// (a >21 MB body times a percent exceeds MaxInt32); the result fits back in int
// because it is bounded by n.
func pctOf(n, pct int) int {
	return int(int64(n) * int64(pct) / 100)
}

// throttleReader drips the body at roughly bps bytes/second, so a large frame
// stalls mid-transfer without ever failing -- the slow-link case the client's
// read deadline must tolerate or time out on.
type throttleReader struct {
	r   io.ReadCloser
	bps int
}

func (t *throttleReader) Read(p []byte) (int, error) {
	chunk := t.bps / 10
	if chunk < 1 {
		chunk = 1
	}
	if len(p) > chunk {
		p = p[:chunk]
	}
	n, err := t.r.Read(p)
	if n > 0 {
		time.Sleep(time.Duration(n) * time.Second / time.Duration(t.bps))
	}
	return n, err
}

func (t *throttleReader) Close() error { return t.r.Close() }

func putI32(b []byte, off int, v int32) {
	if off < 0 || off+4 > len(b) {
		return
	}
	binary.LittleEndian.PutUint32(b[off:], uint32(v))
}

func getI32(b []byte, off int) int32 {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b[off:]))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func setErr(m map[string]json.RawMessage, num int, msg string) {
	m["ErrorNumber"] = json.RawMessage(strconv.Itoa(num))
	mb, _ := json.Marshal(msg)
	m["ErrorMessage"] = mb
}

func restoreBody(resp *http.Response, b []byte) {
	resp.Body = io.NopCloser(bytes.NewReader(b))
	resp.ContentLength = int64(len(b))
	resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
}

func (px *proxy) control(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	action := strings.TrimPrefix(r.URL.Path, "/_ctl/")
	action = strings.Trim(action, "/")
	if action == "" || action == "_ctl" {
		action = "list"
	}

	switch action {
	case "list":
	case "clear":
		s := px.store
		s.mu.Lock()
		if m := strings.ToLower(q.Get("member")); m != "" {
			delete(s.member, m)
		} else {
			s.member = map[string]fault{}
			s.latency = 0
			s.drop = false
			s.swapBin = false
			s.jitterMin, s.jitterMax, s.jitterSet = 0, 0, false
			s.lossy, s.lossySet = 0, false
			s.chaos = false
			s.forceJSON = false
			s.swapDims = false
			s.warned = nil
		}
		s.mu.Unlock()
	case "set":
		if err := px.set(q); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "unknown action "+action+" (use set|clear|list)", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(px.store.snapshot())
}

func (px *proxy) set(q url.Values) error {
	kind := q.Get("fault")
	member := strings.ToLower(q.Get("member"))
	value := q.Get("value")
	method := strings.ToUpper(q.Get("method"))
	if method != "" && method != "GET" && method != "PUT" {
		return fmt.Errorf("method must be GET or PUT")
	}
	if method != "" && member == "" {
		return fmt.Errorf("method= applies only to a member fault")
	}
	s := px.store

	switch kind {
	case "latency":
		ms, err := strconv.Atoi(value)
		if err != nil || ms < 0 {
			return fmt.Errorf("latency needs value=<milliseconds>")
		}
		s.mu.Lock()
		s.latency = time.Duration(ms) * time.Millisecond
		s.mu.Unlock()
	case "jitter":
		lo, hi, err := parseRange(value)
		if err != nil {
			return fmt.Errorf("jitter needs value=<minMs>-<maxMs>")
		}
		s.mu.Lock()
		s.jitterMin, s.jitterMax, s.jitterSet = lo, hi, true
		s.mu.Unlock()
	case "lossy":
		p, err := strconv.Atoi(value)
		if err != nil || p < 0 || p > 100 {
			return fmt.Errorf("lossy needs value=<percent 0-100>")
		}
		s.mu.Lock()
		s.lossy, s.lossySet = p, true
		s.mu.Unlock()
	case "drop":
		s.mu.Lock()
		s.drop = boolArg(value)
		s.mu.Unlock()
	case "chaos", "badwifi":
		s.mu.Lock()
		s.chaos = boolArg(value)
		s.mu.Unlock()
	case "swapbin":
		s.mu.Lock()
		s.swapBin = boolArg(value)
		s.mu.Unlock()
	case "forcejson":
		// Strip Accept: application/imagebytes so the upstream returns the JSON
		// ImageArray transport -- exercises a client's JSON fallback decode path.
		s.mu.Lock()
		s.forceJSON = boolArg(value)
		s.mu.Unlock()
	case "swapdims":
		// Transpose the returned ImageBytes frame (swap dim1/dim2 and the pixels) --
		// simulates a driver that emits its array with the axes flipped.
		s.mu.Lock()
		s.swapDims = boolArg(value)
		s.mu.Unlock()
	case "hang":
		if member == "" {
			return fmt.Errorf("hang needs member=<name>")
		}
		s.armMember(member, kind, "", method)
	case "truncate", "inject", "partial-drop":
		if member == "" {
			return fmt.Errorf("%s needs member=<name> (e.g. imagearray)", kind)
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 100 {
			return fmt.Errorf("%s needs value=<percent 0-100>", kind)
		}
		s.armMember(member, kind, value, method)
	case "corrupthead", "corrupttail":
		if member == "" {
			return fmt.Errorf("%s needs member=<name> (e.g. imagearray)", kind)
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("%s needs value=<byte count>", kind)
		}
		s.armMember(member, kind, value, method)
	case "imgfield":
		if member == "" {
			return fmt.Errorf("imgfield needs member=<name> (e.g. imagearray)")
		}
		field, valStr, ok := strings.Cut(value, ":")
		if _, known := imgFields[field]; !ok || !known {
			return fmt.Errorf("imgfield needs value=<field>:<int> where field is one of %s", fieldNames())
		}
		if _, err := strconv.Atoi(valStr); err != nil {
			return fmt.Errorf("imgfield value must be <field>:<int>")
		}
		s.armMember(member, kind, value, method)
	case "pixels":
		if member == "" {
			return fmt.Errorf("pixels needs member=<name> (e.g. imagearray)")
		}
		if value != "sat" && value != "zero" {
			return fmt.Errorf("pixels needs value=sat|zero")
		}
		s.armMember(member, kind, value, method)
	case "throttle":
		if member == "" {
			return fmt.Errorf("throttle needs member=<name> (e.g. imagearray)")
		}
		if n, err := strconv.Atoi(value); err != nil || n <= 0 {
			return fmt.Errorf("throttle needs value=<bytes/sec>")
		}
		s.armMember(member, kind, value, method)
	case "flaky":
		if member == "" {
			return fmt.Errorf("flaky needs member=<name>")
		}
		if n, err := strconv.Atoi(value); err != nil || n < 0 || n > 100 {
			return fmt.Errorf("flaky needs value=<percent 0-100>")
		}
		s.armMember(member, kind, value, method)
	case "failfirst", "everynth":
		if member == "" {
			return fmt.Errorf("%s needs member=<name>", kind)
		}
		if n, err := strconv.Atoi(value); err != nil || n < 1 {
			return fmt.Errorf("%s needs value=<count >= 1>", kind)
		}
		s.armMember(member, kind, value, method)
	case "dropack":
		if member == "" {
			return fmt.Errorf("dropack needs member=<name> (e.g. pulseguide)")
		}
		s.armMember(member, kind, "", method)
	case "contenttype":
		if member == "" {
			return fmt.Errorf("contenttype needs member=<name>")
		}
		s.armMember(member, kind, value, method)
	case "novalue", "malform":
		if member == "" {
			return fmt.Errorf("%s needs member=<name>", kind)
		}
		s.armMember(member, kind, "", method)
	case "fail", "notimpl", "emptyerr", "http", "value":
		if member == "" {
			return fmt.Errorf("%s needs member=<name>", kind)
		}
		arg := ""
		switch kind {
		case "http":
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf("http needs value=<status code>")
			}
			arg = value
		case "value":
			// Value must be valid JSON on the wire; wrap a bare token as a
			// JSON string so e.g. value=true stays a bool but value=east
			// becomes "east".
			if !json.Valid([]byte(value)) {
				b, _ := json.Marshal(value)
				value = string(b)
			}
			arg = value
		}
		s.armMember(member, kind, arg, method)
	default:
		return fmt.Errorf("unknown fault %q (fail|notimpl|emptyerr|value|novalue|malform|http|hang|"+
			"latency|drop|swapbin|forcejson|swapdims|truncate|inject|corrupthead|corrupttail|imgfield|"+
			"pixels|partial-drop|dropack|contenttype|throttle|jitter|flaky|lossy|chaos|badwifi|"+
			"failfirst|everynth)", kind)
	}
	return nil
}

// boolArg interprets a global toggle's optional value: absent/anything arms it,
// but off|false|0 disarms it, so a single toggle can be turned off without a
// full clear that would wipe every other armed fault.
func boolArg(value string) bool {
	switch strings.ToLower(value) {
	case "off", "false", "0", "no":
		return false
	}
	return true
}

// armMember replaces the fault on a member, resetting its request counter and
// applying the optional method scope atomically (so it can never restamp a
// different fault, and hang is scoped like any other member fault).
func (s *store) armMember(member, kind, arg, method string) {
	s.mu.Lock()
	s.member[member] = fault{kind: kind, arg: arg, method: method}
	s.mu.Unlock()
}

// parseRange parses "<lo>-<hi>" millisecond bounds.
func parseRange(v string) (time.Duration, time.Duration, error) {
	lo, hi, ok := strings.Cut(v, "-")
	if !ok {
		return 0, 0, fmt.Errorf("range")
	}
	a, err1 := strconv.Atoi(lo)
	b, err2 := strconv.Atoi(hi)
	if err1 != nil || err2 != nil || a < 0 || b < a {
		return 0, 0, fmt.Errorf("range")
	}
	return time.Duration(a) * time.Millisecond, time.Duration(b) * time.Millisecond, nil
}

func fieldNames() string {
	names := make([]string, 0, len(imgFields))
	for k := range imgFields {
		names = append(names, k)
	}
	return strings.Join(names, "|")
}

// swapBinningBody flips the BinX/BinY form field 1<->2 in a binx/biny PUT so the
// upstream camera bins differently than the client asked -- the returned frame
// dimensions then won't match the requested ROI, exercising the frame-size guard.
func swapBinningBody(req *http.Request) {
	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}
	vals, err := url.ParseQuery(string(body))
	if err == nil {
		for _, key := range []string{"BinX", "BinY"} {
			switch vals.Get(key) {
			case "1":
				vals.Set(key, "2")
			case "2":
				vals.Set(key, "1")
			}
		}
		body = []byte(vals.Encode())
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

// newFaultProxy wires the reverse proxy to target with the request-side
// (swapbin) and response-side (modifyResponse) fault hooks around st.
func newFaultProxy(target *url.URL, st *store) *proxy {
	rp := httputil.NewSingleHostReverseProxy(target)
	// NewSingleHostReverseProxy keeps the original Host header; point it at the
	// upstream so name-based servers route correctly.
	baseDirector := rp.Director
	rp.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = target.Host
		if req.Method == http.MethodPut && st.swappingBin() {
			if m := lastSegment(req.URL.Path); m == "binx" || m == "biny" {
				swapBinningBody(req)
			}
		}
		// forcejson: drop the ImageBytes Accept so the upstream falls back to JSON.
		if st.forcingJSON() && lastSegment(req.URL.Path) == "imagearray" {
			req.Header.Del("Accept")
		}
	}
	// Flush each write to the client immediately so partial-drop and throttle
	// deliver visible bytes before the transfer stalls or aborts, rather than
	// the whole (short) body being discarded from the write buffer on reset.
	rp.FlushInterval = -1
	px := &proxy{store: st, rp: rp}
	rp.ModifyResponse = px.modifyResponse
	return px
}

// listenPort extracts the numeric port from a listen address (":11510" ->
// 11510), or 0 if it has none.
func listenPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

// resolveUpstream picks the upstream Alpaca server. Without -discover it just
// parses -upstream. With -discover it broadcasts and selects the first server --
// or the first hosting a device of type match, when -discover-match is set --
// excluding the proxy's own port on loopback (a stale same-host instance), so it
// never targets itself. It is called before the advertise responder starts, so
// the proxy's own current advertisement can't appear in the results.
func resolveUpstream(ctx context.Context, discover bool, upstream, match string, ownPort int, timeout time.Duration) (*url.URL, error) {
	if !discover {
		return url.Parse("http://" + upstream)
	}
	servers, err := client.DiscoverContext(ctx, timeout)
	if err != nil {
		return nil, fmt.Errorf("discovery failed: %w", err)
	}
	s, err := chooseServer(servers, match, ownPort, func(s client.DiscoveredServer) ([]client.ConfiguredDevice, error) {
		return s.ConfiguredDevices()
	})
	if err != nil {
		return nil, err
	}
	return url.Parse("http://" + s.Address)
}

// chooseServer selects the upstream from discovery results: it drops the proxy's
// own port on loopback (a stale same-host instance), then returns the first
// server -- or the first whose devices (looked up via the devices callback)
// include a match-typed device, when match is non-empty.
func chooseServer(servers []client.DiscoveredServer, match string, ownPort int,
	devices func(client.DiscoveredServer) ([]client.ConfiguredDevice, error)) (client.DiscoveredServer, error) {
	var found []client.DiscoveredServer
	for _, s := range servers {
		if s.AlpacaPort == ownPort && s.IP.IsLoopback() {
			continue // don't target ourselves
		}
		found = append(found, s)
	}
	if len(found) == 0 {
		return client.DiscoveredServer{}, fmt.Errorf("discovery found no upstream Alpaca servers")
	}
	if match == "" {
		if len(found) > 1 {
			log.Printf("discovery: %d servers found; using %s (use -discover-match to choose)", len(found), found[0].Address)
		}
		return found[0], nil
	}
	for _, s := range found {
		devs, err := devices(s)
		if err != nil {
			log.Printf("discovery: %s configureddevices failed: %v", s.Address, err)
			continue
		}
		for _, d := range devs {
			if strings.EqualFold(d.DeviceType, match) {
				log.Printf("discovery: %s hosts a %s (%q); using it", s.Address, d.DeviceType, d.DeviceName)
				return s, nil
			}
		}
	}
	return client.DiscoveredServer{}, fmt.Errorf("discovery found no server hosting a %q device", match)
}

func main() {
	listen := flag.String("listen", ":11510", "proxy HTTP listen address")
	upstream := flag.String("upstream", "127.0.0.1:11111", "upstream Alpaca server host:port")
	seed := flag.Int64("seed", 0, "PRNG seed for jitter/flaky/lossy/chaos (0 = time-based)")
	advertise := flag.Bool("advertise", false, "answer Alpaca UDP discovery (:32227) with the proxy's own port so it appears in discovery tools alongside the real device (co-binds via SO_REUSEPORT)")
	discover := flag.Bool("discover", false, "find the upstream via Alpaca UDP discovery instead of using -upstream")
	discoverMatch := flag.String("discover-match", "", "with -discover, require the chosen server to host a device of this type (e.g. camera, telescope)")
	discoverTimeout := flag.Duration("discover-timeout", 2*time.Second, "discovery listen window for -discover")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ownPort := listenPort(*listen)
	if (*advertise || *discover) && ownPort == 0 {
		log.Fatalf("-advertise/-discover need a numeric port in -listen %q", *listen)
	}

	target, err := resolveUpstream(ctx, *discover, *upstream, *discoverMatch, ownPort, *discoverTimeout)
	if err != nil {
		log.Fatalf("upstream: %v", err)
	}

	sd := *seed
	if sd == 0 {
		sd = time.Now().UnixNano()
	}
	px := newFaultProxy(target, newStore(sd))

	srv := &http.Server{Addr: *listen, Handler: px}

	if *advertise {
		go runAdvertise(ctx, ownPort)
		log.Printf("advertising Alpaca discovery on :%d -> AlpacaPort %d", alpacaDiscoveryPort, ownPort)
	}

	drained := make(chan struct{})
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		close(drained)
	}()

	// For a bare ":port" listen address, show a reachable host in the hint.
	host := *listen
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	log.Printf("alpaca fault proxy: %s -> %s  (prng seed %d)", *listen, target.Host, sd)
	log.Printf("control: curl 'http://%s/_ctl/list'  (see file header for the fault menu)", host)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
	<-drained // let the in-flight requests finish their graceful-shutdown window
	log.Println("shutting down")
}
