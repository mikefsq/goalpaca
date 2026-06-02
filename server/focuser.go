package alpacadev

// Focuser is the ASCOM Focuser interface (IFocuserV3/V4). Move is an initiator;
// IsMoving is the completion property. Position/StepSize/Temperature return an
// error when the focuser cannot report them (e.g. Position on a relative focuser).
type Focuser interface {
	Device

	Absolute() bool
	IsMoving() bool
	MaxIncrement() int
	MaxStep() int
	Position() (int, error)
	StepSize() (float64, error)
	TempComp() bool
	SetTempComp(bool) error
	TempCompAvailable() bool
	Temperature() (float64, error)

	Halt() error
	Move(position int) error // initiator
}

// BaseFocuser provides not-implemented / zero defaults for Focuser.
type BaseFocuser struct {
	BaseDevice
}

func (b *BaseFocuser) Absolute() bool                { return false }
func (b *BaseFocuser) IsMoving() bool                { return false }
func (b *BaseFocuser) MaxIncrement() int             { return 0 }
func (b *BaseFocuser) MaxStep() int                  { return 0 }
func (b *BaseFocuser) Position() (int, error)        { return 0, ErrNotImplemented }
func (b *BaseFocuser) StepSize() (float64, error)    { return 0, ErrNotImplemented }
func (b *BaseFocuser) TempComp() bool                { return false }
func (b *BaseFocuser) SetTempComp(bool) error        { return ErrNotImplemented }
func (b *BaseFocuser) TempCompAvailable() bool       { return false }
func (b *BaseFocuser) Temperature() (float64, error) { return 0, ErrNotImplemented }
func (b *BaseFocuser) Halt() error                   { return ErrNotImplemented }
func (b *BaseFocuser) Move(int) error                { return ErrNotImplemented }

func focuserGet(member string, f Focuser, _ params) (any, bool, error) {
	switch member {
	case "absolute":
		return f.Absolute(), true, nil
	case "ismoving":
		return f.IsMoving(), true, nil
	case "maxincrement":
		return f.MaxIncrement(), true, nil
	case "maxstep":
		return f.MaxStep(), true, nil
	case "position":
		v, err := f.Position()
		return v, true, err
	case "stepsize":
		v, err := f.StepSize()
		return v, true, err
	case "tempcomp":
		return f.TempComp(), true, nil
	case "tempcompavailable":
		return f.TempCompAvailable(), true, nil
	case "temperature":
		v, err := f.Temperature()
		return v, true, err
	}
	return nil, false, nil
}

func focuserPut(member string, f Focuser, p params) (bool, error) {
	switch member {
	case "tempcomp":
		b, err := p.reqBool("TempComp")
		if err != nil {
			return true, err
		}
		return true, f.SetTempComp(b)
	case "halt":
		return true, f.Halt()
	case "move":
		n, err := p.reqInt("Position")
		if err != nil {
			return true, err
		}
		return true, f.Move(n)
	}
	return false, nil
}
