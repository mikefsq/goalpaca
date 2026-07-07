package client

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/mikefsq/goalpaca/server"
)

// serve starts the real goalpaca server (over httptest) hosting dev at number 0.
func serve(t *testing.T, dt server.DeviceType, dev server.Device) *httptest.Server {
	t.Helper()
	srv := server.New(server.Config{Discovery: server.DiscoveryConfig{Mode: server.DiscoveryOff}})
	if err := srv.Register(dt, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

// fakeSwitch exercises the Id-indexed Switch members.
type fakeSwitch struct {
	server.BaseSwitch
	vals map[int]float64
}

func (s *fakeSwitch) MaxSwitch() int                         { return 2 }
func (s *fakeSwitch) MaxSwitchValue(int) (float64, error)    { return 10, nil }
func (s *fakeSwitch) CanWrite(int) (bool, error)             { return true, nil }
func (s *fakeSwitch) GetSwitchName(id int) (string, error)   { return fmt.Sprintf("SW%d", id), nil }
func (s *fakeSwitch) GetSwitchValue(id int) (float64, error) { return s.vals[id], nil }
func (s *fakeSwitch) SetSwitchValue(id int, v float64) error { s.vals[id] = v; return nil }

func TestSwitchClient(t *testing.T) {
	dev := &fakeSwitch{vals: map[int]float64{}}
	dev.DevName = "Switches"
	dev.IfaceVer = 3
	ts := serve(t, server.SwitchType, dev)
	sc := NewSwitch(ts.URL, 0)
	if err := sc.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if n, err := sc.MaxSwitch(); err != nil || n != 2 {
		t.Fatalf("MaxSwitch() = %d, %v; want 2", n, err)
	}
	if name, err := sc.GetSwitchName(1); err != nil || name != "SW1" {
		t.Fatalf("GetSwitchName(1) = %q, %v; want SW1", name, err)
	}
	if err := sc.SetSwitchValue(0, 3.5); err != nil {
		t.Fatalf("SetSwitchValue: %v", err)
	}
	if v, err := sc.GetSwitchValue(0); err != nil || v != 3.5 {
		t.Fatalf("GetSwitchValue(0) = %v, %v; want 3.5", v, err)
	}
}

// fakeTelescope exercises enum decoding and parameterized getters.
type fakeTelescope struct{ server.BaseTelescope }

func (f *fakeTelescope) CanMoveAxis(axis server.TelescopeAxis) bool {
	return axis == server.AxisPrimary
}
func (f *fakeTelescope) TrackingRates() []server.DriveRate {
	return []server.DriveRate{server.DriveSidereal, server.DriveLunar}
}

func TestTelescopeClient(t *testing.T) {
	dev := &fakeTelescope{}
	dev.DevName = "Mount"
	dev.IfaceVer = 4
	ts := serve(t, server.TelescopeType, dev)
	tc := NewTelescope(ts.URL, 0)
	if err := tc.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if ok, err := tc.CanMoveAxis(server.AxisPrimary); err != nil || !ok {
		t.Fatalf("CanMoveAxis(primary) = %v, %v; want true", ok, err)
	}
	if ok, err := tc.CanMoveAxis(server.AxisSecondary); err != nil || ok {
		t.Fatalf("CanMoveAxis(secondary) = %v, %v; want false", ok, err)
	}
	rates, err := tc.TrackingRates()
	if err != nil || len(rates) != 2 || rates[1] != server.DriveLunar {
		t.Fatalf("TrackingRates() = %v, %v; want [sidereal lunar]", rates, err)
	}
	if am, err := tc.AlignmentMode(); err != nil || am != server.AlignGermanPolar {
		t.Fatalf("AlignmentMode() = %v, %v; want GermanPolar", am, err)
	}
	if err := tc.SetTracking(true); !errors.Is(err, server.ErrNotImplemented) {
		t.Fatalf("SetTracking(): want NotImplemented, got %v", err)
	}
}
