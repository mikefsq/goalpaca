package alpacadev

// SafetyMonitor is the ASCOM SafetyMonitor interface (ISafetyMonitorV3).
type SafetyMonitor interface {
	Device

	IsSafe() bool
}

// BaseSafetyMonitor provides defaults for SafetyMonitor (defaults to unsafe).
type BaseSafetyMonitor struct {
	BaseDevice
}

func (b *BaseSafetyMonitor) IsSafe() bool { return false }

func safetyMonitorGet(member string, sm SafetyMonitor, _ params) (any, bool, error) {
	switch member {
	case "issafe":
		return sm.IsSafe(), true, nil
	}
	return nil, false, nil
}
