// Package sim provides ASCOM Alpaca device simulators built on the goalpaca
// server library. Each simulator is an ordinary driver (it implements the typed
// device interface and embeds the matching Base type), but models realistic
// behaviour — validated writes, connection gating, and time-based asynchronous
// motion — so it passes ConformU and serves as a hardware-free test target and
// a reference for driver authors. Behaviour mirrors the official
// ASCOM.Alpaca.Simulators reference devices.
package sim

import (
	"math"
	"sync"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// Rotator is a simulated ASCOM Rotator. The position converges on the target at
// a fixed rotation rate, computed from the clock (no background goroutine), wraps
// 0–360°, and supports a sync offset between mechanical and sky position.
type Rotator struct {
	alpacadev.BaseRotator

	mu         sync.Mutex
	rate       float64 // degrees per second
	canReverse bool
	reverse    bool
	syncOffset float64 // sky = wrap(mechanical + syncOffset)

	start     float64 // mechanical position at the start of the current move
	target    float64 // mechanical target
	startTime time.Time
	moving    bool
}

// RotatorOption configures a simulated Rotator.
type RotatorOption func(*Rotator)

// WithRotationRate sets the simulated rotation rate in degrees per second.
func WithRotationRate(degPerSec float64) RotatorOption {
	return func(r *Rotator) { r.rate = degPerSec }
}

// NewRotator creates a simulated Rotator at 0°.
func NewRotator(opts ...RotatorOption) *Rotator {
	r := &Rotator{rate: 3.0, canReverse: true}
	r.ID = "goalpaca-sim-rotator-1"
	r.DevName = "Alpaca Rotator Simulator"
	r.Desc = "goalpaca simulated rotator"
	r.Info = "goalpaca sim"
	r.Version = "1.0"
	r.IfaceVer = 4
	for _, o := range opts {
		o(r)
	}
	return r
}

// --- motion model (compute-on-read) ---

// currentMechanicalLocked returns the present mechanical angle, settling the move
// to the target once it has arrived. Caller holds r.mu.
func (r *Rotator) currentMechanicalLocked() float64 {
	if !r.moving {
		return r.start
	}
	dist := shortestDelta(r.start, r.target)
	traveled := time.Since(r.startTime).Seconds() * r.rate
	if traveled >= math.Abs(dist) {
		r.start = r.target // arrived
		r.moving = false
		return r.start
	}
	return wrap360(r.start + math.Copysign(traveled, dist))
}

// beginMoveLocked starts a move to the given mechanical target. Caller holds r.mu.
func (r *Rotator) beginMoveLocked(targetMech float64) {
	r.start = r.currentMechanicalLocked()
	r.target = wrap360(targetMech)
	r.startTime = time.Now()
	r.moving = shortestDelta(r.start, r.target) != 0
}

// --- ASCOM Rotator members ---

func (r *Rotator) CanReverse() bool { return r.canReverse }

func (r *Rotator) IsMoving() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentMechanicalLocked() // settle if arrived
	return r.moving
}

func (r *Rotator) MechanicalPosition() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentMechanicalLocked()
}

func (r *Rotator) Position() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return wrap360(r.currentMechanicalLocked() + r.syncOffset)
}

func (r *Rotator) TargetPosition() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return wrap360(r.target + r.syncOffset)
}

func (r *Rotator) Reverse() bool { return r.reverse }

func (r *Rotator) SetReverse(v bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reverse = v
	return nil
}

func (r *Rotator) StepSize() float64 { return r.rate * 0.25 }

func (r *Rotator) Halt() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.start = r.currentMechanicalLocked()
	r.target = r.start
	r.moving = false
	return nil
}

// MoveAbsolute slews to an absolute sky position (0 ≤ position < 360).
func (r *Rotator) MoveAbsolute(position float64) error {
	if position < 0 || position >= 360 {
		return alpacadev.ErrInvalidValue
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beginMoveLocked(position - r.syncOffset)
	return nil
}

// Move slews by a relative offset (-360 < relative < 360).
func (r *Rotator) Move(relative float64) error {
	if relative <= -360 || relative >= 360 {
		return alpacadev.ErrInvalidValue
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beginMoveLocked(r.currentMechanicalLocked() + relative)
	return nil
}

// MoveMechanical slews to an absolute mechanical position (0 ≤ position < 360),
// ignoring the sync offset.
func (r *Rotator) MoveMechanical(position float64) error {
	if position < 0 || position >= 360 {
		return alpacadev.ErrInvalidValue
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beginMoveLocked(position)
	return nil
}

// Sync sets the sync offset so the current position reads as the given sky angle.
func (r *Rotator) Sync(position float64) error {
	if position < 0 || position >= 360 {
		return alpacadev.ErrInvalidValue
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncOffset = wrap360(position - r.currentMechanicalLocked())
	return nil
}

func wrap360(a float64) float64 {
	a = math.Mod(a, 360)
	if a < 0 {
		a += 360
	}
	return a
}

// shortestDelta is the signed shortest angular distance from→to in (-180, 180].
func shortestDelta(from, to float64) float64 {
	d := math.Mod(to-from, 360)
	if d < -180 {
		d += 360
	}
	if d > 180 {
		d -= 360
	}
	return d
}
