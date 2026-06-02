package sim

import (
	"math"
	"sync"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// Dome is a simulated ASCOM Dome. Azimuth converges on its target along the
// shortest 0–360° path and altitude converges within 0–90°, both at a fixed
// rate computed from the clock (no background goroutine). The shutter runs a
// timed open/close transition. Behaviour mirrors ASCOM.Alpaca.Simulators.
type Dome struct {
	alpacadev.BaseDome

	mu   sync.Mutex
	rate float64 // degrees per second (azimuth and altitude)

	// azimuth motion (shortest path, 0–360 wrap)
	azStart  float64
	azTarget float64
	azTime   time.Time
	azMoving bool

	// altitude motion (clamped 0–90, no wrap)
	altStart  float64
	altTarget float64
	altTime   time.Time
	altMoving bool

	// shutter state machine
	shutterState  alpacadev.ShutterState
	shutterTarget alpacadev.ShutterState
	shutterTime   time.Time

	atHome bool
	atPark bool
}

// DomeOption configures a simulated Dome.
type DomeOption func(*Dome)

// WithDomeRate sets the simulated slew rate in degrees per second.
func WithDomeRate(degPerSec float64) DomeOption {
	return func(d *Dome) { d.rate = degPerSec }
}

// NewDome creates a simulated Dome with the shutter closed at azimuth 0°.
func NewDome(opts ...DomeOption) *Dome {
	d := &Dome{rate: 5.0, shutterState: alpacadev.ShutterClosed, shutterTarget: alpacadev.ShutterClosed}
	d.ID = "goalpaca-sim-dome-1"
	d.DevName = "Alpaca Dome Simulator"
	d.Desc = "goalpaca simulated dome"
	d.Info = "goalpaca sim"
	d.Version = "1.0"
	d.IfaceVer = 3
	for _, o := range opts {
		o(d)
	}
	return d
}

// --- motion model (compute-on-read) ---

// currentAzimuthLocked returns the present azimuth, settling the move and
// AtHome/AtPark once it arrives. Caller holds d.mu.
func (d *Dome) currentAzimuthLocked() float64 {
	if !d.azMoving {
		return d.azStart
	}
	dist := shortestDelta(d.azStart, d.azTarget)
	traveled := time.Since(d.azTime).Seconds() * d.rate
	if traveled >= math.Abs(dist) {
		d.azStart = d.azTarget // arrived
		d.azMoving = false
		return d.azStart
	}
	return wrap360(d.azStart + math.Copysign(traveled, dist))
}

// currentAltitudeLocked returns the present altitude, settling on arrival.
// Caller holds d.mu.
func (d *Dome) currentAltitudeLocked() float64 {
	if !d.altMoving {
		return d.altStart
	}
	dist := d.altTarget - d.altStart
	traveled := time.Since(d.altTime).Seconds() * d.rate
	if traveled >= math.Abs(dist) {
		d.altStart = d.altTarget // arrived
		d.altMoving = false
		return d.altStart
	}
	return d.altStart + math.Copysign(traveled, dist)
}

// currentShutterLocked returns the present shutter state, settling a timed
// transition once it completes. Caller holds d.mu.
func (d *Dome) currentShutterLocked() alpacadev.ShutterState {
	if d.shutterState == d.shutterTarget {
		return d.shutterState
	}
	if time.Since(d.shutterTime).Seconds() >= 1.0 {
		d.shutterState = d.shutterTarget
	}
	return d.shutterState
}

func (d *Dome) beginAzimuthLocked(target float64) {
	d.azStart = d.currentAzimuthLocked()
	d.azTarget = wrap360(target)
	d.azTime = time.Now()
	d.azMoving = shortestDelta(d.azStart, d.azTarget) != 0
}

func (d *Dome) beginAltitudeLocked(target float64) {
	d.altStart = d.currentAltitudeLocked()
	d.altTarget = target
	d.altTime = time.Now()
	d.altMoving = d.altTarget != d.altStart
}

// --- capability flags ---

func (d *Dome) CanFindHome() bool    { return true }
func (d *Dome) CanPark() bool        { return true }
func (d *Dome) CanSetAltitude() bool { return true }
func (d *Dome) CanSetAzimuth() bool  { return true }
func (d *Dome) CanSetPark() bool     { return true }
func (d *Dome) CanSetShutter() bool  { return true }
func (d *Dome) CanSlave() bool       { return false }
func (d *Dome) CanSyncAzimuth() bool { return true }

// --- state properties ---

func (d *Dome) Altitude() (float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentAltitudeLocked(), nil
}

func (d *Dome) Azimuth() (float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentAzimuthLocked(), nil
}

func (d *Dome) AtHome() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentAzimuthLocked() // settle
	return d.atHome && !d.azMoving
}

func (d *Dome) AtPark() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentAzimuthLocked() // settle
	return d.atPark && !d.azMoving
}

func (d *Dome) ShutterStatus() (alpacadev.ShutterState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentShutterLocked(), nil
}

func (d *Dome) Slewing() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentAzimuthLocked()  // settle
	d.currentAltitudeLocked() // settle
	return d.azMoving || d.altMoving
}

func (d *Dome) Slaved() bool { return false }

func (d *Dome) SetSlaved(v bool) error {
	if v {
		return alpacadev.ErrNotImplemented
	}
	return nil
}

// --- initiators ---

func (d *Dome) SlewToAzimuth(azimuth float64) error {
	if azimuth < 0 || azimuth >= 360 {
		return alpacadev.ErrInvalidValue
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.beginAzimuthLocked(azimuth)
	d.atHome = false
	d.atPark = false
	return nil
}

func (d *Dome) SlewToAltitude(altitude float64) error {
	if altitude < 0 || altitude > 90 {
		return alpacadev.ErrInvalidValue
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.beginAltitudeLocked(altitude)
	return nil
}

func (d *Dome) SyncToAzimuth(azimuth float64) error {
	if azimuth < 0 || azimuth >= 360 {
		return alpacadev.ErrInvalidValue
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.azStart = wrap360(azimuth)
	d.azTarget = d.azStart
	d.azMoving = false
	return nil
}

func (d *Dome) OpenShutter() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentShutterLocked()
	d.shutterState = alpacadev.ShutterOpening
	d.shutterTarget = alpacadev.ShutterOpen
	d.shutterTime = time.Now()
	return nil
}

func (d *Dome) CloseShutter() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentShutterLocked()
	d.shutterState = alpacadev.ShutterClosing
	d.shutterTarget = alpacadev.ShutterClosed
	d.shutterTime = time.Now()
	return nil
}

func (d *Dome) FindHome() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.beginAzimuthLocked(0)
	d.atHome = true
	d.atPark = false
	return nil
}

func (d *Dome) Park() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.beginAzimuthLocked(0)
	d.atPark = true
	d.atHome = false
	d.currentShutterLocked()
	d.shutterState = alpacadev.ShutterClosing
	d.shutterTarget = alpacadev.ShutterClosed
	d.shutterTime = time.Now()
	return nil
}

func (d *Dome) SetPark() error { return nil }

func (d *Dome) AbortSlew() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.azStart = d.currentAzimuthLocked()
	d.azTarget = d.azStart
	d.azMoving = false
	d.altStart = d.currentAltitudeLocked()
	d.altTarget = d.altStart
	d.altMoving = false
	d.atHome = false
	d.atPark = false
	return nil
}
