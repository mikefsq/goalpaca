package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// Rotator is a client for an ASCOM Rotator device.
type Rotator struct{ Device }

// NewRotator returns a client for the rotator at the given Alpaca address and
// device number.
func NewRotator(address string, deviceNumber int, opts ...Option) *Rotator {
	return &Rotator{newDevice(address, alpaca.RotatorType, deviceNumber, opts...)}
}

func (r *Rotator) CanReverse() (bool, error)            { return r.getBool("canreverse") }
func (r *Rotator) IsMoving() (bool, error)              { return r.getBool("ismoving") }
func (r *Rotator) MechanicalPosition() (float64, error) { return r.getFloat("mechanicalposition") }
func (r *Rotator) Position() (float64, error)           { return r.getFloat("position") }
func (r *Rotator) Reverse() (bool, error)               { return r.getBool("reverse") }
func (r *Rotator) StepSize() (float64, error)           { return r.getFloat("stepsize") }
func (r *Rotator) TargetPosition() (float64, error)     { return r.getFloat("targetposition") }

func (r *Rotator) SetReverse(rev bool) error {
	return r.put("reverse", url.Values{"Reverse": {boolParam(rev)}})
}
func (r *Rotator) Halt() error { return r.put("halt", nil) }
func (r *Rotator) Move(relative float64) error {
	return r.put("move", url.Values{"Position": {floatParam(relative)}})
}
func (r *Rotator) MoveAbsolute(position float64) error {
	return r.put("moveabsolute", url.Values{"Position": {floatParam(position)}})
}
func (r *Rotator) MoveMechanical(position float64) error {
	return r.put("movemechanical", url.Values{"Position": {floatParam(position)}})
}
func (r *Rotator) Sync(position float64) error {
	return r.put("sync", url.Values{"Position": {floatParam(position)}})
}
