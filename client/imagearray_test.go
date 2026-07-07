package client

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikefsq/goalpaca/server"
)

// jsonOnlyImageServer mimics a server that ignores the ImageBytes Accept
// negotiation and always answers imagearray with the plain-JSON form.
func jsonOnlyImageServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestImageArrayJSONFallback verifies the client decodes the mandatory
// plain-JSON ImageArray form from a server that ignores
// Accept: application/imagebytes: wire [Width][Height] (Y fastest) →
// row-major pixels, matching what DecodeImageBytes produces.
func TestImageArrayJSONFallback(t *testing.T) {
	// 3 wide x 2 high, Value[x][y]: row-major sequence is 1..6.
	ts := jsonOnlyImageServer(t,
		`{"Type":2,"Rank":2,"Value":[[1,4],[2,5],[3,6]],"ClientTransactionID":1,"ServerTransactionID":1,"ErrorNumber":0,"ErrorMessage":""}`)

	c := NewCamera(ts.URL, 0)
	frame, err := c.ImageArray()
	if err != nil {
		t.Fatalf("ImageArray: %v", err)
	}
	if frame.Rank != 2 || frame.Width != 3 || frame.Height != 2 {
		t.Fatalf("frame = rank %d %dx%d, want rank 2 3x2", frame.Rank, frame.Width, frame.Height)
	}
	if frame.ElementType != server.ImgInt32 {
		t.Errorf("ElementType = %v, want Int32", frame.ElementType)
	}
	for i := 0; i < 6; i++ {
		if got := int32(binary.LittleEndian.Uint32(frame.Pixels[i*4:])); got != int32(i+1) {
			t.Errorf("pixel %d = %d, want %d (row-major)", i, got, i+1)
		}
	}
}

// TestImageArrayJSONFallbackRank3 verifies rank-3 colour decode, with Rank
// inferred from nesting when the envelope omits it.
func TestImageArrayJSONFallbackRank3(t *testing.T) {
	ts := jsonOnlyImageServer(t,
		`{"Type":2,"Value":[[[10,20,30]],[[40,50,60]]],"ClientTransactionID":1,"ServerTransactionID":1,"ErrorNumber":0,"ErrorMessage":""}`)

	c := NewCamera(ts.URL, 0)
	frame, err := c.ImageArray()
	if err != nil {
		t.Fatalf("ImageArray: %v", err)
	}
	if frame.Rank != 3 || frame.Width != 2 || frame.Height != 1 || frame.Planes != 3 {
		t.Fatalf("frame = rank %d %dx%dx%d, want rank 3 2x1x3", frame.Rank, frame.Width, frame.Height, frame.Planes)
	}
	want := []int32{10, 20, 30, 40, 50, 60} // wire order [W][H][P]
	for i, w := range want {
		if got := int32(binary.LittleEndian.Uint32(frame.Pixels[i*4:])); got != w {
			t.Errorf("pixel %d = %d, want %d", i, got, w)
		}
	}
}

// TestImageArrayJSONFallbackRagged verifies a ragged Value is rejected
// instead of decoded into a torn frame.
func TestImageArrayJSONFallbackRagged(t *testing.T) {
	ts := jsonOnlyImageServer(t,
		`{"Type":2,"Rank":2,"Value":[[1,2],[3]],"ClientTransactionID":1,"ServerTransactionID":1,"ErrorNumber":0,"ErrorMessage":""}`)

	c := NewCamera(ts.URL, 0)
	if _, err := c.ImageArray(); err == nil {
		t.Fatal("ImageArray on ragged Value: want error, got nil")
	}
}

// TestImageArrayJSONRoundTrip drives the real goalpaca server's JSON
// ImageArray path (no ImageBytes Accept header) end-to-end through the
// client decoder and checks it matches the frame the ImageBytes transport
// yields.
func TestImageArrayJSONRoundTrip(t *testing.T) {
	dev := &fakeCamera{}
	dev.DevName = "Cam"
	dev.IfaceVer = 4
	ts := serve(t, server.CameraType, dev)

	c := NewCamera(ts.URL, 0)
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.StartExposure(1.0, true); err != nil {
		t.Fatalf("StartExposure: %v", err)
	}

	viaBytes, err := c.ImageArray()
	if err != nil {
		t.Fatalf("ImageArray (imagebytes): %v", err)
	}

	// Fetch the same member with the JSON path by not advertising ImageBytes.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/camera/0/imagearray", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	// Decode through the client's fallback (as getImageBytesCtx would).
	viaJSON := func() server.ImageFrame {
		jsonTS := jsonOnlyImageServer(t, readAllString(t, resp))
		jc := NewCamera(jsonTS.URL, 0)
		frame, err := jc.ImageArray()
		if err != nil {
			t.Fatalf("ImageArray (json): %v", err)
		}
		return frame
	}()

	if viaJSON.Rank != viaBytes.Rank || viaJSON.Width != viaBytes.Width || viaJSON.Height != viaBytes.Height {
		t.Fatalf("json frame %dx%d rank %d != imagebytes frame %dx%d rank %d",
			viaJSON.Width, viaJSON.Height, viaJSON.Rank, viaBytes.Width, viaBytes.Height, viaBytes.Rank)
	}
	// fakeCamera pixel 0 is 0xBEEF; both transports must agree on the
	// row-major position. ImageBytes decodes to UInt16 wire values, JSON to
	// Int32 — compare numerically.
	jb := int32(binary.LittleEndian.Uint32(viaJSON.Pixels[0:]))
	bb := binary.LittleEndian.Uint16(viaBytes.Pixels[0:])
	if jb != int32(bb) || jb != 0xBEEF {
		t.Errorf("pixel 0: json %d, imagebytes %d, want both 0xBEEF", jb, bb)
	}
}

func readAllString(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
