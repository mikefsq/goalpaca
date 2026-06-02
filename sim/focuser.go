package sim

import (
	"sync"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// Focuser is a simulated absolute ASCOM Focuser. The position converges on the
// target at a fixed step rate (computed from the clock).
type Focuser struct {
	alpacadev.BaseFocuser

	mu        sync.Mutex
	maxStep   int
	stepRate  float64 // steps per second
	start     int     // position at the start of the current move
	target    int
	startTime time.Time
	moving    bool
	tempComp  bool
}

// FocuserOption configures a simulated Focuser.
type FocuserOption func(*Focuser)

// WithStepRate sets the simulated movement rate in steps per second.
func WithStepRate(stepsPerSec float64) FocuserOption {
	return func(f *Focuser) { f.stepRate = stepsPerSec }
}

// NewFocuser creates a simulated Focuser at position 0.
func NewFocuser(opts ...FocuserOption) *Focuser {
	f := &Focuser{maxStep: 50000, stepRate: 5000}
	f.ID = "goalpaca-sim-focuser-1"
	f.DevName = "Alpaca Focuser Simulator"
	f.Desc = "goalpaca simulated focuser"
	f.Info = "goalpaca sim"
	f.Version = "1.0"
	f.IfaceVer = 4
	for _, o := range opts {
		o(f)
	}
	return f
}

func (f *Focuser) currentLocked() int {
	if !f.moving {
		return f.start
	}
	dist := f.target - f.start
	mag := dist
	if mag < 0 {
		mag = -mag
	}
	traveled := int(time.Since(f.startTime).Seconds() * f.stepRate)
	if traveled >= mag {
		f.start = f.target
		f.moving = false
		return f.start
	}
	if dist >= 0 {
		return f.start + traveled
	}
	return f.start - traveled
}

func (f *Focuser) Absolute() bool                { return true }
func (f *Focuser) MaxStep() int                  { return f.maxStep }
func (f *Focuser) MaxIncrement() int             { return f.maxStep }
func (f *Focuser) StepSize() (float64, error)    { return 1.0, nil } // microns/step
func (f *Focuser) TempCompAvailable() bool       { return true }
func (f *Focuser) Temperature() (float64, error) { return 15.0, nil }

func (f *Focuser) TempComp() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tempComp
}

func (f *Focuser) SetTempComp(v bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tempComp = v
	return nil
}

func (f *Focuser) Position() (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.currentLocked(), nil
}

func (f *Focuser) IsMoving() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentLocked() // settle if arrived
	return f.moving
}

func (f *Focuser) Move(target int) error {
	// Absolute focuser: gracefully clamp to the travel limits rather than
	// rejecting an out-of-range target (ASCOM / ConformU requirement).
	if target < 0 {
		target = 0
	}
	if target > f.maxStep {
		target = f.maxStep
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.start = f.currentLocked()
	f.target = target
	f.startTime = time.Now()
	f.moving = f.start != f.target
	return nil
}

func (f *Focuser) Halt() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.start = f.currentLocked()
	f.target = f.start
	f.moving = false
	return nil
}
