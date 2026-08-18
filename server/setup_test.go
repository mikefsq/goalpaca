package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// configurableFocuser is a minimal device that opts into the Configurable
// interface, used to exercise the /setup form end to end.
type configurableFocuser struct {
	BaseDevice
	mu       sync.Mutex
	stepSize string
	reversed bool
	serial   string // stands in for a host-pinnable field
	pinned   bool   // when true, "serial" is host-pinned (Locked)
}

func (d *configurableFocuser) SettingsForm() []SettingField {
	d.mu.Lock()
	defer d.mu.Unlock()
	f := []SettingField{
		{Name: "stepsize", Label: "Step size (microns)", Type: "number", Value: d.stepSize},
		{Name: "reversed", Label: "Reverse direction", Type: "checkbox", Value: boolStr(d.reversed)},
		{Name: "serial", Label: "Serial", Type: "text", Value: d.serial},
	}
	if d.pinned {
		f[2].Locked = true
		f[2].Source = "set in hurd.conf"
	}
	return f
}

func (d *configurableFocuser) ApplySettings(v map[string]string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := v["stepsize"]; ok && v["stepsize"] == "" {
		return errBadStep
	}
	if s, ok := v["stepsize"]; ok {
		d.stepSize = s
	}
	if _, ok := v["reversed"]; ok {
		d.reversed = v["reversed"] == "true"
	}
	// Apply only keys given: a pinned "serial" never reaches here, so it is
	// left untouched (host owns it).
	if s, ok := v["serial"]; ok {
		d.serial = s
	}
	return nil
}

var errBadStep = &settingErr{"step size is required"}

type settingErr struct{ s string }

func (e *settingErr) Error() string { return e.s }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func newTestFocuser() *configurableFocuser {
	d := &configurableFocuser{stepSize: "5"}
	d.ID, d.DevName, d.Desc = "focus-1", "Test Focuser", "a focuser"
	d.IfaceVer = 4
	return d
}

func setupTestServer(t *testing.T, dev Device) *Server {
	t.Helper()
	s := New(Config{ServerName: "Test Rig", Manufacturer: "acme", ManufacturerVersion: "1.0"})
	// Use a type whose static interface our stub satisfies. Focuser requires the
	// Focuser methods, which the stub lacks, so register under a non-typed name.
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	return s
}

// customType is an unrecognised device type: implementsType returns true, so
// any Device registers. The wire path is /setup/v1/customfocuser/0/setup.
const customType DeviceType = "customfocuser"

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if method == http.MethodPost {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	s.route(w, r)
	return w
}

func TestSetupServerPage(t *testing.T) {
	s := setupTestServer(t, newTestFocuser())
	w := do(t, s, http.MethodGet, "/setup", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Test Rig", "Test Focuser", "/setup/v1/customfocuser/0/setup"} {
		if !strings.Contains(body, want) {
			t.Errorf("server page missing %q", want)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

func TestSetupDevicePageAndApply(t *testing.T) {
	dev := newTestFocuser()
	s := setupTestServer(t, dev)

	// GET renders the form with current values.
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `name="stepsize"`) {
		t.Fatalf("form missing stepsize field:\n%s", w.Body.String())
	}

	// POST applies new values.
	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=12&reversed=true")
	if w.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Settings applied.") {
		t.Errorf("expected success banner:\n%s", w.Body.String())
	}
	if dev.stepSize != "12" || !dev.reversed {
		t.Errorf("apply did not take effect: stepSize=%q reversed=%v", dev.stepSize, dev.reversed)
	}

	// POST with a rejected value shows the driver's error and does not change state.
	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=&reversed=false")
	if !strings.Contains(w.Body.String(), "step size is required") {
		t.Errorf("expected validation error banner:\n%s", w.Body.String())
	}
	if dev.stepSize != "12" {
		t.Errorf("rejected apply mutated state: stepSize=%q", dev.stepSize)
	}
}

func TestSetupNotConfigurable(t *testing.T) {
	// A device that does not implement Configurable still gets a 200 page.
	dev := &BaseDevice{ID: "x", DevName: "Plain", Desc: "d"}
	s := New(Config{ServerName: "Rig"})
	if err := s.Register(customType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no configurable settings") {
		t.Errorf("expected not-configurable message:\n%s", w.Body.String())
	}
}

func TestSetupInvalidURLsReturn403(t *testing.T) {
	s := setupTestServer(t, newTestFocuser())
	cases := []string{
		"/setup/garbage",
		"/setup/v1/customfocuser/0/notsetup",
		"/setup/v1/customfocuser/notanumber/setup",
		"/setup/v1/customfocuser/9/setup", // no such device number
		"/setup/v1/nosuchtype/0/setup",
	}
	for _, path := range cases {
		w := do(t, s, http.MethodGet, path, "")
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", path, w.Code)
		}
	}
}

func TestSetupLockedFieldDisabledAndUneditable(t *testing.T) {
	dev := newTestFocuser()
	dev.serial = "ABC123"
	dev.pinned = true
	s := setupTestServer(t, dev)

	// GET: the pinned field renders disabled with its source note.
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	body := w.Body.String()
	if !strings.Contains(body, `name="serial"`) || !strings.Contains(body, "disabled") {
		t.Fatalf("locked field not rendered disabled:\n%s", body)
	}
	if !strings.Contains(body, "set in hurd.conf") {
		t.Errorf("missing source note:\n%s", body)
	}

	// POST trying to change the locked field (hand-crafted body) is ignored,
	// while an editable field on the same form still applies.
	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=7&serial=HACKED")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if dev.serial != "ABC123" {
		t.Errorf("locked field was changed via POST: %q", dev.serial)
	}
	if dev.stepSize != "7" {
		t.Errorf("editable field not applied: %q", dev.stepSize)
	}
}

func TestSetupServerPageRejectsPost(t *testing.T) {
	s := setupTestServer(t, newTestFocuser())
	w := do(t, s, http.MethodPost, "/setup", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /setup status = %d, want 405", w.Code)
	}
}
