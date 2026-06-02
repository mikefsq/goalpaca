package alpacadev

// Rotator is the ASCOM Rotator interface (IRotatorV3/V4). Move/MoveAbsolute/
// MoveMechanical are initiators; IsMoving is the completion property. Angles
// are in degrees.
type Rotator interface {
	Device

	CanReverse() bool
	IsMoving() bool
	MechanicalPosition() float64
	Position() float64
	Reverse() bool
	SetReverse(bool) error
	StepSize() float64
	TargetPosition() float64

	Halt() error
	Move(relative float64) error           // initiator (relative)
	MoveAbsolute(position float64) error   // initiator
	MoveMechanical(position float64) error // initiator
	Sync(position float64) error
}

// BaseRotator provides not-implemented / zero defaults for Rotator.
type BaseRotator struct {
	BaseDevice
}

func (b *BaseRotator) CanReverse() bool             { return false }
func (b *BaseRotator) IsMoving() bool               { return false }
func (b *BaseRotator) MechanicalPosition() float64  { return 0 }
func (b *BaseRotator) Position() float64            { return 0 }
func (b *BaseRotator) Reverse() bool                { return false }
func (b *BaseRotator) SetReverse(bool) error        { return ErrNotImplemented }
func (b *BaseRotator) StepSize() float64            { return 0 }
func (b *BaseRotator) TargetPosition() float64      { return 0 }
func (b *BaseRotator) Halt() error                  { return ErrNotImplemented }
func (b *BaseRotator) Move(float64) error           { return ErrNotImplemented }
func (b *BaseRotator) MoveAbsolute(float64) error   { return ErrNotImplemented }
func (b *BaseRotator) MoveMechanical(float64) error { return ErrNotImplemented }
func (b *BaseRotator) Sync(float64) error           { return ErrNotImplemented }

func rotatorGet(member string, r Rotator, _ params) (any, bool, error) {
	switch member {
	case "canreverse":
		return r.CanReverse(), true, nil
	case "ismoving":
		return r.IsMoving(), true, nil
	case "mechanicalposition":
		return r.MechanicalPosition(), true, nil
	case "position":
		return r.Position(), true, nil
	case "reverse":
		return r.Reverse(), true, nil
	case "stepsize":
		return r.StepSize(), true, nil
	case "targetposition":
		return r.TargetPosition(), true, nil
	}
	return nil, false, nil
}

func rotatorPut(member string, r Rotator, p params) (bool, error) {
	switch member {
	case "reverse":
		b, err := p.reqBool("Reverse")
		if err != nil {
			return true, err
		}
		return true, r.SetReverse(b)
	case "halt":
		return true, r.Halt()
	case "move":
		f, err := p.reqFloat("Position")
		if err != nil {
			return true, err
		}
		return true, r.Move(f)
	case "moveabsolute":
		f, err := p.reqFloat("Position")
		if err != nil {
			return true, err
		}
		return true, r.MoveAbsolute(f)
	case "movemechanical":
		f, err := p.reqFloat("Position")
		if err != nil {
			return true, err
		}
		return true, r.MoveMechanical(f)
	case "sync":
		f, err := p.reqFloat("Position")
		if err != nil {
			return true, err
		}
		return true, r.Sync(f)
	}
	return false, nil
}
