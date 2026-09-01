package conformance

import (
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	"github.com/mikefsq/goalpaca/server"
)

// focuserPositionTolerance is the arrival tolerance in steps. ConformU allows a
// small settling tolerance; the sim converges exactly but movement is sampled
// from the clock, so a few steps of slack avoids flakiness.
const focuserPositionTolerance = 5

// CheckFocuser runs the ConformU Focuser conformance checks against f. Ported
// from ConformU's FocuserTester (CheckProperties / CheckMethods): NotConnected
// gating, capability/property consistency and ranges, absolute Move arrival,
// out-of-range Move clamped gracefully to the limits, Halt, and TempComp read/write.
//
// The simulator is an absolute focuser (Absolute()==true), so relative-focuser
// behaviour (Move treated as a signed delta, MaxIncrement bounding a single
// relative step) is not exercised here. StepSize is read but not range-checked
// beyond being error-free, matching the sim's fixed value.
func CheckFocuser(t *testing.T, f *client.Focuser) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = f.SetConnected(false)
	if _, err := f.Position(); !errors.Is(err, server.ErrNotConnected) {
		t.Errorf("Position() while disconnected: want NotConnected, got %v", err)
	}
	if err := f.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// IsMoving - the focuser must be stationary at the start, before any Move.
	if m, err := f.IsMoving(); err != nil || m {
		t.Errorf("IsMoving() at start = %v, %v; want false", m, err)
	}

	// Absolute - required, and the sim is an absolute focuser.
	abs, err := f.Absolute()
	if err != nil {
		t.Errorf("Absolute(): %v", err)
	}
	if !abs {
		t.Errorf("Absolute() = false; sim is an absolute focuser, want true")
	}

	// MaxStep - required, must be positive.
	maxStep, err := f.MaxStep()
	if err != nil || maxStep <= 0 {
		t.Errorf("MaxStep() = %v, %v; want > 0", maxStep, err)
	}

	// MaxIncrement - required, must be at least 1 and not exceed MaxStep.
	if maxInc, err := f.MaxIncrement(); err != nil || maxInc < 1 || maxInc > maxStep {
		t.Errorf("MaxIncrement() = %v, %v; want [1, MaxStep=%d]", maxInc, err, maxStep)
	}

	// TempCompAvailable - required, just needs to be readable.
	tcAvail, err := f.TempCompAvailable()
	if err != nil {
		t.Errorf("TempCompAvailable(): %v", err)
	}

	// TempComp read - required. If TempComp is true, TempCompAvailable must be true.
	if tc, err := f.TempComp(); err != nil {
		t.Errorf("TempComp(): %v", err)
	} else if tc && !tcAvail {
		t.Errorf("TempComp() = true while TempCompAvailable() = false")
	}

	// Temperature - readable without error and within a sane range.
	if temp, err := f.Temperature(); err != nil || temp < -50 || temp > 50 {
		t.Errorf("Temperature() = %v, %v; want [-50, 50]", temp, err)
	}

	// StepSize - positive when implemented; NotImplemented is spec-legal
	// (IFocuserV3: a focuser that does not intrinsically know its step size
	// must throw rather than guess).
	if ss, err := f.StepSize(); err != nil {
		if !errors.Is(err, server.ErrNotImplemented) {
			t.Errorf("StepSize() error = %v; want a value or NotImplemented", err)
		}
	} else if ss <= 0 {
		t.Errorf("StepSize() = %v; want > 0", ss)
	}

	// Position - in [0, MaxStep] for an absolute focuser.
	if p, err := f.Position(); err != nil || p < 0 || p > maxStep {
		t.Errorf("Position() = %v, %v; want [0, %d]", p, err, maxStep)
	}

	// Move happy path: absolute moves to known targets, verify arrival.
	for _, target := range []int{1000, 20000} {
		if err := f.Move(target); err != nil {
			t.Errorf("Move(%d): %v", target, err)
			continue
		}
		focuserWaitStopped(t, f)
		if p, err := f.Position(); err != nil || focuserAbsDiff(p, target) > focuserPositionTolerance {
			t.Errorf("Move(%d): Position = %v, %v; want ~%d", target, p, err, target)
		}
	}

	// Move to the MaxStep boundary: must arrive exactly at the upper limit.
	if err := f.Move(maxStep); err != nil {
		t.Errorf("Move(MaxStep=%d): %v", maxStep, err)
	} else {
		focuserWaitStopped(t, f)
		if p, err := f.Position(); err != nil || p != maxStep {
			t.Errorf("Move(MaxStep=%d): Position = %v, %v; want %d", maxStep, p, err, maxStep)
		}
	}

	// Move out-of-range: a driver may clamp gracefully to the limit (ConformU
	// "Move - Below 0" / "Move - Above MaxStep") or reject with InvalidValue,
	// leaving the position untouched — both are spec-legal; what it must not
	// do is move somewhere else or fault differently.
	checkOutOfRange := func(target, clamped, unchanged int) {
		switch err := f.Move(target); {
		case err == nil:
			focuserWaitStopped(t, f)
			if p, perr := f.Position(); perr != nil || p != clamped {
				t.Errorf("Move(%d): Position = %v, %v; want %d (clamped)", target, p, perr, clamped)
			}
		case errors.Is(err, server.ErrInvalidValue):
			if p, perr := f.Position(); perr != nil || p != unchanged {
				t.Errorf("Move(%d) rejected: Position = %v, %v; want %d (unchanged)", target, p, perr, unchanged)
			}
		default:
			t.Errorf("Move(%d): %v; want clamp or InvalidValue", target, err)
		}
	}
	// Position is maxStep here (previous check); a rejected move leaves it so.
	checkOutOfRange(-1000, 0, maxStep)
	if p, _ := f.Position(); p == 0 {
		checkOutOfRange(maxStep+1000, maxStep, 0)
	} else {
		checkOutOfRange(maxStep+1000, maxStep, maxStep)
	}

	// Halt: confirm it stops a move where implemented; NotImplemented is
	// spec-legal (IFocuserV3: a focuser that cannot halt must throw).
	if err := f.Move(0); err != nil {
		t.Errorf("Move(0) for Halt test: %v", err)
	}
	if err := f.Halt(); err != nil && !errors.Is(err, server.ErrNotImplemented) {
		t.Errorf("Halt(): %v; want success or NotImplemented", err)
	}
	focuserWaitStopped(t, f)
	if m, err := f.IsMoving(); err != nil || m {
		t.Errorf("IsMoving() after Halt/settle = %v, %v; want false", m, err)
	}

	// TempComp write (available on the sim): set true then false and verify.
	if tcAvail {
		if err := f.SetTempComp(true); err != nil {
			t.Errorf("SetTempComp(true): %v", err)
		} else if tc, err := f.TempComp(); err != nil || !tc {
			t.Errorf("TempComp() after SetTempComp(true) = %v, %v; want true", tc, err)
		}

		// Move while TempComp is enabled. The sim is interface V3+ (IfaceVer 4),
		// where Move with TempComp on must succeed (no exception). Per ASCOM the
		// driver may further adjust position for temperature, so arrival at the
		// requested target is not asserted.
		if err := f.SetTempComp(true); err != nil {
			t.Errorf("SetTempComp(true) before TempComp move: %v", err)
		}
		if err := f.Move(5000); err != nil {
			t.Errorf("Move(5000) with TempComp enabled: want no exception, got %v", err)
		}
		focuserWaitStopped(t, f)
		if err := f.SetTempComp(false); err != nil {
			t.Errorf("SetTempComp(false) after TempComp move: %v", err)
		}

		if err := f.SetTempComp(false); err != nil {
			t.Errorf("SetTempComp(false): %v", err)
		} else if tc, err := f.TempComp(); err != nil || tc {
			t.Errorf("TempComp() after SetTempComp(false) = %v, %v; want false", tc, err)
		}
	}
}

// focuserWaitStopped polls IsMoving until the focuser is stationary or the
// timeout elapses.
func focuserWaitStopped(t *testing.T, f *client.Focuser) {
	t.Helper()
	deadline := time.Now().Add(SettleTimeout)
	for time.Now().Before(deadline) {
		m, err := f.IsMoving()
		if err != nil {
			t.Fatalf("IsMoving(): %v", err)
		}
		if !m {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("focuser still moving after timeout")
}

// focuserAbsDiff returns the absolute difference between two step positions.
func focuserAbsDiff(a, b int) int {
	if a < b {
		return b - a
	}
	return a - b
}
