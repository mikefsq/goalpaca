package sim

import (
	"errors"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	"github.com/mikefsq/goalpaca/server"
)

// allSimsServer registers one of every simulator on a server and returns its URL.
func allSimsServer(t *testing.T) string {
	t.Helper()
	srv := server.New(server.Config{Discovery: server.DiscoveryConfig{Mode: server.DiscoveryOff}})
	reg := func(typ server.DeviceType, d server.Device) {
		if err := srv.Register(typ, 0, d); err != nil {
			t.Fatalf("register %s: %v", typ, err)
		}
	}
	reg(server.CameraType, NewCamera())
	reg(server.CoverCalibratorType, NewCoverCalibrator())
	reg(server.DomeType, NewDome())
	reg(server.FilterWheelType, NewFilterWheel())
	reg(server.FocuserType, NewFocuser())
	reg(server.ObservingConditionsType, NewObservingConditions())
	reg(server.RotatorType, NewRotator())
	reg(server.SafetyMonitorType, NewSafetyMonitor())
	reg(server.SwitchType, NewSwitch())
	reg(server.TelescopeType, NewTelescope())
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts.URL
}

type connector interface{ SetConnected(bool) error }

func mustConnect(t *testing.T, c connector) {
	t.Helper()
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

func TestAllSimsSmoke(t *testing.T) {
	url := allSimsServer(t)

	t.Run("Camera", func(t *testing.T) {
		c := client.NewCamera(url, 0)
		mustConnect(t, c)
		if x, err := c.CameraXSize(); err != nil || x != 1936 {
			t.Fatalf("CameraXSize = %d, %v", x, err)
		}
		if err := c.StartExposure(0.01, true); err != nil {
			t.Fatalf("StartExposure: %v", err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			ready, err := c.ImageReady()
			if err != nil {
				t.Fatalf("ImageReady: %v", err)
			}
			if ready {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("image never ready")
			}
			time.Sleep(10 * time.Millisecond)
		}
		frame, err := c.ImageArray()
		if err != nil || frame.Width != 1936 || frame.Height != 1096 {
			t.Fatalf("ImageArray: %dx%d, %v", frame.Width, frame.Height, err)
		}
	})

	t.Run("CoverCalibrator", func(t *testing.T) {
		c := client.NewCoverCalibrator(url, 0)
		mustConnect(t, c)
		if mb, err := c.MaxBrightness(); err != nil || mb != 100 {
			t.Fatalf("MaxBrightness = %d, %v", mb, err)
		}
		if err := c.CalibratorOn(50); err != nil {
			t.Fatalf("CalibratorOn(50): %v", err)
		}
		if err := c.CalibratorOn(500); !errors.Is(err, server.ErrInvalidValue) {
			t.Fatalf("CalibratorOn(500): want InvalidValue, got %v", err)
		}
	})

	t.Run("Dome", func(t *testing.T) {
		c := client.NewDome(url, 0)
		mustConnect(t, c)
		if can, err := c.CanPark(); err != nil || !can {
			t.Fatalf("CanPark = %v, %v", can, err)
		}
		if err := c.SlewToAzimuth(90); err != nil {
			t.Fatalf("SlewToAzimuth(90): %v", err)
		}
		if err := c.SlewToAzimuth(400); !errors.Is(err, server.ErrInvalidValue) {
			t.Fatalf("SlewToAzimuth(400): want InvalidValue, got %v", err)
		}
	})

	t.Run("FilterWheel", func(t *testing.T) {
		c := client.NewFilterWheel(url, 0)
		mustConnect(t, c)
		if names, err := c.Names(); err != nil || len(names) != 4 {
			t.Fatalf("Names = %v, %v", names, err)
		}
		if err := c.SetPosition(2); err != nil {
			t.Fatalf("SetPosition(2): %v", err)
		}
		if err := c.SetPosition(9); !errors.Is(err, server.ErrInvalidValue) {
			t.Fatalf("SetPosition(9): want InvalidValue, got %v", err)
		}
	})

	t.Run("Focuser", func(t *testing.T) {
		c := client.NewFocuser(url, 0)
		mustConnect(t, c)
		if abs, err := c.Absolute(); err != nil || !abs {
			t.Fatalf("Absolute = %v, %v", abs, err)
		}
		if err := c.Move(1000); err != nil {
			t.Fatalf("Move(1000): %v", err)
		}
		if err := c.Move(-5); err != nil { // absolute focuser clamps gracefully
			t.Fatalf("Move(-5): want graceful clamp, got error %v", err)
		}
	})

	t.Run("ObservingConditions", func(t *testing.T) {
		c := client.NewObservingConditions(url, 0)
		mustConnect(t, c)
		if temp, err := c.Temperature(); err != nil || math.Abs(temp-15) > 0.001 {
			t.Fatalf("Temperature = %v, %v", temp, err)
		}
		if err := c.SetAveragePeriod(-1); !errors.Is(err, server.ErrInvalidValue) {
			t.Fatalf("SetAveragePeriod(-1): want InvalidValue, got %v", err)
		}
	})

	t.Run("Rotator", func(t *testing.T) {
		c := client.NewRotator(url, 0)
		mustConnect(t, c)
		if err := c.MoveAbsolute(45); err != nil {
			t.Fatalf("MoveAbsolute(45): %v", err)
		}
	})

	t.Run("SafetyMonitor", func(t *testing.T) {
		c := client.NewSafetyMonitor(url, 0)
		mustConnect(t, c)
		if safe, err := c.IsSafe(); err != nil || !safe {
			t.Fatalf("IsSafe = %v, %v", safe, err)
		}
	})

	t.Run("Switch", func(t *testing.T) {
		c := client.NewSwitch(url, 0)
		mustConnect(t, c)
		if n, err := c.MaxSwitch(); err != nil || n != 4 {
			t.Fatalf("MaxSwitch = %d, %v", n, err)
		}
		if err := c.SetSwitchValue(0, 50); err != nil {
			t.Fatalf("SetSwitchValue(0,50): %v", err)
		}
		if v, err := c.GetSwitchValue(0); err != nil || v != 50 {
			t.Fatalf("GetSwitchValue(0) = %v, %v", v, err)
		}
		if err := c.SetSwitchValue(0, 500); !errors.Is(err, server.ErrInvalidValue) {
			t.Fatalf("SetSwitchValue(0,500): want InvalidValue, got %v", err)
		}
	})

	t.Run("Telescope", func(t *testing.T) {
		c := client.NewTelescope(url, 0)
		mustConnect(t, c)
		if can, err := c.CanSlew(); err != nil || !can {
			t.Fatalf("CanSlew = %v, %v", can, err)
		}
		if err := c.SetTracking(true); err != nil {
			t.Fatalf("SetTracking(true): %v", err)
		}
		if err := c.SlewToCoordinatesAsync(5, 20); err != nil {
			t.Fatalf("SlewToCoordinatesAsync(5,20): %v", err)
		}
		if err := c.SlewToCoordinatesAsync(30, 0); !errors.Is(err, server.ErrInvalidValue) {
			t.Fatalf("SlewToCoordinatesAsync(30,0): want InvalidValue, got %v", err)
		}
	})
}
