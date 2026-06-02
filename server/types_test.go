package alpacadev

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Fakes embedding the Base* helpers, overriding a few members.

type fakeFocuser struct {
	BaseFocuser
	pos int
}

func (f *fakeFocuser) Absolute() bool         { return true }
func (f *fakeFocuser) Position() (int, error) { return f.pos, nil }
func (f *fakeFocuser) Move(p int) error       { f.pos = p; return nil }

type fakeSwitch struct{ BaseSwitch }

func (s *fakeSwitch) MaxSwitch() int { return 3 }
func (s *fakeSwitch) GetSwitchName(id int) (string, error) {
	return fmt.Sprintf("SW%d", id), nil
}

type fakeSafety struct{ BaseSafetyMonitor }

func (s *fakeSafety) IsSafe() bool { return true }

func multiTypeServer(t *testing.T) *Server {
	t.Helper()
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	must := func(err error) {
		if err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	foc := &fakeFocuser{}
	sw := &fakeSwitch{}
	safe := &fakeSafety{}
	tel := &struct{ BaseTelescope }{}
	for _, d := range []interface {
		MarkConnected()
	}{foc, sw, safe, tel} {
		d.MarkConnected() // operational members are gated until connected
	}
	must(s.Register(FocuserType, 0, foc))
	must(s.Register(SwitchType, 0, sw))
	must(s.Register(SafetyMonitorType, 0, safe))
	must(s.Register(TelescopeType, 0, tel))
	return s
}

func TestFocuserMoveAndPosition(t *testing.T) {
	s := multiTypeServer(t)
	if v := getValue(t, s, "/api/v1/focuser/0/absolute").Value; v != true {
		t.Errorf("absolute = %v, want true", v)
	}
	put(t, s, "/api/v1/focuser/0/move", url.Values{"Position": {"4200"}})
	if v := getValue(t, s, "/api/v1/focuser/0/position").Value; v != float64(4200) {
		t.Errorf("position = %v, want 4200", v)
	}
}

func TestSwitchIdParam(t *testing.T) {
	s := multiTypeServer(t)
	if v := getValue(t, s, "/api/v1/switch/0/maxswitch").Value; v != float64(3) {
		t.Errorf("maxswitch = %v, want 3", v)
	}
	// Id parameter, case-insensitive member name handled by router lowercasing.
	if v := getValue(t, s, "/api/v1/switch/0/getswitchname?Id=2").Value; v != "SW2" {
		t.Errorf("getswitchname(2) = %v, want SW2", v)
	}
	// Missing required Id -> HTTP 400 text/plain (Alpaca spec responses/400),
	// distinct from a device-level in-band InvalidValue (0x401).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/switch/0/getswitchname", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("getswitchname no Id: status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("getswitchname no Id: Content-Type = %q, want text/plain", ct)
	}
}

func TestSafetyMonitor(t *testing.T) {
	s := multiTypeServer(t)
	if v := getValue(t, s, "/api/v1/safetymonitor/0/issafe").Value; v != true {
		t.Errorf("issafe = %v, want true", v)
	}
}

func TestTelescopeParamGetterAndUnsupportedSet(t *testing.T) {
	s := multiTypeServer(t)
	// Param getter on the Base default.
	if v := getValue(t, s, "/api/v1/telescope/0/canmoveaxis?Axis=0").Value; v != false {
		t.Errorf("canmoveaxis(0) = %v, want false", v)
	}
	// Unsupported setter returns NotImplemented in-band (HTTP 200).
	mr := put(t, s, "/api/v1/telescope/0/tracking", url.Values{"Tracking": {"true"}})
	if mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("set tracking ErrorNumber = %#x, want %#x", mr.ErrorNumber, ErrNumNotImplemented)
	}
}
