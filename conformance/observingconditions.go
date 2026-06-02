package conformance

import (
	"errors"
	"testing"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// CheckObservingConditions runs the ConformU ObservingConditions conformance
// checks against c. Ported from ConformU's ObservingConditionsTester
// (CheckProperties / CheckMethods): NotConnected gating, AveragePeriod
// read/write with InvalidValue rejection of values below the permitted minimum,
// every sensor getter readable within its ASCOM-sensible range, Refresh, and the
// SensorDescription / TimeSinceLastUpdate metadata methods.
//
// The goalpaca sim implements all optional sensors, so the optional-sensor
// "NotImplemented" handling (where a sensor and its description / time-of-last-
// update must consistently be implemented or not) is not exercised here.
func CheckObservingConditions(t *testing.T, c *client.ObservingConditions) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.Temperature(); !errors.Is(err, alpacadev.ErrNotConnected) {
		t.Errorf("Temperature() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	observingconditionsCheckSensors(t, c)
	observingconditionsCheckAveragePeriod(t, c)

	// Refresh is mandatory and should complete without error.
	if err := c.Refresh(); err != nil {
		t.Errorf("Refresh(): %v", err)
	}

	// SensorDescription returns a non-empty description for an implemented sensor.
	if d, err := c.SensorDescription("Temperature"); err != nil || d == "" {
		t.Errorf(`SensorDescription("Temperature") = %q, %v; want non-empty, nil`, d, err)
	}

	// TimeSinceLastUpdate returns a non-negative age in seconds.
	if ts, err := c.TimeSinceLastUpdate("Temperature"); err != nil || ts < 0 {
		t.Errorf(`TimeSinceLastUpdate("Temperature") = %v, %v; want >=0, nil`, ts, err)
	}

	// The mandatory "latest update" form: TimeSinceLastUpdate("") reports the age
	// of the most recently updated sensor. ASCOM permits -1 to signal "unknown",
	// so the value must be >= -1.
	if ts, err := c.TimeSinceLastUpdate(""); err != nil || ts < -1 {
		t.Errorf(`TimeSinceLastUpdate("") = %v, %v; want >=-1, nil`, ts, err)
	}

	observingconditionsCheckMetadata(t, c)
}

// observingconditionsCheckMetadata verifies that, for every ASCOM sensor name,
// SensorDescription returns a non-empty description and TimeSinceLastUpdate
// returns a value >= -1 (where -1 signals "unknown"), both without error.
func observingconditionsCheckMetadata(t *testing.T, c *client.ObservingConditions) {
	t.Helper()

	names := []string{
		"CloudCover",
		"DewPoint",
		"Humidity",
		"Pressure",
		"RainRate",
		"SkyBrightness",
		"SkyQuality",
		"SkyTemperature",
		"StarFWHM",
		"Temperature",
		"WindDirection",
		"WindGust",
		"WindSpeed",
	}
	for _, name := range names {
		if d, err := c.SensorDescription(name); err != nil || d == "" {
			t.Errorf("SensorDescription(%q) = %q, %v; want non-empty, nil", name, d, err)
		}
		if ts, err := c.TimeSinceLastUpdate(name); err != nil || ts < -1 {
			t.Errorf("TimeSinceLastUpdate(%q) = %v, %v; want >=-1, nil", name, ts, err)
		}
	}
}

// observingconditionsCheckSensors verifies every sensor getter returns no error
// and, where a sensor has a defined physical range, a value within that range.
func observingconditionsCheckSensors(t *testing.T, c *client.ObservingConditions) {
	t.Helper()

	// Sensors with a bounded ASCOM-sensible range.
	bounded := []struct {
		name     string
		get      func() (float64, error)
		min, max float64
	}{
		{"Humidity", c.Humidity, 0, 100},
		{"CloudCover", c.CloudCover, 0, 100},
		{"WindDirection", c.WindDirection, 0, 360},
	}
	for _, s := range bounded {
		if v, err := s.get(); err != nil || v < s.min || v > s.max {
			t.Errorf("%s() = %v, %v; want [%g,%g]", s.name, v, err, s.min, s.max)
		}
	}

	// Sensors that must be non-negative.
	nonNegative := []struct {
		name string
		get  func() (float64, error)
	}{
		{"RainRate", c.RainRate},
		{"WindSpeed", c.WindSpeed},
		{"WindGust", c.WindGust},
	}
	for _, s := range nonNegative {
		if v, err := s.get(); err != nil || v < 0 {
			t.Errorf("%s() = %v, %v; want >=0", s.name, v, err)
		}
	}

	// Pressure must be strictly positive and within a sane atmospheric bound (hPa).
	if v, err := c.Pressure(); err != nil || v <= 0 || v > 1100 {
		t.Errorf("Pressure() = %v, %v; want (0,1100]", v, err)
	}

	// Tighter upper bounds for the rate/speed sensors: a rain rate beyond
	// 20000 mm/hr or wind beyond 1000 m/s is not physically sensible.
	upperBounded := []struct {
		name string
		get  func() (float64, error)
		max  float64
	}{
		{"RainRate", c.RainRate, 20000},
		{"WindSpeed", c.WindSpeed, 1000},
		{"WindGust", c.WindGust, 1000},
	}
	for _, s := range upperBounded {
		if v, err := s.get(); err != nil || v > s.max {
			t.Errorf("%s() = %v, %v; want <=%g", s.name, v, err, s.max)
		}
	}

	// Remaining sensors only need to be readable without error.
	readable := []struct {
		name string
		get  func() (float64, error)
	}{
		{"Temperature", c.Temperature},
		{"DewPoint", c.DewPoint},
		{"SkyTemperature", c.SkyTemperature},
		{"SkyBrightness", c.SkyBrightness},
		{"SkyQuality", c.SkyQuality},
		{"StarFWHM", c.StarFWHM},
	}
	for _, s := range readable {
		if _, err := s.get(); err != nil {
			t.Errorf("%s(): %v", s.name, err)
		}
	}
}

// observingconditionsCheckAveragePeriod exercises the mandatory AveragePeriod
// read/write semantics: a positive period round-trips, the value is restored to
// 0, and a sub-minimum value is rejected with InvalidValue.
func observingconditionsCheckAveragePeriod(t *testing.T, c *client.ObservingConditions) {
	t.Helper()

	if _, err := c.AveragePeriod(); err != nil {
		t.Errorf("AveragePeriod(): %v", err)
	}

	if err := c.SetAveragePeriod(1.0); err != nil {
		t.Errorf("SetAveragePeriod(1.0): %v", err)
	} else if got, err := c.AveragePeriod(); err != nil || got != 1.0 {
		t.Errorf("AveragePeriod() after set = %v, %v; want 1.0", got, err)
	}

	// Restore the default averaging period.
	if err := c.SetAveragePeriod(0); err != nil {
		t.Errorf("SetAveragePeriod(0): %v", err)
	}

	// A value below the permitted minimum must be rejected.
	if err := c.SetAveragePeriod(-1); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetAveragePeriod(-1): want InvalidValue, got %v", err)
	}
}
