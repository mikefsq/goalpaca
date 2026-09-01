package server

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
	busy        bool
	aborted     bool
	movedAxis   bool
	movedAxisAt float64
}

func (t *fakeTelescope) Busy() bool { return t.busy }
func (t *fakeTelescope) AbortSlew() error {
	t.aborted = true
	return nil
}
func (t *fakeTelescope) CanMoveAxis(TelescopeAxis) bool { return true }

// AxisRates has to advertise the rate the test uses: MoveAxis is gated on the rate falling inside
// one of the device's published ranges, and BaseTelescope publishes none — so without this the
// request is refused with InvalidValue before the busy gate is ever reached.
func (t *fakeTelescope) AxisRates(TelescopeAxis) []AxisRate {
	return []AxisRate{{Minimum: 0.1, Maximum: 5}}
}
func (t *fakeTelescope) MoveAxis(_ TelescopeAxis, rate float64) error {
	t.movedAxisAt, t.movedAxis = rate, true
	return nil
}

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

	// MoveAxis must also reach a busy device, and this assertion was BACKWARDS until the
	// exemption that makes it true was added.
	//
	// It used to require InvalidOperation, on the reading that MoveAxis is an initiator like any
	// other. interruptMembers now exempts it, with the reason recorded there: ASCOM defines rate 0
	// as the stop for an axis already in motion (ConformU stops exactly that way), and a rate
	// CHANGE mid-move is legal too. A MoveAxis that cannot reach a moving mount is a mount that
	// cannot be slowed or stopped through the axis it is moving on.
	//
	// The test kept failing rather than being noticed because it also needed CanMoveAxis on the
	// fake — BaseTelescope answers false — so the request was refused with NotImplemented and the
	// busy gate never came into it either way.
	if mr := put(t, s, "/api/v1/telescope/0/moveaxis", url.Values{"Axis": {"0"}, "Rate": {"1.5"}}); mr.ErrorNumber != 0 {
		t.Errorf("busy moveaxis ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
	if !tel.movedAxis {
		t.Error("busy moveaxis did not reach the device, so a moving mount cannot be slowed or stopped")
	}
	if tel.movedAxisAt != 1.5 {
		t.Errorf("moveaxis reached the device at rate %g, want 1.5", tel.movedAxisAt)
	}
}

// fakeCoverCal is a minimal Busyable CoverCalibrator for the interrupt gating test.
type fakeCoverCal struct {
	BaseCoverCalibrator
	busy    bool
	halted  bool
	calOff  bool
	covOpen bool
}

func (c *fakeCoverCal) Busy() bool                        { return c.busy }
func (c *fakeCoverCal) CoverState() CoverStatus           { return CoverClosed }
func (c *fakeCoverCal) CalibratorState() CalibratorStatus { return CalibratorOff }
func (c *fakeCoverCal) HaltCover() error {
	c.halted = true
	return nil
}
func (c *fakeCoverCal) CalibratorOff() error {
	c.calOff = true
	return nil
}
func (c *fakeCoverCal) OpenCover() error {
	c.covOpen = true
	return nil
}

func newFakeCoverCal() *fakeCoverCal {
	cc := &fakeCoverCal{}
	cc.ID = "fake-covercal-1"
	cc.DevName = "FakeCoverCal"
	cc.IfaceVer = 2
	return cc
}

// TestBusyGatingCoverCalibrator verifies HaltCover and CalibratorOff bypass the
// Busy gate — they are the interrupts that end cover motion / a calibrator
// ramp — while OpenCover (an initiator) stays gated with InvalidOperation.
func TestBusyGatingCoverCalibrator(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	cc := newFakeCoverCal()
	cc.MarkConnected()
	cc.busy = true
	if err := s.Register(CoverCalibratorType, 0, cc); err != nil {
		t.Fatalf("register: %v", err)
	}

	if mr := put(t, s, "/api/v1/covercalibrator/0/haltcover", url.Values{}); mr.ErrorNumber != 0 {
		t.Errorf("busy haltcover ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
	if !cc.halted {
		t.Error("busy haltcover did not reach the device")
	}
	if mr := put(t, s, "/api/v1/covercalibrator/0/calibratoroff", url.Values{}); mr.ErrorNumber != 0 {
		t.Errorf("busy calibratoroff ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
	if !cc.calOff {
		t.Error("busy calibratoroff did not reach the device")
	}

	// OpenCover (an initiator) stays gated while busy.
	if mr := put(t, s, "/api/v1/covercalibrator/0/opencover", url.Values{}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("busy opencover ErrorNumber = %#x, want InvalidOperation %#x", mr.ErrorNumber, ErrNumInvalidOperation)
	}
	if cc.covOpen {
		t.Error("busy opencover reached the device; the gate should have rejected it")
	}
}

// fakeAsyncSwitch is a minimal Busyable Switch for the async-interrupt gating test.
type fakeAsyncSwitch struct {
	BaseSwitch
	busy      bool
	cancelled bool
}

func (f *fakeAsyncSwitch) Busy() bool                 { return f.busy }
func (f *fakeAsyncSwitch) MaxSwitch() int             { return 1 }
func (f *fakeAsyncSwitch) CanAsync(int) (bool, error) { return true, nil }
func (f *fakeAsyncSwitch) CancelAsync(int) error {
	f.cancelled = true
	return nil
}
func (f *fakeAsyncSwitch) SetAsync(int, bool) error { return nil }

func newFakeSwitch() *fakeAsyncSwitch {
	sw := &fakeAsyncSwitch{}
	sw.ID = "fake-switch-1"
	sw.DevName = "FakeSwitch"
	sw.IfaceVer = 3
	return sw
}

// TestBusyGatingSwitchAsync verifies CancelAsync bypasses the Busy gate — it is
// ISwitchV3's interrupt for an in-flight async state change — while SetAsync
// (an initiator) stays gated with InvalidOperation.
func TestBusyGatingSwitchAsync(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	sw := newFakeSwitch()
	sw.MarkConnected()
	sw.busy = true
	if err := s.Register(SwitchType, 0, sw); err != nil {
		t.Fatalf("register: %v", err)
	}

	if mr := put(t, s, "/api/v1/switch/0/cancelasync", url.Values{"Id": {"0"}}); mr.ErrorNumber != 0 {
		t.Errorf("busy cancelasync ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
	if !sw.cancelled {
		t.Error("busy cancelasync did not reach the device")
	}

	// SetAsync (an initiator) stays gated while busy.
	if mr := put(t, s, "/api/v1/switch/0/setasync", url.Values{"Id": {"0"}, "State": {"true"}}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("busy setasync ErrorNumber = %#x, want InvalidOperation %#x", mr.ErrorNumber, ErrNumInvalidOperation)
	}
}
