package sim

import (
	"sync"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// ObservingConditions is a simulated ASCOM ObservingConditions device. It models
// a plausible weather station, reporting fixed-but-realistic sensor values and a
// configurable averaging period.
type ObservingConditions struct {
	alpacadev.BaseObservingConditions

	mu            sync.Mutex
	averagePeriod float64 // hours
}

// ObservingConditionsOption configures a simulated ObservingConditions device.
type ObservingConditionsOption func(*ObservingConditions)

// NewObservingConditions creates a simulated ObservingConditions device.
func NewObservingConditions(opts ...ObservingConditionsOption) *ObservingConditions {
	oc := &ObservingConditions{}
	oc.ID = "goalpaca-sim-observingconditions-1"
	oc.DevName = "Alpaca ObservingConditions Simulator"
	oc.Desc = "goalpaca simulated observing conditions"
	oc.Info = "goalpaca sim"
	oc.Version = "1.0"
	oc.IfaceVer = 2
	for _, o := range opts {
		o(oc)
	}
	return oc
}

// --- ASCOM ObservingConditions members ---

func (oc *ObservingConditions) AveragePeriod() float64 {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	return oc.averagePeriod
}

func (oc *ObservingConditions) SetAveragePeriod(hours float64) error {
	if hours < 0 {
		return alpacadev.ErrInvalidValue
	}
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.averagePeriod = hours
	return nil
}

func (oc *ObservingConditions) CloudCover() (float64, error)     { return 10, nil }
func (oc *ObservingConditions) DewPoint() (float64, error)       { return 3.3, nil }
func (oc *ObservingConditions) Humidity() (float64, error)       { return 45, nil }
func (oc *ObservingConditions) Pressure() (float64, error)       { return 1010, nil }
func (oc *ObservingConditions) RainRate() (float64, error)       { return 0, nil }
func (oc *ObservingConditions) SkyBrightness() (float64, error)  { return 21.5, nil }
func (oc *ObservingConditions) SkyQuality() (float64, error)     { return 20.5, nil }
func (oc *ObservingConditions) SkyTemperature() (float64, error) { return -25, nil }
func (oc *ObservingConditions) StarFWHM() (float64, error)       { return 2.4, nil }
func (oc *ObservingConditions) Temperature() (float64, error)    { return 15.0, nil }
func (oc *ObservingConditions) WindDirection() (float64, error)  { return 180, nil }
func (oc *ObservingConditions) WindGust() (float64, error)       { return 4.0, nil }
func (oc *ObservingConditions) WindSpeed() (float64, error)      { return 2.5, nil }

func (oc *ObservingConditions) SensorDescription(name string) (string, error) {
	return "Simulated " + name + " sensor", nil
}

func (oc *ObservingConditions) TimeSinceLastUpdate(name string) (float64, error) {
	return 0.5, nil
}

func (oc *ObservingConditions) Refresh() error { return nil }
