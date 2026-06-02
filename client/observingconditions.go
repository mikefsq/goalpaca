package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// ObservingConditions is a client for an ASCOM ObservingConditions device.
type ObservingConditions struct{ Device }

// NewObservingConditions returns a client for the weather station at the given
// Alpaca address and device number.
func NewObservingConditions(address string, deviceNumber int, opts ...Option) *ObservingConditions {
	return &ObservingConditions{newDevice(address, alpaca.ObservingConditionsType, deviceNumber, opts...)}
}

func (o *ObservingConditions) AveragePeriod() (float64, error) { return o.getFloat("averageperiod") }
func (o *ObservingConditions) SetAveragePeriod(hours float64) error {
	return o.put("averageperiod", url.Values{"AveragePeriod": {floatParam(hours)}})
}
func (o *ObservingConditions) CloudCover() (float64, error)     { return o.getFloat("cloudcover") }
func (o *ObservingConditions) DewPoint() (float64, error)       { return o.getFloat("dewpoint") }
func (o *ObservingConditions) Humidity() (float64, error)       { return o.getFloat("humidity") }
func (o *ObservingConditions) Pressure() (float64, error)       { return o.getFloat("pressure") }
func (o *ObservingConditions) RainRate() (float64, error)       { return o.getFloat("rainrate") }
func (o *ObservingConditions) SkyBrightness() (float64, error)  { return o.getFloat("skybrightness") }
func (o *ObservingConditions) SkyQuality() (float64, error)     { return o.getFloat("skyquality") }
func (o *ObservingConditions) SkyTemperature() (float64, error) { return o.getFloat("skytemperature") }
func (o *ObservingConditions) StarFWHM() (float64, error)       { return o.getFloat("starfwhm") }
func (o *ObservingConditions) Temperature() (float64, error)    { return o.getFloat("temperature") }
func (o *ObservingConditions) WindDirection() (float64, error)  { return o.getFloat("winddirection") }
func (o *ObservingConditions) WindGust() (float64, error)       { return o.getFloat("windgust") }
func (o *ObservingConditions) WindSpeed() (float64, error)      { return o.getFloat("windspeed") }

// SensorDescription returns a description of the named sensor.
func (o *ObservingConditions) SensorDescription(sensorName string) (string, error) {
	var v string
	err := o.getInto("sensordescription", url.Values{"SensorName": {sensorName}}, &v)
	return v, err
}

// TimeSinceLastUpdate returns seconds since the named sensor last updated.
func (o *ObservingConditions) TimeSinceLastUpdate(sensorName string) (float64, error) {
	var v float64
	err := o.getInto("timesincelastupdate", url.Values{"SensorName": {sensorName}}, &v)
	return v, err
}

func (o *ObservingConditions) Refresh() error { return o.put("refresh", nil) }
