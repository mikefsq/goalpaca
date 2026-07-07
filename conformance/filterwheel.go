package conformance

import (
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	"github.com/mikefsq/goalpaca/server"
)

// filterWheelMoving is the ASCOM sentinel returned by Position while the wheel
// is in motion.
const filterWheelMoving = -1

// CheckFilterWheel runs the ConformU FilterWheel conformance checks against c.
// Ported from ConformU's FilterWheelTester (CheckProperties / Position Set):
// NotConnected gating, Names/FocusOffsets length consistency, Position range,
// move arrival, and out-of-range → InvalidValue. Per-driver capability tests
// the sim does not implement (performance, directionality) are skipped.
func CheckFilterWheel(t *testing.T, c *client.FilterWheel) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.Position(); !errors.Is(err, server.ErrNotConnected) {
		t.Errorf("Position() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Names and FocusOffsets: required, equal length, non-empty.
	names, err := c.Names()
	if err != nil {
		t.Fatalf("Names(): %v", err)
	}
	offsets, err := c.FocusOffsets()
	if err != nil {
		t.Fatalf("FocusOffsets(): %v", err)
	}
	if len(names) == 0 {
		t.Errorf("Names() returned no entries; want > 0")
	}
	if len(offsets) != len(names) {
		t.Errorf("FocusOffsets() length = %d; want %d (same as Names)", len(offsets), len(names))
	}
	for i, n := range names {
		if n == "" {
			t.Errorf("Names()[%d] is empty; want a non-empty filter name", i)
		}
	}

	// Position: required, readable, and >= -1 (-1 means moving).
	if p, err := c.Position(); err != nil || p < filterWheelMoving {
		t.Errorf("Position() = %v, %v; want >= -1", p, err)
	}

	// No filter slots means movement tests cannot run.
	if len(names) == 0 {
		return
	}

	// Position Set happy path: move to slot 2 (when available) and back to 0,
	// each time confirming arrival at the requested slot. During a cross-slot
	// move, Position must report the moving sentinel (-1) — a wheel that
	// teleports (never reports -1) or reports a stale slot mid-move fails;
	// ConformU reads Position while the move is in flight for the same reason.
	prev := filterWheelWaitArrived(t, c)
	targets := []int{0}
	if len(names) > 2 {
		targets = []int{2, 0}
	}
	for _, target := range targets {
		if err := c.SetPosition(target); err != nil {
			t.Errorf("SetPosition(%d): %v", target, err)
			continue
		}
		got, sawMoving := filterWheelWaitArrivedObserved(t, c)
		if got != target {
			t.Errorf("SetPosition(%d): Position settled at %d; want %d", target, got, target)
		}
		if target != prev && !sawMoving {
			t.Errorf("SetPosition(%d→%d): moving sentinel (-1) never observed during a cross-slot move", prev, target)
		}
		prev = target
	}

	// While stationary, Position must be a valid slot index in [0, len(Names)-1].
	// The move loop above has settled, so the wheel is no longer moving.
	if p := filterWheelWaitArrived(t, c); p < 0 || p >= len(names) {
		t.Errorf("stationary Position() = %d; want in [0, %d]", p, len(names)-1)
	}

	// Position Set validation: out-of-range slots must be rejected as InvalidValue.
	for _, bad := range []int{len(names), -1} {
		if err := c.SetPosition(bad); !errors.Is(err, server.ErrInvalidValue) {
			t.Errorf("SetPosition(%d): want InvalidValue, got %v", bad, err)
		}
	}
}

// filterWheelWaitArrived polls Position until it is no longer the moving
// sentinel (-1) and returns the settled slot. Fails the test on timeout.
func filterWheelWaitArrived(t *testing.T, c *client.FilterWheel) int {
	t.Helper()
	p, _ := filterWheelWaitArrivedObserved(t, c)
	return p
}

// filterWheelWaitArrivedObserved is filterWheelWaitArrived, also reporting
// whether the moving sentinel was observed at least once before arrival.
func filterWheelWaitArrivedObserved(t *testing.T, c *client.FilterWheel) (slot int, sawMoving bool) {
	t.Helper()
	deadline := time.Now().Add(SettleTimeout)
	for time.Now().Before(deadline) {
		p, err := c.Position()
		if err != nil {
			t.Fatalf("Position(): %v", err)
		}
		if p != filterWheelMoving {
			return p, sawMoving
		}
		sawMoving = true
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("filter wheel still moving after %v", SettleTimeout)
	return filterWheelMoving, sawMoving
}
