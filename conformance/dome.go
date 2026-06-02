package conformance

import (
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// domePositionTolerance is the arrival tolerance in degrees for azimuth and
// altitude (ConformU uses a small tolerance to allow for real-hardware settling).
const domePositionTolerance = 2.0

// CheckDome runs the ConformU Dome conformance checks against c. Ported from
// ConformU's DomeTester (CheckProperties / CheckMethods): NotConnected gating,
// capability flag readability and expected values, azimuth/altitude slew arrival
// with out-of-range → InvalidValue, SyncToAzimuth, the shutter open/close state
// machine, Slaved read/write, and FindHome/Park/AbortSlew.
func CheckDome(t *testing.T, c *client.Dome) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.Azimuth(); !errors.Is(err, alpacadev.ErrNotConnected) {
		t.Errorf("Azimuth() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Capability flags must be readable and match the simulator's values.
	caps := []struct {
		name string
		read func() (bool, error)
		want bool
	}{
		{"CanFindHome", c.CanFindHome, true},
		{"CanPark", c.CanPark, true},
		{"CanSetAltitude", c.CanSetAltitude, true},
		{"CanSetAzimuth", c.CanSetAzimuth, true},
		{"CanSetPark", c.CanSetPark, true},
		{"CanSetShutter", c.CanSetShutter, true},
		{"CanSlave", c.CanSlave, false},
		{"CanSyncAzimuth", c.CanSyncAzimuth, true},
	}
	for _, cap := range caps {
		if got, err := cap.read(); err != nil || got != cap.want {
			t.Errorf("%s() = %v, %v; want %v", cap.name, got, err, cap.want)
		}
	}

	// Azimuth slew arrival, then out-of-range rejection.
	if err := c.SlewToAzimuth(90); err != nil {
		t.Errorf("SlewToAzimuth(90): %v", err)
	} else {
		domeWaitStopped(t, c)
		if az, err := c.Azimuth(); err != nil || angleDiff(az, 90) > domePositionTolerance {
			t.Errorf("SlewToAzimuth(90): Azimuth = %v, %v; want ~90", az, err)
		}
	}
	if err := c.SlewToAzimuth(400); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SlewToAzimuth(400): want InvalidValue, got %v", err)
	}
	if err := c.SlewToAzimuth(-10); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SlewToAzimuth(-10): want InvalidValue, got %v", err)
	}

	// Altitude slew arrival (CanSetAltitude is true), then out-of-range rejection.
	if err := c.SlewToAltitude(45); err != nil {
		t.Errorf("SlewToAltitude(45): %v", err)
	} else {
		domeWaitStopped(t, c)
		if alt, err := c.Altitude(); err != nil || angleDiff(alt, 45) > domePositionTolerance {
			t.Errorf("SlewToAltitude(45): Altitude = %v, %v; want ~45", alt, err)
		}
	}
	if err := c.SlewToAltitude(100); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SlewToAltitude(100): want InvalidValue, got %v", err)
	}
	if err := c.SlewToAltitude(-10); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SlewToAltitude(-10): want InvalidValue, got %v", err)
	}

	// SyncToAzimuth changes the reported azimuth without motion, then rejects
	// an out-of-range value.
	if err := c.SyncToAzimuth(180); err != nil {
		t.Errorf("SyncToAzimuth(180): %v", err)
	} else if az, err := c.Azimuth(); err != nil || angleDiff(az, 180) > domePositionTolerance {
		t.Errorf("SyncToAzimuth(180): Azimuth = %v, %v; want ~180", az, err)
	}
	if err := c.SyncToAzimuth(400); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SyncToAzimuth(400): want InvalidValue, got %v", err)
	}
	if err := c.SyncToAzimuth(-10); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SyncToAzimuth(-10): want InvalidValue, got %v", err)
	}

	// Shutter open/close state machine.
	if err := c.OpenShutter(); err != nil {
		t.Errorf("OpenShutter(): %v", err)
	} else {
		domeWaitShutter(t, c, alpacadev.ShutterOpen)
	}
	if err := c.CloseShutter(); err != nil {
		t.Errorf("CloseShutter(): %v", err)
	} else {
		domeWaitShutter(t, c, alpacadev.ShutterClosed)
	}

	// Slaved read/write: CanSlave is false so Slaved reads false, enabling slaving
	// is NotImplemented, and disabling it succeeds.
	if s, err := c.Slaved(); err != nil || s {
		t.Errorf("Slaved() = %v, %v; want false", s, err)
	}
	if err := c.SetSlaved(true); !errors.Is(err, alpacadev.ErrNotImplemented) {
		t.Errorf("SetSlaved(true): want NotImplemented, got %v", err)
	}
	if err := c.SetSlaved(false); err != nil {
		t.Errorf("SetSlaved(false): %v", err)
	}

	// FindHome arrives at home.
	if err := c.FindHome(); err != nil {
		t.Errorf("FindHome(): %v", err)
	} else {
		domeWaitStopped(t, c)
		if home, err := c.AtHome(); err != nil || !home {
			t.Errorf("AtHome() after FindHome = %v, %v; want true", home, err)
		}
	}

	// Park arrives at park.
	if err := c.Park(); err != nil {
		t.Errorf("Park(): %v", err)
	} else {
		domeWaitStopped(t, c)
		if parked, err := c.AtPark(); err != nil || !parked {
			t.Errorf("AtPark() after Park = %v, %v; want true", parked, err)
		}
	}

	// SetPark succeeds (CanSetPark is true).
	if err := c.SetPark(); err != nil {
		t.Errorf("SetPark(): %v", err)
	}

	// AbortSlew is a no-op-safe stop.
	if err := c.AbortSlew(); err != nil {
		t.Errorf("AbortSlew(): %v", err)
	}

	// AbortSlew mid-slew: command a far target, confirm motion starts, then abort
	// and confirm the dome stops slewing.
	if err := c.SlewToAzimuth(270); err != nil {
		t.Errorf("SlewToAzimuth(270): %v", err)
	} else {
		domeWaitSlewing(t, c)
		if err := c.AbortSlew(); err != nil {
			t.Errorf("AbortSlew() mid-slew: %v", err)
		}
		domeWaitStopped(t, c)
		if az, err := c.Azimuth(); err != nil {
			t.Errorf("Azimuth() after abort: %v", err)
		} else if angleDiff(az, 270) <= domePositionTolerance {
			t.Errorf("Azimuth() after abort = %v; want stopped short of 270", az)
		}
	}
}

// domeWaitSlewing polls Slewing until the dome is moving or the timeout elapses.
func domeWaitSlewing(t *testing.T, c *client.Dome) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		moving, err := c.Slewing()
		if err != nil {
			t.Fatalf("Slewing(): %v", err)
		}
		if moving {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("dome did not start slewing before timeout")
}

// domeWaitStopped polls Slewing until the dome is stationary or the timeout
// elapses.
func domeWaitStopped(t *testing.T, c *client.Dome) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		moving, err := c.Slewing()
		if err != nil {
			t.Fatalf("Slewing(): %v", err)
		}
		if !moving {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("dome still slewing after timeout")
}

// domeWaitShutter polls ShutterStatus until it reaches want or the timeout
// elapses.
func domeWaitShutter(t *testing.T, c *client.Dome, want alpacadev.ShutterState) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		s, err := c.ShutterStatus()
		if err != nil {
			t.Fatalf("ShutterStatus(): %v", err)
		}
		if s == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("shutter did not reach state %v after timeout", want)
}
