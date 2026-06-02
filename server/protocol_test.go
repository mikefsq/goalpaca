package alpacadev

// Alpaca protocol-conformance tests, derived from ConformU's
// AlpacaProtocolTestManager (the device-agnostic "Check Alpaca Protocol" mode).
// These exercise the wire contract independent of any device's semantics:
// URL structure, HTTP methods, common members, the ClientID/ClientTransactionID
// matrix, status codes, error envelope, and field casing. Device-behaviour
// conformance is covered separately by running ConformU against the simulators.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// --- raw request helpers (status/header level; the getValue/put helpers in
// server_test.go fatal on non-200, which several protocol checks expect). ---

func rawGet(s *Server, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func rawReq(s *Server, method, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func is4xx(code int) bool { return code >= 400 && code <= 499 }

func decodeValue(t *testing.T, rec *httptest.ResponseRecorder) valueResponse {
	t.Helper()
	var vr valueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &vr); err != nil {
		t.Fatalf("decode: %v body=%q", err, rec.Body.String())
	}
	return vr
}

// Bad URL structure must be rejected (4XX). Mirrors ConformU TestCommon URL checks.
func TestProtocolBadURLsRejected(t *testing.T) {
	s := newTestServer(t) // camera registered at device 0, connected
	bad := []string{
		"/apx/v1/camera/0/description",        // bad base element
		"/api/1/camera/0/description",         // missing "v"
		"/api/v/camera/0/description",         // version has no number
		"/api/V1/camera/0/description",        // capital V
		"/api/v2/camera/0/description",        // wrong version
		"/api/v1/CAMERA/0/description",        // device type must be lowercase
		"/api/v1/baddevicetype/0/description", // unknown device type
		"/api/v1/camera/-1/description",       // device number -1
		"/api/v1/camera/99999/description",    // nonexistent device number
		"/api/v1/camera/A/description",        // non-numeric device number
		"/api/v1/camera/0/descrip",            // unknown member name
	}
	for _, p := range bad {
		rec := rawGet(s, p)
		if !is4xx(rec.Code) {
			t.Errorf("GET %s: status %d, want a 4XX rejection", p, rec.Code)
		}
	}
}

// Only GET and PUT are allowed; other methods are rejected.
func TestProtocolBadHTTPMethods(t *testing.T) {
	s := newTestServer(t)
	for _, m := range []string{http.MethodPost, http.MethodDelete} {
		rec := rawReq(s, m, "/api/v1/camera/0/connected", url.Values{"Connected": {"true"}})
		if !is4xx(rec.Code) {
			t.Errorf("%s /connected: status %d, want 4XX", m, rec.Code)
		}
	}
}

// Common GET members return a well-formed 200 JSON envelope.
func TestProtocolCommonGetMembers(t *testing.T) {
	s := newTestServer(t)
	members := []string{
		"connected", "description", "driverinfo", "driverversion",
		"interfaceversion", "name", "supportedactions", "devicestate",
	}
	for _, m := range members {
		rec := rawGet(s, "/api/v1/camera/0/"+m)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200 (body %q)", m, rec.Code, rec.Body.String())
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("GET %s: Content-Type %q, want application/json", m, ct)
		}
		vr := decodeValue(t, rec)
		if vr.ErrorNumber != 0 {
			t.Errorf("GET %s: ErrorNumber %#x, want 0", m, vr.ErrorNumber)
		}
		if vr.ServerTransactionID == 0 {
			t.Errorf("GET %s: ServerTransactionID = 0, want non-zero", m)
		}
	}
}

// Member names are case-insensitive (ConformU requires this).
func TestProtocolMemberNameCaseInsensitive(t *testing.T) {
	s := newTestServer(t)
	for _, m := range []string{"connected", "Connected", "CONNECTED"} {
		rec := rawGet(s, "/api/v1/camera/0/"+m)
		if rec.Code != http.StatusOK {
			t.Errorf("GET member %q: status %d, want 200", m, rec.Code)
		}
	}
}

// A malformed PUT parameter value is a protocol error -> HTTP 400.
func TestProtocolBadPutValue(t *testing.T) {
	s := newTestServer(t)
	for _, v := range []string{"", "123456", "asdqwe"} {
		rec := rawReq(s, http.MethodPut, "/api/v1/camera/0/connected",
			url.Values{"Connected": {v}, "ClientTransactionID": {"5"}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT connected=%q: status %d, want 400", v, rec.Code)
		}
	}
}

// ClientTransactionID round-trips when valid, echoes 0 when missing/invalid;
// param names are case-insensitive, order-independent, and spurious params are
// ignored. Mirrors ConformU's ClientID/ClientTransactionID matrix.
func TestProtocolClientTransactionIDEcho(t *testing.T) {
	s := newTestServer(t)
	const path = "/api/v1/camera/0/connected"
	cases := []struct {
		name   string
		query  string
		expect uint32
	}{
		{"both ok", "ClientID=99&ClientTransactionID=42", 42},
		{"extra spurious param", "ClientID=99&ClientTransactionID=42&ExtraParameter=ExtraValue", 42},
		{"lowercase param names", "clientid=99&clienttransactionid=42", 42},
		{"reordered", "ClientTransactionID=42&ClientID=99", 42},
		{"empty CTID", "ClientID=99&ClientTransactionID=", 0},
		{"whitespace CTID", "ClientID=99&ClientTransactionID=%20%20%20", 0},
		{"negative CTID", "ClientID=99&ClientTransactionID=-12345", 0},
		{"non-numeric CTID", "ClientID=99&ClientTransactionID=qweqwe", 0},
		{"missing CTID", "ClientID=99", 0},
	}
	for _, c := range cases {
		rec := rawGet(s, path+"?"+c.query)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", c.name, rec.Code)
			continue
		}
		vr := decodeValue(t, rec)
		if vr.ClientTransactionID != c.expect {
			t.Errorf("%s: ClientTransactionID echo = %d, want %d", c.name, vr.ClientTransactionID, c.expect)
		}
	}
}

// ServerTransactionID is present and strictly increasing across requests.
func TestProtocolServerTransactionIDMonotonic(t *testing.T) {
	s := newTestServer(t)
	first := decodeValue(t, rawGet(s, "/api/v1/camera/0/name")).ServerTransactionID
	second := decodeValue(t, rawGet(s, "/api/v1/camera/0/name")).ServerTransactionID
	if first == 0 || second <= first {
		t.Errorf("ServerTransactionID not strictly increasing: %d then %d", first, second)
	}
}

// The JSON envelope uses the exact ASCOM field casing.
func TestProtocolResponseFieldCasing(t *testing.T) {
	s := newTestServer(t)
	body := rawGet(s, "/api/v1/camera/0/name").Body.String()
	for _, f := range []string{
		`"Value"`, `"ClientTransactionID"`, `"ServerTransactionID"`, `"ErrorNumber"`, `"ErrorMessage"`,
	} {
		if !strings.Contains(body, f) {
			t.Errorf("success response missing exact-cased field %s; body=%s", f, body)
		}
	}
}
