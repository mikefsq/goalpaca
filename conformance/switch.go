package conformance

import (
	"errors"
	"math"
	"testing"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// CheckSwitch runs the ConformU Switch conformance checks against c. Ported from
// ConformU's SwitchTester (CheckProperties / CheckMethods): NotConnected gating,
// MaxSwitch read, per-switch property/range consistency (name, description,
// CanWrite, min/max/step), analog and boolean read/write round-trips, and
// out-of-range / invalid-Id → InvalidValue rejections.
//
// The async members (SetAsync, SetAsyncValue, CancelAsync, StateChangeComplete)
// are intentionally skipped because the simulator's CanAsync is false (the
// BaseSwitch default); see switchdevSkipped.
func CheckSwitch(t *testing.T, c *client.Switch) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.MaxSwitch(); !errors.Is(err, alpacadev.ErrNotConnected) {
		t.Errorf("MaxSwitch() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	n, err := c.MaxSwitch()
	if err != nil {
		t.Fatalf("MaxSwitch(): %v", err)
	}
	if n <= 0 {
		t.Fatalf("MaxSwitch() = %d; want > 0", n)
	}

	// Per-switch property and range consistency.
	for id := 0; id < n; id++ {
		switchdevCheckSwitchProperties(t, c, id)
	}

	// Analog write/read round-trip on switch 0.
	if err := c.SetSwitchValue(0, 50); err != nil {
		t.Errorf("SetSwitchValue(0, 50): %v", err)
	} else if got, err := c.GetSwitchValue(0); err != nil || got != 50 {
		t.Errorf("GetSwitchValue(0) after set = %v, %v; want 50", got, err)
	}

	// Boolean write/read round-trip on switch 1.
	if err := c.SetSwitch(1, true); err != nil {
		t.Errorf("SetSwitch(1, true): %v", err)
	} else if got, err := c.GetSwitch(1); err != nil || !got {
		t.Errorf("GetSwitch(1) after set true = %v, %v; want true", got, err)
	}
	if err := c.SetSwitch(1, false); err != nil {
		t.Errorf("SetSwitch(1, false): %v", err)
	} else if got, err := c.GetSwitch(1); err != nil || got {
		t.Errorf("GetSwitch(1) after set false = %v, %v; want false", got, err)
	}

	// Validation: value above MaxSwitchValue → InvalidValue.
	if err := c.SetSwitchValue(0, 9999); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSwitchValue(0, 9999): want InvalidValue, got %v", err)
	}

	// Validation: invalid Id → InvalidValue.
	if _, err := c.GetSwitch(n + 100); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("GetSwitch(%d): want InvalidValue, got %v", n+100, err)
	}

	// Invalid-Id rejection for every member, exercised with a negative Id and
	// with the first out-of-range high Id (MaxSwitch).
	switchdevCheckInvalidID(t, c, -1)
	switchdevCheckInvalidID(t, c, n)

	// Below-minimum value rejection: MinSwitchValue is 0, so a negative value is
	// below range and must be rejected.
	if err := c.SetSwitchValue(0, -1); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSwitchValue(0, -1): want InvalidValue, got %v", err)
	}

	// Step consistency: the step must fit within the range and the range must be
	// an integer multiple of the step.
	for id := 0; id < n; id++ {
		switchdevCheckStep(t, c, id)
	}

	// Boolean/analog linkage on switch 0.
	switchdevCheckLinkage(t, c, 0)

	// SetSwitchName is an optional member; the simulator implements it as a no-op
	// that returns nil.
	if err := c.SetSwitchName(0, "Test"); err != nil {
		t.Errorf("SetSwitchName(0, %q): %v", "Test", err)
	}

	switchdevSkipped(t)
}

// switchdevCheckSwitchProperties verifies the read-only descriptive properties
// and value-range consistency for a single switch Id.
func switchdevCheckSwitchProperties(t *testing.T, c *client.Switch, id int) {
	t.Helper()

	if name, err := c.GetSwitchName(id); err != nil || name == "" {
		t.Errorf("GetSwitchName(%d) = %q, %v; want non-empty", id, name, err)
	}
	if desc, err := c.GetSwitchDescription(id); err != nil || desc == "" {
		t.Errorf("GetSwitchDescription(%d) = %q, %v; want non-empty", id, desc, err)
	}
	if w, err := c.CanWrite(id); err != nil || !w {
		t.Errorf("CanWrite(%d) = %v, %v; want true", id, w, err)
	}

	max, err := c.MaxSwitchValue(id)
	if err != nil {
		t.Errorf("MaxSwitchValue(%d): %v", id, err)
	}
	min, err := c.MinSwitchValue(id)
	if err != nil {
		t.Errorf("MinSwitchValue(%d): %v", id, err)
	}
	if !(max > min) {
		t.Errorf("MaxSwitchValue(%d)=%v MinSwitchValue(%d)=%v; want max > min", id, max, id, min)
	}
	if step, err := c.SwitchStep(id); err != nil || step <= 0 {
		t.Errorf("SwitchStep(%d) = %v, %v; want > 0", id, step, err)
	}
}

