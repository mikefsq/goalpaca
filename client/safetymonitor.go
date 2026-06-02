package client

import alpaca "github.com/mikefsq/goalpaca/server"

// SafetyMonitor is a client for an ASCOM SafetyMonitor device.
type SafetyMonitor struct{ Device }

// NewSafetyMonitor returns a client for the safety monitor at the given Alpaca
// address and device number.
func NewSafetyMonitor(address string, deviceNumber int, opts ...Option) *SafetyMonitor {
	return &SafetyMonitor{newDevice(address, alpaca.SafetyMonitorType, deviceNumber, opts...)}
}

func (s *SafetyMonitor) IsSafe() (bool, error) { return s.getBool("issafe") }
