package alpacadev

// ObservingConditions is the ASCOM ObservingConditions interface
// (IObservingConditionsV1/V2). Sensor properties return an error (typically
// NotImplemented) when that sensor is not present. SensorDescription and
// TimeSinceLastUpdate take a "SensorName" parameter on GET.
type ObservingConditions interface {
	Device

	AveragePeriod() float64
	SetAveragePeriod(float64) error
	CloudCover() (float64, error)
	DewPoint() (float64, error)
	Humidity() (float64, error)
	Pressure() (float64, error)
	RainRate() (float64, error)
	SkyBrightness() (float64, error)
	SkyQuality() (float64, error)
	SkyTemperature() (float64, error)
	StarFWHM() (float64, error)
	Temperature() (float64, error)
	WindDirection() (float64, error)
	WindGust() (float64, error)
	WindSpeed() (float64, error)

	SensorDescription(name string) (string, error)
	TimeSinceLastUpdate(name string) (float64, error)
	Refresh() error
}

// BaseObservingConditions provides not-implemented / zero defaults.
type BaseObservingConditions struct {
	BaseDevice
}

func (b *BaseObservingConditions) AveragePeriod() float64           { return 0 }
func (b *BaseObservingConditions) SetAveragePeriod(float64) error   { return ErrNotImplemented }
func (b *BaseObservingConditions) CloudCover() (float64, error)     { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) DewPoint() (float64, error)       { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) Humidity() (float64, error)       { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) Pressure() (float64, error)       { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) RainRate() (float64, error)       { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) SkyBrightness() (float64, error)  { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) SkyQuality() (float64, error)     { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) SkyTemperature() (float64, error) { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) StarFWHM() (float64, error)       { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) Temperature() (float64, error)    { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) WindDirection() (float64, error)  { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) WindGust() (float64, error)       { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) WindSpeed() (float64, error)      { return 0, ErrNotImplemented }
func (b *BaseObservingConditions) SensorDescription(string) (string, error) {
	return "", ErrNotImplemented
}
func (b *BaseObservingConditions) TimeSinceLastUpdate(string) (float64, error) {
	return 0, ErrNotImplemented
}
func (b *BaseObservingConditions) Refresh() error { return ErrNotImplemented }

func observingConditionsGet(member string, oc ObservingConditions, p params) (any, bool, error) {
	switch member {
	case "averageperiod":
		return oc.AveragePeriod(), true, nil
	case "cloudcover":
		v, err := oc.CloudCover()
		return v, true, err
	case "dewpoint":
		v, err := oc.DewPoint()
		return v, true, err
	case "humidity":
		v, err := oc.Humidity()
		return v, true, err
	case "pressure":
		v, err := oc.Pressure()
		return v, true, err
	case "rainrate":
		v, err := oc.RainRate()
		return v, true, err
	case "skybrightness":
		v, err := oc.SkyBrightness()
		return v, true, err
	case "skyquality":
		v, err := oc.SkyQuality()
		return v, true, err
	case "skytemperature":
		v, err := oc.SkyTemperature()
		return v, true, err
	case "starfwhm":
		v, err := oc.StarFWHM()
		return v, true, err
	case "temperature":
		v, err := oc.Temperature()
		return v, true, err
	case "winddirection":
		v, err := oc.WindDirection()
		return v, true, err
	case "windgust":
		v, err := oc.WindGust()
		return v, true, err
	case "windspeed":
		v, err := oc.WindSpeed()
		return v, true, err
	case "sensordescription":
		name, _ := p.get("SensorName")
		v, err := oc.SensorDescription(name)
		return v, true, err
	case "timesincelastupdate":
		name, _ := p.get("SensorName")
		v, err := oc.TimeSinceLastUpdate(name)
		return v, true, err
	}
	return nil, false, nil
}

func observingConditionsPut(member string, oc ObservingConditions, p params) (bool, error) {
	switch member {
	case "averageperiod":
		f, err := p.reqFloat("AveragePeriod")
		if err != nil {
			return true, err
		}
		return true, oc.SetAveragePeriod(f)
	case "refresh":
		return true, oc.Refresh()
	}
	return false, nil
}
