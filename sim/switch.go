package sim

import (
	"fmt"
	"sync"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// Switch is a simulated ASCOM Switch with N analog switches (value 0–100, the
// boolean view is value > 0).
type Switch struct {
	alpacadev.BaseSwitch

	mu     sync.Mutex
	n      int
	values []float64
}

// SwitchOption configures a simulated Switch.
type SwitchOption func(*Switch)

// WithSwitches sets the number of switches (default 4).
func WithSwitches(n int) SwitchOption {
	return func(s *Switch) { s.n = n; s.values = make([]float64, n) }
}

// NewSwitch creates a simulated Switch with 4 analog switches.
func NewSwitch(opts ...SwitchOption) *Switch {
	s := &Switch{n: 4, values: make([]float64, 4)}
	s.ID = "goalpaca-sim-switch-1"
	s.DevName = "Alpaca Switch Simulator"
	s.Desc = "goalpaca simulated switch"
	s.Info = "goalpaca sim"
	s.Version = "1.0"
	s.IfaceVer = 3
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Switch) valid(id int) bool { return id >= 0 && id < s.n }

func (s *Switch) MaxSwitch() int { return s.n }

func (s *Switch) CanWrite(id int) (bool, error) {
	if !s.valid(id) {
		return false, alpacadev.ErrInvalidValue
	}
	return true, nil
}

func (s *Switch) GetSwitch(id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid(id) {
		return false, alpacadev.ErrInvalidValue
	}
	return s.values[id] > 0, nil
}

func (s *Switch) GetSwitchName(id int) (string, error) {
	if !s.valid(id) {
		return "", alpacadev.ErrInvalidValue
	}
	return fmt.Sprintf("Switch %d", id), nil
}

func (s *Switch) GetSwitchDescription(id int) (string, error) {
	if !s.valid(id) {
		return "", alpacadev.ErrInvalidValue
	}
	return fmt.Sprintf("Simulated switch %d", id), nil
}

func (s *Switch) GetSwitchValue(id int) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid(id) {
		return 0, alpacadev.ErrInvalidValue
	}
	return s.values[id], nil
}

func (s *Switch) MaxSwitchValue(id int) (float64, error) {
	if !s.valid(id) {
		return 0, alpacadev.ErrInvalidValue
	}
	return 100, nil
}

func (s *Switch) MinSwitchValue(id int) (float64, error) {
	if !s.valid(id) {
		return 0, alpacadev.ErrInvalidValue
	}
	return 0, nil
}

func (s *Switch) SwitchStep(id int) (float64, error) {
	if !s.valid(id) {
		return 0, alpacadev.ErrInvalidValue
	}
	return 1, nil
}

func (s *Switch) SetSwitch(id int, state bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid(id) {
		return alpacadev.ErrInvalidValue
	}
	if state {
		s.values[id] = 100
	} else {
		s.values[id] = 0
	}
	return nil
}

func (s *Switch) SetSwitchName(id int, _ string) error {
	if !s.valid(id) {
		return alpacadev.ErrInvalidValue
	}
	return nil
}

func (s *Switch) SetSwitchValue(id int, value float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid(id) {
		return alpacadev.ErrInvalidValue
	}
	if value < 0 || value > 100 {
		return alpacadev.ErrInvalidValue
	}
	s.values[id] = value
	return nil
}
