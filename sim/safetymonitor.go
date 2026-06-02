package sim

import alpacadev "github.com/mikefsq/goalpaca/server"

// SafetyMonitor is a simulated ASCOM SafetyMonitor (defaults to safe).
type SafetyMonitor struct {
	alpacadev.BaseSafetyMonitor
	safe bool
}

// SafetyMonitorOption configures a simulated SafetyMonitor.
type SafetyMonitorOption func(*SafetyMonitor)

// WithSafe sets the initial safety state.
func WithSafe(safe bool) SafetyMonitorOption { return func(s *SafetyMonitor) { s.safe = safe } }

// NewSafetyMonitor creates a simulated SafetyMonitor.
func NewSafetyMonitor(opts ...SafetyMonitorOption) *SafetyMonitor {
	s := &SafetyMonitor{safe: true}
	s.ID = "goalpaca-sim-safetymonitor-1"
	s.DevName = "Alpaca SafetyMonitor Simulator"
	s.Desc = "goalpaca simulated safety monitor"
	s.Info = "goalpaca sim"
	s.Version = "1.0"
	s.IfaceVer = 3
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *SafetyMonitor) IsSafe() bool { return s.safe }
