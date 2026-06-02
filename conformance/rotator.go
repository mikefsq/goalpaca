package conformance

import (
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// rotatorPositionTolerance is the arrival tolerance in degrees (ConformU uses a
// small tolerance to allow for real-hardware settling).
const rotatorPositionTolerance = 2.0

// CheckRotator runs the ConformU Rotator conformance checks against r. Ported
// from ConformU's RotatorTester (CheckProperties / CheckMethods): NotConnected
// gating, capability/property consistency and ranges, Reverse read/write,
// MoveAbsolute/Move/MoveMechanical arrival, out-of-range → InvalidValue, and Sync.
func CheckRotator(t *testing.T, r *client.Rotator) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = r.SetConnected(false)
	if _, err := r.Position(); !errors.Is(err, alpacadev.ErrNotConnected) {
		t.Errorf("Position() while disconnected: want NotConnected, got %v", err)
	}
	if err := r.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	canReverse, err := r.CanReverse()
	if err != nil {
		t.Errorf("CanReverse(): %v", err)
	}

	// Get to a stationary, known state.
	if err := r.Halt(); err != nil {
		t.Errorf("Halt(): %v", err)
	}
	waitRotatorStopped(t, r)
	if m, err := r.IsMoving(); err != nil || m {
		t.Errorf("IsMoving() before movement = %v, %v; want false", m, err)
	}

	// Property ranges.
	if p, err := r.Position(); err != nil || p < 0 || p >= 360 {
		t.Errorf("Position() = %v, %v; want [0,360)", p, err)
	}
	if tp, err := r.TargetPosition(); err != nil || tp < 0 || tp >= 360 {
		t.Errorf("TargetPosition() = %v, %v; want [0,360)", tp, err)
	}
	if s, err := r.StepSize(); err == nil && (s < 0 || s >= 360) { // StepSize is optional
		t.Errorf("StepSize() = %v; want [0,360)", s)
	}
	if mp, err := r.MechanicalPosition(); err != nil || mp < 0 || mp >= 360 {
		t.Errorf("MechanicalPosition() = %v, %v; want [0,360)", mp, err)
	}

	// Reverse read/write (when supported).
	if canReverse {
		rev, err := r.Reverse()
		if err != nil {
			t.Errorf("Reverse(): %v", err)
		}
		if err := r.SetReverse(!rev); err != nil {
			t.Errorf("SetReverse(%v): %v", !rev, err)
		}
		if got, err := r.Reverse(); err != nil || got != !rev {
			t.Errorf("Reverse() after set = %v, %v; want %v", got, err, !rev)
		}
		_ = r.SetReverse(rev) // restore
	}

	// Asynchronous behaviour: MoveAbsolute must return promptly with the move
	// still in progress (ConformU/IRotatorV4 require an async initiator). Command
	// a large (~170°) move so that, even at the fast test rate, IsMoving is
	// reliably still true immediately after the call returns.
	if p0, err := r.Position(); err != nil {
		t.Errorf("Position(): %v", err)
	} else {
		asyncTarget := wrap360(p0 + 170)
		if err := r.MoveAbsolute(asyncTarget); err != nil {
			t.Errorf("MoveAbsolute(%g): %v", asyncTarget, err)
		} else if m, err := r.IsMoving(); err != nil || !m {
			t.Errorf("IsMoving() immediately after MoveAbsolute(%g) = %v, %v; want true (move must be asynchronous)", asyncTarget, m, err)
		}
		waitRotatorStopped(t, r)
	}

	// MoveAbsolute to the cardinal-ish angles, then out-of-range rejections.
	for _, target := range []float64{45, 135, 225, 315} {
		if err := r.MoveAbsolute(target); err != nil {
			t.Errorf("MoveAbsolute(%g): %v", target, err)
			continue
		}
		waitRotatorStopped(t, r)
		if p, err := r.Position(); err != nil || angleDiff(p, target) > rotatorPositionTolerance {
			t.Errorf("MoveAbsolute(%g): Position = %v, %v; want ~%g", target, p, err, target)
		}
	}
	for _, bad := range []float64{405, -405} {
		if err := r.MoveAbsolute(bad); !errors.Is(err, alpacadev.ErrInvalidValue) {
			t.Errorf("MoveAbsolute(%g): want InvalidValue, got %v", bad, err)
		}
	}

	// Relative Move, then out-of-range rejections.
	for _, delta := range []float64{10, 40, 130} {
		p0, err := r.Position()
		if err != nil {
			t.Errorf("Position(): %v", err)
			continue
		}
		if err := r.Move(delta); err != nil {
			t.Errorf("Move(%g): %v", delta, err)
			continue
		}
		waitRotatorStopped(t, r)
		want := wrap360(p0 + delta)
		if p1, err := r.Position(); err != nil || angleDiff(p1, want) > rotatorPositionTolerance {
			t.Errorf("Move(%g) from %g: Position = %v, %v; want ~%g", delta, p0, p1, err, want)
		}
	}
	for _, bad := range []float64{375, -375} {
		if err := r.Move(bad); !errors.Is(err, alpacadev.ErrInvalidValue) {
			t.Errorf("Move(%g): want InvalidValue, got %v", bad, err)
		}
	}

	// MoveMechanical, then out-of-range rejections.
	for _, target := range []float64{45, 135, 225, 315} {
		if err := r.MoveMechanical(target); err != nil {
			t.Errorf("MoveMechanical(%g): %v", target, err)
			continue
		}
		waitRotatorStopped(t, r)
		if mp, err := r.MechanicalPosition(); err != nil || angleDiff(mp, target) > rotatorPositionTolerance {
			t.Errorf("MoveMechanical(%g): MechanicalPosition = %v, %v; want ~%g", target, mp, err, target)
		}
	}
	for _, bad := range []float64{405, -405} {
		if err := r.MoveMechanical(bad); !errors.Is(err, alpacadev.ErrInvalidValue) {
			t.Errorf("MoveMechanical(%g): want InvalidValue, got %v", bad, err)
		}
	}

	// Sync: after a mechanical move, Sync makes Position read the synced angle
	// while MechanicalPosition is unchanged.
	syncCases := []struct{ sync, mech float64 }{
		{90, 90}, {120, 90}, {60, 90}, {0, 0}, {30, 0}, {330, 0},
	}
	for _, c := range syncCases {
		if err := r.MoveMechanical(c.mech); err != nil {
			t.Errorf("MoveMechanical(%g): %v", c.mech, err)
			continue
		}
		waitRotatorStopped(t, r)
		if err := r.Sync(c.sync); err != nil {
			t.Errorf("Sync(%g): %v", c.sync, err)
			continue
		}
		if p, err := r.Position(); err != nil || angleDiff(p, c.sync) > rotatorPositionTolerance {
			t.Errorf("Sync(%g): Position = %v, %v; want ~%g", c.sync, p, err, c.sync)
		}
		if mp, err := r.MechanicalPosition(); err != nil || angleDiff(mp, c.mech) > rotatorPositionTolerance {
			t.Errorf("Sync(%g,%g): MechanicalPosition = %v, %v; want ~%g", c.sync, c.mech, mp, err, c.mech)
		}
	}
}

func waitRotatorStopped(t *testing.T, r *client.Rotator) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m, err := r.IsMoving()
		if err != nil {
			t.Fatalf("IsMoving(): %v", err)
		}
		if !m {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("rotator still moving after timeout")
}
