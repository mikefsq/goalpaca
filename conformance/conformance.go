// Package conformance ports ASCOM ConformU's device-conformance checks into Go
// tests that drive the goalpaca client library. Each Check* function takes a
// constructed client and asserts the ASCOM spec behaviour (capability/property
// consistency, async-operation completion, and error semantics) using ConformU's
// *Tester logic as the reference. The same checks can run in-process against the
// simulators (over httptest) or against an external Alpaca server by address.
package conformance

import (
	"math"
	"testing"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// CommonDevice is the ICommon surface shared by every typed client (satisfied by
// *client.Camera, *client.Rotator, … via the embedded client.Device).
type CommonDevice interface {
	Connected() (bool, error)
	SetConnected(bool) error
	Connect() error    // Platform 7 async connect initiator
	Disconnect() error // Platform 7 async disconnect initiator
	Connecting() (bool, error)
	Name() (string, error)
	Description() (string, error)
	DriverInfo() (string, error)
	DriverVersion() (string, error)
	InterfaceVersion() (int, error)
	SupportedActions() ([]string, error)
	DeviceState() ([]alpacadev.StateValue, error)
}

// CheckCommon verifies the common ASCOM members: identity (readable without a
// connection), the connection round-trip, and Connecting. Mirrors ConformU's
// DeviceTesterBaseClass common-member checks.
func CheckCommon(t *testing.T, d CommonDevice) {
	t.Helper()

	// Identity members are mandatory and work without being connected.
	if name, err := d.Name(); err != nil || name == "" {
		t.Errorf("Name() = %q, %v; want non-empty, no error", name, err)
	}
	if _, err := d.Description(); err != nil {
		t.Errorf("Description(): %v", err)
	}
	if di, err := d.DriverInfo(); err != nil || di == "" {
		t.Errorf("DriverInfo() = %q, %v; want non-empty, no error", di, err)
	}
	if dv, err := d.DriverVersion(); err != nil || dv == "" {
		t.Errorf("DriverVersion() = %q, %v; want non-empty, no error", dv, err)
	}
	if iv, err := d.InterfaceVersion(); err != nil || iv < 1 {
		t.Errorf("InterfaceVersion() = %d, %v; want >= 1, no error", iv, err)
	}
	// SupportedActions returns a list; each entry must be a non-empty string.
	if actions, err := d.SupportedActions(); err != nil {
		t.Errorf("SupportedActions(): %v", err)
	} else {
		for i, a := range actions {
			if a == "" {
				t.Errorf("SupportedActions()[%d] is an empty string", i)
			}
		}
	}

	// Synchronous connection round-trip.
	if err := d.SetConnected(true); err != nil {
		t.Fatalf("SetConnected(true): %v", err)
	}
	if c, err := d.Connected(); err != nil || !c {
		t.Errorf("Connected() after connect = %v, %v; want true", c, err)
	}
	if _, err := d.Connecting(); err != nil {
		t.Errorf("Connecting(): %v", err)
	}
	// DeviceState (Platform 7): readable while connected, returns a list.
	if _, err := d.DeviceState(); err != nil {
		t.Errorf("DeviceState(): %v", err)
	}
	if err := d.SetConnected(false); err != nil {
		t.Fatalf("SetConnected(false): %v", err)
	}
	if c, err := d.Connected(); err != nil || c {
		t.Errorf("Connected() after disconnect = %v, %v; want false", c, err)
	}

	// Asynchronous connect/disconnect (Platform 7): Connect()/Disconnect() are
	// initiators and Connecting() is the completion property.
	if err := d.Connect(); err != nil {
		t.Errorf("Connect(): %v", err)
	}
	waitNotConnecting(t, d)
	if c, err := d.Connected(); err != nil || !c {
		t.Errorf("Connected() after async Connect = %v, %v; want true", c, err)
	}
	if err := d.Disconnect(); err != nil {
		t.Errorf("Disconnect(): %v", err)
	}
	waitNotConnecting(t, d)
	if c, err := d.Connected(); err != nil || c {
		t.Errorf("Connected() after async Disconnect = %v, %v; want false", c, err)
	}
}

// waitNotConnecting polls Connecting() until an async connect/disconnect
// completes or the timeout elapses.
func waitNotConnecting(t *testing.T, d CommonDevice) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connecting, err := d.Connecting()
		if err != nil {
			t.Fatalf("Connecting(): %v", err)
		}
		if !connecting {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("device still Connecting after timeout")
}

// wrap360 reduces an angle to [0, 360).
func wrap360(a float64) float64 {
	a = math.Mod(a, 360)
	if a < 0 {
		a += 360
	}
	return a
}

// angleDiff is the absolute shortest angular distance between two angles (degrees).
func angleDiff(a, b float64) float64 {
	d := math.Mod(a-b, 360)
	if d < -180 {
		d += 360
	}
	if d > 180 {
		d -= 360
	}
	if d < 0 {
		d = -d
	}
	return d
}
