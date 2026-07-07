package conformance

import (
	"errors"
	"testing"

	"github.com/mikefsq/goalpaca/client"
	"github.com/mikefsq/goalpaca/server"
)

// CheckSafetyMonitor runs the ConformU SafetyMonitor conformance checks against
// c. Ported from ConformU's SafetyMonitorTester (CheckProperties): NotConnected
// gating, the required IsSafe property (a bool with no error), and the
// Platform 7 DeviceState operational set (IsSafe plus the mandatory TimeStamp).
func CheckSafetyMonitor(t *testing.T, c *client.SafetyMonitor) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.IsSafe(); !errors.Is(err, server.ErrNotConnected) {
		t.Errorf("IsSafe() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Required property: IsSafe returns a bool with no error.
	if _, err := c.IsSafe(); err != nil {
		t.Errorf("IsSafe(): %v", err)
	}

	// DeviceState (Platform 7): the ISafetyMonitorV3 operational set is IsSafe
	// plus the mandatory TimeStamp.
	sv, err := c.DeviceState()
	if err != nil {
		t.Fatalf("DeviceState(): %v", err)
	}
	got := map[string]bool{}
	for _, v := range sv {
		got[v.Name] = true
	}
	for _, want := range []string{"IsSafe", "TimeStamp"} {
		if !got[want] {
			t.Errorf("DeviceState() missing %q (have %v)", want, sv)
		}
	}
}
