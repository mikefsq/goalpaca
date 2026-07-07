package sim

import (
	"fmt"
	"sync"

	"github.com/mikefsq/goalpaca/server"
)

// Switch is a simulated ASCOM Switch with N analog switches (value 0–100, the
// boolean view is value > 0). The server library validates the switch Id and
// value ranges and gates the async members on CanAsync before ever calling
// into this type, so it implements only the simulated hardware behavior.
type Switch struct {
	server.BaseSwitch

	mu     sync.Mutex
	n      int
	values []float64
	names  []string
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

func (s *Switch) MaxSwitch() int { return s.n }

func (s *Switch) CanWrite(int) (bool, error) { return true, nil }

func (s *Switch) GetSwitch(id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[id] > 0, nil
}

func (s *Switch) GetSwitchName(id int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.names != nil && s.names[id] != "" {
		return s.names[id], nil
	}
	return fmt.Sprintf("Switch %d", id), nil
}

func (s *Switch) GetSwitchDescription(id int) (string, error) {
	return fmt.Sprintf("Simulated switch %d", id), nil
}

func (s *Switch) GetSwitchValue(id int) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[id], nil
}

func (s *Switch) MaxSwitchValue(int) (float64, error) { return 100, nil }

func (s *Switch) MinSwitchValue(int) (float64, error) { return 0, nil }

func (s *Switch) SwitchStep(int) (float64, error) { return 1, nil }

func (s *Switch) SetSwitch(id int, state bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state {
		s.values[id] = 100
	} else {
		s.values[id] = 0
	}
	return nil
}

// SetSwitchName stores the name; ConformU sets one and verifies the readback.
func (s *Switch) SetSwitchName(id int, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.names == nil {
		s.names = make([]string, s.n)
	}
	s.names[id] = name
	return nil
}

func (s *Switch) SetSwitchValue(id int, value float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[id] = value
	return nil
}

// --- ISwitchV3 async members ---
//
// This simulator has no asynchronous switches: CanAsync is false for every
// id, so the server library rejects SetAsync/SetAsyncValue/CancelAsync with
// NotImplemented before calling these.

func (s *Switch) CanAsync(int) (bool, error) { return false, nil }

// StateChangeComplete reports true for every switch: no async operation is
// ever in flight on this simulator.
func (s *Switch) StateChangeComplete(int) (bool, error) { return true, nil }

func (s *Switch) SetAsync(int, bool) error { return server.ErrNotImplemented }

func (s *Switch) SetAsyncValue(int, float64) error { return server.ErrNotImplemented }

func (s *Switch) CancelAsync(int) error { return server.ErrNotImplemented }

// DeviceState publishes the per-switch operational values (merged onto the
// library-built set): ISwitchV3 defines GetSwitchN / GetSwitchValueN /
// StateChangeCompleteN entries per switch id.
func (s *Switch) DeviceState() []server.StateValue {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv := make([]server.StateValue, 0, 3*s.n)
	for i := 0; i < s.n; i++ {
		sv = append(sv,
			server.StateValue{Name: fmt.Sprintf("GetSwitch%d", i), Value: s.values[i] > 0},
			server.StateValue{Name: fmt.Sprintf("GetSwitchValue%d", i), Value: s.values[i]},
			server.StateValue{Name: fmt.Sprintf("StateChangeComplete%d", i), Value: true},
		)
	}
	return sv
}
