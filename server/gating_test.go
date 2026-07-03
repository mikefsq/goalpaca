package alpacadev

import (
	"net/url"
	"testing"
)

// TestNotConnectedGating verifies operational members return NotConnected
// (0x407) while disconnected, that the introspection/connection members stay
// available, and that operational members work once connected.
func TestNotConnectedGating(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	if err := s.Register(CameraType, 0, newFakeCamera()); err != nil { // NOT connected
		t.Fatalf("register: %v", err)
	}

	// Operational GET is gated.
	if n := getValue(t, s, "/api/v1/camera/0/cameraxsize").ErrorNumber; n != ErrNumNotConnected {
		t.Errorf("disconnected cameraxsize ErrorNumber = %#x, want NotConnected %#x", n, ErrNumNotConnected)
	}
	// DeviceState is gated.
	if n := getValue(t, s, "/api/v1/camera/0/devicestate").ErrorNumber; n != ErrNumNotConnected {
		t.Errorf("disconnected devicestate ErrorNumber = %#x, want NotConnected", n)
	}
	// Operational PUT is gated.
	if mr := put(t, s, "/api/v1/camera/0/gain", url.Values{"Gain": {"5"}}); mr.ErrorNumber != ErrNumNotConnected {
		t.Errorf("disconnected set gain ErrorNumber = %#x, want NotConnected", mr.ErrorNumber)
	}

	// Exempt introspection members stay available while disconnected.
	if vr := getValue(t, s, "/api/v1/camera/0/name"); vr.ErrorNumber != 0 || vr.Value != "FakeCam" {
		t.Errorf("disconnected name = %#v (err %#x), want FakeCam/0", vr.Value, vr.ErrorNumber)
	}
	if n := getValue(t, s, "/api/v1/camera/0/interfaceversion").ErrorNumber; n != 0 {
		t.Errorf("disconnected interfaceversion gated unexpectedly: %#x", n)
	}
	if n := getValue(t, s, "/api/v1/camera/0/connected").ErrorNumber; n != 0 {
		t.Errorf("disconnected 'connected' query gated unexpectedly: %#x", n)
	}

	// Connect via the exempt PUT, then operational members work.
	if mr := put(t, s, "/api/v1/camera/0/connected", url.Values{"Connected": {"true"}}); mr.ErrorNumber != 0 {
		t.Fatalf("connect failed: %#x", mr.ErrorNumber)
	}
	if vr := getValue(t, s, "/api/v1/camera/0/cameraxsize"); vr.ErrorNumber != 0 || vr.Value != float64(100) {
		t.Errorf("after connect cameraxsize = %#v (err %#x), want 100/0", vr.Value, vr.ErrorNumber)
	}
}

// TestBusyGating verifies that while a Busyable device reports Busy(), mutating
// PUTs are rejected with InvalidOperation, while reads and interrupt members
// (abortexposure) still work.
func TestBusyGating(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	cam := newFakeCamera()
	cam.MarkConnected()
	cam.busy = true
	if err := s.Register(CameraType, 0, cam); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Mutating PUT is rejected while busy.
	if mr := put(t, s, "/api/v1/camera/0/gain", url.Values{"Gain": {"5"}}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("busy set gain ErrorNumber = %#x, want InvalidOperation %#x", mr.ErrorNumber, ErrNumInvalidOperation)
	}
	if mr := put(t, s, "/api/v1/camera/0/startexposure", url.Values{"Duration": {"1"}, "Light": {"true"}}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("busy startexposure ErrorNumber = %#x, want InvalidOperation", mr.ErrorNumber)
	}

	// Reads are NOT gated by Busy.
	if vr := getValue(t, s, "/api/v1/camera/0/cameraxsize"); vr.ErrorNumber != 0 {
		t.Errorf("busy cameraxsize gated unexpectedly: %#x", vr.ErrorNumber)
	}

	// Interrupt members work while busy.
	if mr := put(t, s, "/api/v1/camera/0/abortexposure", url.Values{}); mr.ErrorNumber != 0 {
		t.Errorf("busy abortexposure ErrorNumber = %#x, want success", mr.ErrorNumber)
	}

	// Command* passthroughs are NOT Busy-gated: a raw vendor command (here a
	// not-implemented stub → 0x400) reaches the device instead of being rejected with
	// InvalidOperation, so a plugin's read-only queries work mid-slew (the 10Micron
	// meridian-limit / alignment-model reads the NINA plugin needs over Alpaca).
	if mr := put(t, s, "/api/v1/camera/0/commandstring", url.Values{"Command": {":X#"}, "Raw": {"true"}}); mr.ErrorNumber == ErrNumInvalidOperation {
		t.Errorf("busy commandstring was Busy-gated (%#x); passthroughs must not be", mr.ErrorNumber)
	}

	// Once idle, mutating PUTs are allowed again.
	cam.busy = false
	if mr := put(t, s, "/api/v1/camera/0/gain", url.Values{"Gain": {"5"}}); mr.ErrorNumber != 0 {
		t.Errorf("idle set gain ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
}

// fakeTelescope is a minimal Busyable telescope for the motion-interrupt gating test.
type fakeTelescope struct {
	BaseTelescope
	busy    bool
	aborted bool
}

func (t *fakeTelescope) Busy() bool { return t.busy }
func (t *fakeTelescope) AbortSlew() error {
	t.aborted = true
	return nil
}
func (t *fakeTelescope) MoveAxis(TelescopeAxis, float64) error { return nil }

func newFakeTelescope() *fakeTelescope {
	tel := &fakeTelescope{}
	tel.ID = "fake-scope-1"
	tel.DevName = "FakeScope"
	tel.IfaceVer = 4
	return tel
}

// TestBusyGatingTelescopeMotion verifies AbortSlew halts a busy (slewing) mount —
// it must bypass the Busy gate, since it is THE interrupt that ends the motion —
// while MoveAxis (a motion INITIATOR) stays gated with InvalidOperation.
func TestBusyGatingTelescopeMotion(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	tel := newFakeTelescope()
	tel.MarkConnected()
	tel.busy = true
	if err := s.Register(TelescopeType, 0, tel); err != nil {
		t.Fatalf("register: %v", err)
	}

	// AbortSlew must work while busy and actually reach the device.
	if mr := put(t, s, "/api/v1/telescope/0/abortslew", url.Values{}); mr.ErrorNumber != 0 {
		t.Errorf("busy abortslew ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
	if !tel.aborted {
		t.Error("busy abortslew did not reach the device")
	}

	// MoveAxis (an initiator) stays gated while busy.
	if mr := put(t, s, "/api/v1/telescope/0/moveaxis", url.Values{"Axis": {"0"}, "Rate": {"1.5"}}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("busy moveaxis ErrorNumber = %#x, want InvalidOperation %#x", mr.ErrorNumber, ErrNumInvalidOperation)
	}
}
