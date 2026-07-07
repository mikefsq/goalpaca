// Package alpaca holds the wire vocabulary of the ASCOM Alpaca protocol —
// the device-type names, per-type enums, the error model, and the image
// transport types/codec — shared by the goalpaca server (device hosting) and
// client packages. It is the single source of truth both sides bind to; it
// has no behavior beyond the ImageBytes codec and depends only on the
// standard library.
//
// The server package re-exports everything here under its original names
// (type aliases and identical sentinel values), so code written against
// server.CameraState, server.ErrParked, etc. is unaffected.
package alpaca

// DeviceType is the ASCOM device-type path segment (lowercased on the wire,
// e.g. "camera"). The set is fixed by ASCOM; anything outside it is modeled as
// Switch or Action.
type DeviceType string

const (
	CameraType              DeviceType = "camera"
	CoverCalibratorType     DeviceType = "covercalibrator"
	DomeType                DeviceType = "dome"
	FilterWheelType         DeviceType = "filterwheel"
	FocuserType             DeviceType = "focuser"
	ObservingConditionsType DeviceType = "observingconditions"
	RotatorType             DeviceType = "rotator"
	SafetyMonitorType       DeviceType = "safetymonitor"
	SwitchType              DeviceType = "switch"
	TelescopeType           DeviceType = "telescope"
)

// StateValue is one entry in a DeviceState batch snapshot.
type StateValue struct {
	Name  string
	Value any
}
