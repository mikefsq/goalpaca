package server

import "time"

// deviceStateValues builds the Platform 7 DeviceState operational-property set
// for a device, appending the mandatory ISO-8601 UTC TimeStamp. The library
// composes this from the typed interface getters, so a driver gets a correct
// DeviceState for free (it need not implement DeviceState itself) — then the
// driver's own DeviceState() is merged in: entries override library-built
// values by name, new names are appended. That is how a driver publishes state
// the library cannot derive (per-switch values, vendor extras) or corrects a
// built entry.
//
// Properties whose getter returns an error (e.g. NotImplemented) are omitted,
// matching ASCOM's rule that DeviceState carries only supported operational
// properties. The exact per-type sets follow the ASCOM Master Interface
// Definitions (Platform 7).
func deviceStateValues(devType DeviceType, dev Device) []StateValue {
	var sv []StateValue
	switch devType {
	case CameraType:
		if x, ok := dev.(Camera); ok {
			sv = cameraStateValues(x)
		}
	case CoverCalibratorType:
		if x, ok := dev.(CoverCalibrator); ok {
			sv = coverCalibratorStateValues(x)
		}
	case DomeType:
		if x, ok := dev.(Dome); ok {
			sv = domeStateValues(x)
		}
	case FilterWheelType:
		if x, ok := dev.(FilterWheel); ok {
			sv = []StateValue{{Name: "Position", Value: x.Position()}}
		}
	case FocuserType:
		if x, ok := dev.(Focuser); ok {
			sv = focuserStateValues(x)
		}
	case ObservingConditionsType:
		if x, ok := dev.(ObservingConditions); ok {
			sv = observingConditionsStateValues(x)
		}
	case RotatorType:
		if x, ok := dev.(Rotator); ok {
			sv = []StateValue{
				{Name: "IsMoving", Value: x.IsMoving()},
				{Name: "MechanicalPosition", Value: x.MechanicalPosition()},
				{Name: "Position", Value: x.Position()},
			}
		}
	case SafetyMonitorType:
		if x, ok := dev.(SafetyMonitor); ok {
			sv = []StateValue{{Name: "IsSafe", Value: x.IsSafe()}}
		}
	case SwitchType:
		// Switch state is indexed per-switch; DeviceState carries no scalar
		// operational property (only the TimeStamp below).
		sv = []StateValue{}
	case TelescopeType:
		if x, ok := dev.(Telescope); ok {
			sv = telescopeStateValues(x)
		}
	}

	// Merge the driver's own DeviceState on top of the built set. For an
	// unknown/custom type (sv == nil) this is the whole response.
	sv = mergeStateValues(sv, dev.DeviceState())

	// The library supplies the mandatory TimeStamp unless the driver already
	// did (e.g. a hardware-stamped snapshot).
	for _, v := range sv {
		if v.Name == "TimeStamp" {
			return sv
		}
	}
	return append(sv, StateValue{Name: "TimeStamp", Value: nowISO8601()})
}

