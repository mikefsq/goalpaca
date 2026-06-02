package alpacadev

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeCamera is a minimal in-memory Camera for exercising the HTTP adapter
// without hardware. It overrides only what the tests touch.
type fakeCamera struct {
	BaseCamera
	gain    int
	started bool
	busy    bool
}

func (c *fakeCamera) Busy() bool           { return c.busy }
func (c *fakeCamera) AbortExposure() error { c.started = false; return nil }

func newFakeCamera() *fakeCamera {
	c := &fakeCamera{}
	c.ID = "fake-guid-1"
	c.DevName = "FakeCam"
	c.IfaceVer = 4
	return c
}

func (c *fakeCamera) CameraXSize() int       { return 100 }
func (c *fakeCamera) CameraYSize() int       { return 50 }
func (c *fakeCamera) CanAbortExposure() bool { return true }
func (c *fakeCamera) Gain() int              { return c.gain }
func (c *fakeCamera) SetGain(g int) error    { c.gain = g; return nil }
func (c *fakeCamera) StartExposure(float64, bool) error {
	c.started = true
	return nil
}
func (c *fakeCamera) ImageReady() bool { return c.started }
func (c *fakeCamera) ImageFrame() (ImageFrame, error) {
	if !c.started {
		return ImageFrame{}, ErrValueNotSet
	}
	pix := make([]byte, 100*50*2) // 16-bit mono
	binary.LittleEndian.PutUint16(pix[0:], 0xBEEF)
	return ImageFrame{
		Rank: 2, Width: 100, Height: 50,
		ElementType:             ImgInt32,
		TransmissionElementType: ImgUInt16,
		Pixels:                  pix,
	}, nil
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := New(Config{AlpacaPort: 0, Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "test"})
	cam := newFakeCamera()
	cam.MarkConnected() // operational members are gated until connected
	if err := s.Register(CameraType, 0, cam); err != nil {
		t.Fatalf("register: %v", err)
	}
	return s
}

func getValue(t *testing.T, s *Server, path string) valueResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d body %q", path, rec.Code, rec.Body.String())
	}
	var vr valueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &vr); err != nil {
		t.Fatalf("GET %s: decode: %v body %q", path, err, rec.Body.String())
	}
	return vr
}

func put(t *testing.T, s *Server, path string, form url.Values) methodResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s: status %d body %q", path, rec.Code, rec.Body.String())
	}
	var mr methodResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &mr); err != nil {
		t.Fatalf("PUT %s: decode: %v", path, err)
	}
	return mr
}

func TestCommonAndCameraMembers(t *testing.T) {
	s := newTestServer(t)

	// newTestServer marks the device connected so operational members are reachable.
	if v := getValue(t, s, "/api/v1/camera/0/connected").Value; v != true {
		t.Errorf("connected = %v, want true", v)
	}
	// Numbers decode from JSON as float64.
	if v := getValue(t, s, "/api/v1/camera/0/cameraxsize").Value; v != float64(100) {
		t.Errorf("cameraxsize = %v, want 100", v)
	}
	if v := getValue(t, s, "/api/v1/camera/0/canabortexposure").Value; v != true {
		t.Errorf("canabortexposure = %v, want true", v)
	}
	// Unsupported (Base default) member returns NotImplemented in-band, HTTP 200.
	if n := getValue(t, s, "/api/v1/camera/0/ccdtemperature").ErrorNumber; n != ErrNumNotImplemented {
		t.Errorf("ccdtemperature ErrorNumber = %#x, want %#x", n, ErrNumNotImplemented)
	}
	// On error the Value field is OMITTED entirely (not null) — the typed Value
	// has a concrete schema type, so null would violate it. (Spec: AlpacaResponse
	// + typed responses.)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/camera/0/ccdtemperature", nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), `"Value"`) {
			t.Errorf("error response should omit Value, got %s", rec.Body.String())
		}
	}
}

func TestConnectAndTransactionEcho(t *testing.T) {
	s := newTestServer(t)
	mr := put(t, s, "/api/v1/camera/0/connected", url.Values{
		"Connected": {"true"}, "ClientTransactionID": {"77"},
	})
	if mr.ErrorNumber != 0 {
		t.Fatalf("connect error: %d %s", mr.ErrorNumber, "")
	}
	if mr.ClientTransactionID != 77 {
		t.Errorf("ClientTransactionID echo = %d, want 77", mr.ClientTransactionID)
	}
	if mr.ServerTransactionID == 0 {
		t.Errorf("ServerTransactionID should be monotonic non-zero")
	}
	if v := getValue(t, s, "/api/v1/camera/0/connected").Value; v != true {
		t.Errorf("connected after connect = %v, want true", v)
	}
}

func TestPutGetGainRoundTrip(t *testing.T) {
	s := newTestServer(t)
	put(t, s, "/api/v1/camera/0/gain", url.Values{"Gain": {"123"}})
	if v := getValue(t, s, "/api/v1/camera/0/gain").Value; v != float64(123) {
		t.Errorf("gain = %v, want 123", v)
	}
}

func TestImageBytes(t *testing.T) {
	s := newTestServer(t)
	put(t, s, "/api/v1/camera/0/startexposure", url.Values{"Duration": {"1.0"}, "Light": {"true"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/camera/0/imagearray", nil)
	req.Header.Set("Accept", ImageBytesMIME)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != ImageBytesMIME {
		t.Fatalf("Content-Type = %q, want %q", ct, ImageBytesMIME)
	}
	body := rec.Body.Bytes()
	if len(body) < imageBytesMetadataLen {
		t.Fatalf("body too short: %d", len(body))
	}
	le := binary.LittleEndian
	if v := le.Uint32(body[0:]); v != imageBytesMetadataVersion {
		t.Errorf("metadata version = %d, want %d", v, imageBytesMetadataVersion)
	}
	if v := le.Uint32(body[20:]); ImageElementType(v) != ImgInt32 {
		t.Errorf("element type = %d, want Int32", v)
	}
	if v := le.Uint32(body[24:]); ImageElementType(v) != ImgUInt16 {
		t.Errorf("transmission type = %d, want UInt16", v)
	}
	if v := le.Uint32(body[28:]); v != 2 {
		t.Errorf("rank = %d, want 2", v)
	}
	if v := le.Uint32(body[32:]); v != 100 {
		t.Errorf("dim1 = %d, want 100", v)
	}
	if v := le.Uint32(body[36:]); v != 50 {
		t.Errorf("dim2 = %d, want 50", v)
	}
	if got := len(body) - imageBytesMetadataLen; got != 100*50*2 {
		t.Errorf("pixel bytes = %d, want %d", got, 100*50*2)
	}
}

func TestManagement(t *testing.T) {
	s := newTestServer(t)
	vr := getValue(t, s, "/management/v1/configureddevices")
	arr, ok := vr.Value.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("configureddevices = %#v", vr.Value)
	}
	dev := arr[0].(map[string]any)
	if dev["DeviceType"] != "camera" || dev["UniqueID"] != "fake-guid-1" {
		t.Errorf("device entry = %#v", dev)
	}
}
