package conformance

import (
	"errors"
	"testing"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// CheckSafetyMonitor runs the ConformU SafetyMonitor conformance checks against
// c. Ported from ConformU's SafetyMonitorTester (CheckProperties): NotConnected
// gating and the required IsSafe property, which returns a bool with no error.
func CheckSafetyMonitor(t *testing.T, c *client.SafetyMonitor) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.IsSafe(); !errors.Is(err, alpacadev.ErrNotConnected) {
		t.Errorf("IsSafe() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Required property: IsSafe returns a bool with no error.
	if _, err := c.IsSafe(); err != nil {
		t.Errorf("IsSafe(): %v", err)
	}
}