// mergeStateValues overlays extra onto built: same-name entries replace the
// built value in place, new names append in extra's order.
func mergeStateValues(built, extra []StateValue) []StateValue {
	for _, e := range extra {
		replaced := false
		for i := range built {
			if built[i].Name == e.Name {
				built[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			built = append(built, e)
		}
	}
	return built
}

func nowISO8601() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func appendIfOK(sv []StateValue, name string, v float64, err error) []StateValue {
	if err == nil {
		sv = append(sv, StateValue{Name: name, Value: v})
	}
	return sv
}

func cameraStateValues(c Camera) []StateValue {
	sv := []StateValue{
		{Name: "CameraState", Value: int(c.CameraState())},
		{Name: "ImageReady", Value: c.ImageReady()},
		{Name: "IsPulseGuiding", Value: c.IsPulseGuiding()},
		{Name: "PercentCompleted", Value: c.PercentCompleted()},
	}
	ccd, ccdErr := c.CCDTemperature()
	sv = appendIfOK(sv, "CCDTemperature", ccd, ccdErr)
	cp, cpErr := c.CoolerPower()
	sv = appendIfOK(sv, "CoolerPower", cp, cpErr)
	hs, hsErr := c.HeatSinkTemperature()
	sv = appendIfOK(sv, "HeatSinkTemperature", hs, hsErr)
	return sv
}

func coverCalibratorStateValues(cc CoverCalibrator) []StateValue {
	return []StateValue{
		{Name: "Brightness", Value: cc.Brightness()},
		{Name: "CalibratorState", Value: int(cc.CalibratorState())},
		{Name: "CoverState", Value: int(cc.CoverState())},
		{Name: "CalibratorChanging", Value: cc.CalibratorChanging()},
		{Name: "CoverMoving", Value: cc.CoverMoving()},
	}
}

func domeStateValues(d Dome) []StateValue {
	sv := []StateValue{
		{Name: "AtHome", Value: d.AtHome()},
		{Name: "AtPark", Value: d.AtPark()},
		{Name: "Slewing", Value: d.Slewing()},
	}
	alt, altErr := d.Altitude()
	sv = appendIfOK(sv, "Altitude", alt, altErr)
	az, azErr := d.Azimuth()
	sv = appendIfOK(sv, "Azimuth", az, azErr)
	if ss, err := d.ShutterStatus(); err == nil {
		sv = append(sv, StateValue{Name: "ShutterStatus", Value: int(ss)})
	}
	return sv
}

func focuserStateValues(f Focuser) []StateValue {
	sv := []StateValue{{Name: "IsMoving", Value: f.IsMoving()}}
	if pos, err := f.Position(); err == nil {
		sv = append(sv, StateValue{Name: "Position", Value: pos})
	}
	t, tErr := f.Temperature()
	sv = appendIfOK(sv, "Temperature", t, tErr)
	return sv
}

func observingConditionsStateValues(oc ObservingConditions) []StateValue {
	var sv []StateValue
	type entry struct {
		name string
		fn   func() (float64, error)
	}
	for _, e := range []entry{
		{"CloudCover", oc.CloudCover},
		{"DewPoint", oc.DewPoint},
		{"Humidity", oc.Humidity},
		{"Pressure", oc.Pressure},
		{"RainRate", oc.RainRate},
		{"SkyBrightness", oc.SkyBrightness},
		{"SkyQuality", oc.SkyQuality},
		{"SkyTemperature", oc.SkyTemperature},
		{"StarFWHM", oc.StarFWHM},
		{"Temperature", oc.Temperature},
		{"WindDirection", oc.WindDirection},
		{"WindGust", oc.WindGust},
		{"WindSpeed", oc.WindSpeed},
	} {
		v, err := e.fn()
		sv = appendIfOK(sv, e.name, v, err)
	}
	return sv
}

func telescopeStateValues(t Telescope) []StateValue {
	return []StateValue{
		{Name: "Altitude", Value: t.Altitude()},
		{Name: "AtHome", Value: t.AtHome()},
		{Name: "AtPark", Value: t.AtPark()},
		{Name: "Azimuth", Value: t.Azimuth()},
		{Name: "Declination", Value: t.Declination()},
		{Name: "IsPulseGuiding", Value: t.IsPulseGuiding()},
		{Name: "RightAscension", Value: t.RightAscension()},
		{Name: "SideOfPier", Value: int(t.SideOfPier())},
		{Name: "SiderealTime", Value: t.SiderealTime()},
		{Name: "Slewing", Value: t.Slewing()},
		{Name: "Tracking", Value: t.Tracking()},
		{Name: "UTCDate", Value: t.UTCDate()},
		// Only the ASCOM-defined operational set: ConformU flags extra
		// entries as issues. A driver that wants rate values batched for a
		// specific client (e.g. the NINA 10Micron plugin) can add them via
		// its own DeviceState() override, which is merged on top.
	}
}
