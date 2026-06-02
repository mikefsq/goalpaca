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

	// Once idle, mutating PUTs are allowed again.
	cam.busy = false
	if mr := put(t, s, "/api/v1/camera/0/gain", url.Values{"Gain": {"5"}}); mr.ErrorNumber != 0 {
		t.Errorf("idle set gain ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
}
