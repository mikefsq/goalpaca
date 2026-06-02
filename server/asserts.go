package alpacadev

// Compile-time guarantees that the Base* helpers fully satisfy their contracts.
// If a member is added to an interface but not to its Base*, the build breaks
// here rather than at a driver site.
var (
	_ Device              = (*BaseDevice)(nil)
	_ Camera              = (*BaseCamera)(nil)
	_ CoverCalibrator     = (*BaseCoverCalibrator)(nil)
	_ Dome                = (*BaseDome)(nil)
	_ FilterWheel         = (*BaseFilterWheel)(nil)
	_ Focuser             = (*BaseFocuser)(nil)
	_ ObservingConditions = (*BaseObservingConditions)(nil)
	_ Rotator             = (*BaseRotator)(nil)
	_ SafetyMonitor       = (*BaseSafetyMonitor)(nil)
	_ Switch              = (*BaseSwitch)(nil)
	_ Telescope           = (*BaseTelescope)(nil)
)
