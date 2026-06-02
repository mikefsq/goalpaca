package sim

import (
	"sync"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// CoverCalibrator is a simulated ASCOM CoverCalibrator. Both a calibrator and a
// motorised cover are present. Calibrator brightness changes and cover motion
// are modelled as time-based asynchronous transitions computed from the clock
// (no background goroutine): each transition has an "until" timestamp, and the
// in-progress and settled states are computed on read. Behaviour mirrors the
// official ASCOM.Alpaca.Simulators reference device.
type CoverCalibrator struct {
	alpacadev.BaseCoverCalibrator

	mu sync.Mutex

	maxBrightness int
	brightness    int

	// Calibrator transition: while time.Now() < calUntil the calibrator is
	// changing and reports calTransient; once settled it reports calSettled.
	calUntil     time.Time
	calChanging  bool
	calTransient alpacadev.CalibratorStatus
	calSettled   alpacadev.CalibratorStatus

	// Cover transition: while time.Now() < covUntil the cover is moving; once
	// settled it reports covSettled.
	covUntil   time.Time
	covMoving  bool
	covSettled alpacadev.CoverStatus
}

// CoverCalibratorOption configures a simulated CoverCalibrator.
type CoverCalibratorOption func(*CoverCalibrator)

// WithMaxBrightness sets the simulated maximum calibrator brightness.
func WithMaxBrightness(max int) CoverCalibratorOption {
	return func(c *CoverCalibrator) { c.maxBrightness = max }
}

// NewCoverCalibrator creates a simulated CoverCalibrator with the calibrator off
// and the cover closed.
func NewCoverCalibrator(opts ...CoverCalibratorOption) *CoverCalibrator {
	c := &CoverCalibrator{
		maxBrightness: 100,
		brightness:    0,
		calSettled:    alpacadev.CalibratorOff,
		covSettled:    alpacadev.CoverClosed,
	}
	c.ID = "goalpaca-sim-covercalibrator-1"
	c.DevName = "Alpaca CoverCalibrator Simulator"
	c.Desc = "goalpaca simulated cover calibrator"
	c.Info = "goalpaca sim"
	c.Version = "1.0"
	c.IfaceVer = 2
	for _, o := range opts {
		o(c)
	}
	return c
}

// --- transition model (compute-on-read) ---

// settleCalibratorLocked clears the calibrator transition once its deadline has
// passed. Caller holds c.mu.
func (c *CoverCalibrator) settleCalibratorLocked() {
	if c.calChanging && !time.Now().Before(c.calUntil) {
		c.calChanging = false
	}
}

// settleCoverLocked clears the cover transition once its deadline has passed.
// Caller holds c.mu.
func (c *CoverCalibrator) settleCoverLocked() {
	if c.covMoving && !time.Now().Before(c.covUntil) {
		c.covMoving = false
	}
}

// --- ASCOM CoverCalibrator members ---

func (c *CoverCalibrator) MaxBrightness() int { return c.maxBrightness }

func (c *CoverCalibrator) Brightness() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.brightness
}

func (c *CoverCalibrator) CalibratorState() alpacadev.CalibratorStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settleCalibratorLocked()
	if c.calChanging {
		return c.calTransient
	}
	return c.calSettled
}

func (c *CoverCalibrator) CalibratorChanging() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settleCalibratorLocked()
	return c.calChanging
}

func (c *CoverCalibrator) CoverState() alpacadev.CoverStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settleCoverLocked()
	if c.covMoving {
		return alpacadev.CoverMoving
	}
	return c.covSettled
}

func (c *CoverCalibrator) CoverMoving() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settleCoverLocked()
	return c.covMoving
}

// CalibratorOn turns the calibrator on at the given brightness (0 ≤ brightness ≤
// MaxBrightness) and begins a short transition before it reports ready.
func (c *CoverCalibrator) CalibratorOn(brightness int) error {
	if brightness < 0 || brightness > c.maxBrightness {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.brightness = brightness
	c.calChanging = true
	c.calTransient = alpacadev.CalibratorNotReady
	c.calSettled = alpacadev.CalibratorReady
	c.calUntil = time.Now().Add(400 * time.Millisecond)
	return nil
}

// CalibratorOff turns the calibrator off, beginning a short transition before it
// reports off.
func (c *CoverCalibrator) CalibratorOff() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.brightness = 0
	c.calChanging = true
	c.calTransient = alpacadev.CalibratorNotReady
	c.calSettled = alpacadev.CalibratorOff
	c.calUntil = time.Now().Add(400 * time.Millisecond)
	return nil
}

// OpenCover begins opening the cover; it reports moving during the transition and
// open once settled.
func (c *CoverCalibrator) OpenCover() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.covMoving = true
	c.covSettled = alpacadev.CoverOpen
	c.covUntil = time.Now().Add(700 * time.Millisecond)
	return nil
}

// CloseCover begins closing the cover; it reports moving during the transition
// and closed once settled.
func (c *CoverCalibrator) CloseCover() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.covMoving = true
	c.covSettled = alpacadev.CoverClosed
	c.covUntil = time.Now().Add(700 * time.Millisecond)
	return nil
}

// HaltCover stops any cover motion immediately, settling to a stable state.
func (c *CoverCalibrator) HaltCover() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.covMoving = false
	return nil
}
