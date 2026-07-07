package server

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// getImageArrayJSON GETs imagearray with NO ImageBytes Accept header and
// decodes the JSON envelope.
func getImageArrayJSON(t *testing.T, s *Server) (typ, rank int, value json.RawMessage, errNum int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/camera/0/imagearray", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var env struct {
		Type        int
		Rank        int
		Value       json.RawMessage
		ErrorNumber int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("envelope decode: %v", err)
	}
	return env.Type, env.Rank, env.Value, env.ErrorNumber
}

// TestImageArrayJSON verifies the mandatory plain-JSON ImageArray transport: a
// GET without Accept: application/imagebytes must return the Type/Rank/Value
// envelope with Value in the ASCOM [Width][Height] wire order (Y varying
// fastest — the same transpose the ImageBytes path applies).
func TestImageArrayJSON(t *testing.T) {
	s := newTestServer(t)
	put(t, s, "/api/v1/camera/0/startexposure", url.Values{"Duration": {"1.0"}, "Light": {"true"}})

	typ, rank, value, errNum := getImageArrayJSON(t, s)
	if errNum != 0 {
		t.Fatalf("ErrorNumber = %#x, want 0", errNum)
	}
	if typ != 2 { // fakeCamera presents ElementType Int32
		t.Errorf("Type = %d, want 2 (Int32)", typ)
	}
	if rank != 2 {
		t.Errorf("Rank = %d, want 2", rank)
	}
	var v [][]int64
	if err := json.Unmarshal(value, &v); err != nil {
		t.Fatalf("Value decode: %v", err)
	}
	// fakeCamera: 100x50 UInt16 frame, all zero except Pixels[0] = 0xBEEF
	// (row-major position x=0,y=0).
	if len(v) != 100 || len(v[0]) != 50 {
		t.Fatalf("Value dims = %dx%d, want 100x50 ([Width][Height])", len(v), len(v[0]))
	}
	if v[0][0] != 0xBEEF {
		t.Errorf("Value[0][0] = %d, want %d", v[0][0], 0xBEEF)
	}
	if v[1][0] != 0 || v[0][1] != 0 {
		t.Errorf("Value[1][0], Value[0][1] = %d, %d, want 0, 0", v[1][0], v[0][1])
	}
}

// jsonFrameCamera serves an arbitrary ImageFrame for transport tests.
type jsonFrameCamera struct {
	BaseCamera
	frame ImageFrame
}

func (c *jsonFrameCamera) ImageReady() bool { return true }
func (c *jsonFrameCamera) ImageFrame() (ImageFrame, error) {
	return c.frame, nil
}

func registerFrame(t *testing.T, frame ImageFrame) *Server {
	t.Helper()
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	cam := &jsonFrameCamera{frame: frame}
	cam.DevName = "JSONCam"
	cam.IfaceVer = 4
	cam.MarkConnected()
	if err := s.Register(CameraType, 0, cam); err != nil {
		t.Fatalf("register: %v", err)
	}
	return s
}

// TestImageArrayJSONTranspose pins the exact wire order with a tiny
// asymmetric frame: 3 wide x 2 high, row-major pixels 1..6 →
// Value[x][y] = [[1,4],[2,5],[3,6]].
func TestImageArrayJSONTranspose(t *testing.T) {
	pix := make([]byte, 6*2)
	for i := 0; i < 6; i++ {
		binary.LittleEndian.PutUint16(pix[i*2:], uint16(i+1))
	}
	s := registerFrame(t, ImageFrame{
		Rank: 2, Width: 3, Height: 2,
		ElementType:             ImgInt32,
		TransmissionElementType: ImgUInt16,
		Pixels:                  pix,
	})

	_, _, value, errNum := getImageArrayJSON(t, s)
	if errNum != 0 {
		t.Fatalf("ErrorNumber = %#x, want 0", errNum)
	}
	want := "[[1,4],[2,5],[3,6]]"
	if string(value) != want {
		t.Errorf("Value = %s, want %s", value, want)
	}
}

// TestImageArrayJSONRank3 verifies rank-3 (colour) frames stream as
// [Width][Height][Planes] triple-nested arrays.
func TestImageArrayJSONRank3(t *testing.T) {
	// 2 wide x 1 high x 3 planes, wire-order bytes 10,20,30 (x=0) 40,50,60 (x=1).
	pix := []byte{10, 20, 30, 40, 50, 60}
	s := registerFrame(t, ImageFrame{
		Rank: 3, Width: 2, Height: 1, Planes: 3,
		ElementType:             ImgInt32,
		TransmissionElementType: ImgByte,
		Pixels:                  pix,
	})

	_, rank, value, errNum := getImageArrayJSON(t, s)
	if errNum != 0 {
		t.Fatalf("ErrorNumber = %#x, want 0", errNum)
	}
	if rank != 3 {
		t.Errorf("Rank = %d, want 3", rank)
	}
	want := "[[[10,20,30]],[[40,50,60]]]"
	if string(value) != want {
		t.Errorf("Value = %s, want %s", value, want)
	}
}

// TestImageArrayJSONDouble verifies float frames stream as JSON doubles.
func TestImageArrayJSONDouble(t *testing.T) {
	pix := make([]byte, 8*2)
	binary.LittleEndian.PutUint64(pix[0:], math.Float64bits(1.5))
	binary.LittleEndian.PutUint64(pix[8:], math.Float64bits(-2.25))
	s := registerFrame(t, ImageFrame{
		Rank: 2, Width: 1, Height: 2,
		ElementType: ImgDouble,
		Pixels:      pix,
	})

	typ, _, value, errNum := getImageArrayJSON(t, s)
	if errNum != 0 {
		t.Fatalf("ErrorNumber = %#x, want 0", errNum)
	}
	if typ != 3 {
		t.Errorf("Type = %d, want 3 (Double)", typ)
	}
	want := "[[1.5,-2.25]]"
	if string(value) != want {
		t.Errorf("Value = %s, want %s", value, want)
	}
}

// TestImageArrayJSONMalformed verifies a frame whose pixel buffer doesn't
// match its declared geometry produces an in-band JSON error, not a panic.
func TestImageArrayJSONMalformed(t *testing.T) {
	s := registerFrame(t, ImageFrame{
		Rank: 2, Width: 4, Height: 4,
		ElementType: ImgInt32,
		Pixels:      []byte{1, 2, 3}, // 3 bytes, want 64
	})

	_, _, _, errNum := getImageArrayJSON(t, s)
	if errNum == 0 {
		t.Fatal("ErrorNumber = 0, want non-zero for malformed frame")
	}
}

// TestImageArrayJSONNoImage verifies the no-image device error round-trips
// the JSON envelope (no Accept header) as an in-band error.
func TestImageArrayJSONNoImage(t *testing.T) {
	s := newTestServer(t) // fakeCamera with no exposure started
	_, _, _, errNum := getImageArrayJSON(t, s)
	if errNum == 0 {
		t.Fatal("ErrorNumber = 0, want device error before exposure")
	}
}