// switchdevCheckInvalidID verifies that every Id-indexed member rejects an
// out-of-range Id with InvalidValue.
func switchdevCheckInvalidID(t *testing.T, c *client.Switch, id int) {
	t.Helper()

	if _, err := c.CanWrite(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("CanWrite(%d): want InvalidValue, got %v", id, err)
	}
	if _, err := c.GetSwitch(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("GetSwitch(%d): want InvalidValue, got %v", id, err)
	}
	if _, err := c.GetSwitchName(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("GetSwitchName(%d): want InvalidValue, got %v", id, err)
	}
	if _, err := c.GetSwitchDescription(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("GetSwitchDescription(%d): want InvalidValue, got %v", id, err)
	}
	if _, err := c.GetSwitchValue(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("GetSwitchValue(%d): want InvalidValue, got %v", id, err)
	}
	if _, err := c.MaxSwitchValue(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("MaxSwitchValue(%d): want InvalidValue, got %v", id, err)
	}
	if _, err := c.MinSwitchValue(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("MinSwitchValue(%d): want InvalidValue, got %v", id, err)
	}
	if _, err := c.SwitchStep(id); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SwitchStep(%d): want InvalidValue, got %v", id, err)
	}
	if err := c.SetSwitch(id, true); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSwitch(%d, true): want InvalidValue, got %v", id, err)
	}
	if err := c.SetSwitchValue(id, 0); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSwitchValue(%d, 0): want InvalidValue, got %v", id, err)
	}
}

// switchdevCheckStep verifies step-vs-range consistency for a single switch Id:
// the step fits within the range, and the range is an integer multiple of it.
func switchdevCheckStep(t *testing.T, c *client.Switch, id int) {
	t.Helper()

	max, err := c.MaxSwitchValue(id)
	if err != nil {
		t.Errorf("MaxSwitchValue(%d): %v", id, err)
		return
	}
	min, err := c.MinSwitchValue(id)
	if err != nil {
		t.Errorf("MinSwitchValue(%d): %v", id, err)
		return
	}
	step, err := c.SwitchStep(id)
	if err != nil {
		t.Errorf("SwitchStep(%d): %v", id, err)
		return
	}
	rng := max - min
	if step > rng {
		t.Errorf("SwitchStep(%d)=%v; want <= MaxSwitchValue-MinSwitchValue=%v", id, step, rng)
	}
	if step > 0 {
		steps := rng / step
		const tol = 1e-6
		if d := steps - math.Round(steps); math.Abs(d) > tol {
			t.Errorf("SwitchStep(%d)=%v does not evenly divide range %v (%.6f steps)", id, step, rng, steps)
		}
	}
}

// switchdevCheckLinkage verifies that the boolean and analog views of a switch
// stay consistent: SetSwitch toggles between Min/Max value, and writing the
// Min/Max value toggles the boolean.
func switchdevCheckLinkage(t *testing.T, c *client.Switch, id int) {
	t.Helper()

	max, err := c.MaxSwitchValue(id)
	if err != nil {
		t.Errorf("MaxSwitchValue(%d): %v", id, err)
		return
	}
	min, err := c.MinSwitchValue(id)
	if err != nil {
		t.Errorf("MinSwitchValue(%d): %v", id, err)
		return
	}

	if err := c.SetSwitch(id, true); err != nil {
		t.Errorf("SetSwitch(%d, true): %v", id, err)
	} else if v, err := c.GetSwitchValue(id); err != nil || v != max {
		t.Errorf("GetSwitchValue(%d) after SetSwitch true = %v, %v; want %v", id, v, err, max)
	}
	if err := c.SetSwitch(id, false); err != nil {
		t.Errorf("SetSwitch(%d, false): %v", id, err)
	} else if v, err := c.GetSwitchValue(id); err != nil || v != min {
		t.Errorf("GetSwitchValue(%d) after SetSwitch false = %v, %v; want %v", id, v, err, min)
	}

	if err := c.SetSwitchValue(id, max); err != nil {
		t.Errorf("SetSwitchValue(%d, %v): %v", id, max, err)
	} else if b, err := c.GetSwitch(id); err != nil || !b {
		t.Errorf("GetSwitch(%d) after SetSwitchValue(max) = %v, %v; want true", id, b, err)
	}
	if err := c.SetSwitchValue(id, min); err != nil {
		t.Errorf("SetSwitchValue(%d, %v): %v", id, min, err)
	} else if b, err := c.GetSwitch(id); err != nil || b {
		t.Errorf("GetSwitch(%d) after SetSwitchValue(min) = %v, %v; want false", id, b, err)
	}
}

// switchdevSkipped records the conformance members that are not exercised
// because the simulator reports CanAsync == false.
func switchdevSkipped(t *testing.T) {
	t.Helper()
	t.Log("Switch: skipping async members (SetAsync, SetAsyncValue, CancelAsync, StateChangeComplete): CanAsync is false")
}
