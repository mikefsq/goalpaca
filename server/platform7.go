package alpacadev

// Platform 7 InterfaceVersion values — the version a driver implementing the
// full Platform 7 contract should report from InterfaceVersion() for each
// device type. (Set BaseDevice.IfaceVer to the matching value.)
//
// All Platform 7 interfaces share the new common members Connect / Disconnect /
// Connecting (async connection) and DeviceState (batched operational state),
// which this library implements for every device type.
const (
	InterfaceVersionCamera              = 4 // ICameraV4
	InterfaceVersionCoverCalibrator     = 2 // ICoverCalibratorV2
	InterfaceVersionDome                = 3 // IDomeV3
	InterfaceVersionFilterWheel         = 3 // IFilterWheelV3
	InterfaceVersionFocuser             = 4 // IFocuserV4
	InterfaceVersionObservingConditions = 2 // IObservingConditionsV2
	InterfaceVersionRotator             = 4 // IRotatorV4
	InterfaceVersionSafetyMonitor       = 3 // ISafetyMonitorV3
	InterfaceVersionSwitch              = 3 // ISwitchV3
	InterfaceVersionTelescope           = 4 // ITelescopeV4
)
