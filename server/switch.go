package server

// Switch is the ASCOM Switch interface (ISwitchV3, Platform 7). Most members
// are indexed by a switch Id (0..MaxSwitch-1), passed as the "Id" parameter,
// and include the V3 asynchronous-set members.
type Switch interface {
	Device

	MaxSwitch() int
	CanWrite(id int) (bool, error)
	GetSwitch(id int) (bool, error)
	GetSwitchDescription(id int) (string, error)
	GetSwitchName(id int) (string, error)
	GetSwitchValue(id int) (float64, error)
	MaxSwitchValue(id int) (float64, error)
	MinSwitchValue(id int) (float64, error)
	SwitchStep(id int) (float64, error)

	SetSwitch(id int, state bool) error
	SetSwitchName(id int, name string) error
	SetSwitchValue(id int, value float64) error

	// Asynchronous control (ISwitchV3, Platform 7). SetAsync/SetAsyncValue are
	// initiators; StateChangeComplete is the completion property. All are
	// optional, gated by CanAsync.
	CanAsync(id int) (bool, error)
	SetAsync(id int, state bool) error         // initiator
	SetAsyncValue(id int, value float64) error // initiator
	StateChangeComplete(id int) (bool, error)  // completion
	CancelAsync(id int) error
}

// BaseSwitch provides not-implemented / zero defaults for Switch.
type BaseSwitch struct {
	BaseDevice
}

func (b *BaseSwitch) MaxSwitch() int                           { return 0 }
func (b *BaseSwitch) CanWrite(int) (bool, error)               { return false, nil }
func (b *BaseSwitch) GetSwitch(int) (bool, error)              { return false, ErrNotImplemented }
func (b *BaseSwitch) GetSwitchDescription(int) (string, error) { return "", ErrNotImplemented }
func (b *BaseSwitch) GetSwitchName(int) (string, error)        { return "", ErrNotImplemented }
func (b *BaseSwitch) GetSwitchValue(int) (float64, error)      { return 0, ErrNotImplemented }
func (b *BaseSwitch) MaxSwitchValue(int) (float64, error)      { return 1, nil }
func (b *BaseSwitch) MinSwitchValue(int) (float64, error)      { return 0, nil }
func (b *BaseSwitch) SwitchStep(int) (float64, error)          { return 1, nil }
func (b *BaseSwitch) SetSwitch(int, bool) error                { return ErrNotImplemented }
func (b *BaseSwitch) SetSwitchName(int, string) error          { return ErrNotImplemented }
func (b *BaseSwitch) SetSwitchValue(int, float64) error        { return ErrNotImplemented }
func (b *BaseSwitch) CanAsync(int) (bool, error)               { return false, nil }
func (b *BaseSwitch) SetAsync(int, bool) error                 { return ErrNotImplemented }
func (b *BaseSwitch) SetAsyncValue(int, float64) error         { return ErrNotImplemented }
func (b *BaseSwitch) StateChangeComplete(int) (bool, error)    { return true, nil }
func (b *BaseSwitch) CancelAsync(int) error                    { return ErrNotImplemented }

func switchGet(member string, sw Switch, p params) (any, bool, error) {
	// MaxSwitch is the only member without an Id.
	if member == "maxswitch" {
		return sw.MaxSwitch(), true, nil
	}
	idMembers := map[string]func(int) (any, error){
		"canwrite":             func(id int) (any, error) { return sw.CanWrite(id) },
		"getswitch":            func(id int) (any, error) { return sw.GetSwitch(id) },
		"getswitchdescription": func(id int) (any, error) { return sw.GetSwitchDescription(id) },
		"getswitchname":        func(id int) (any, error) { return sw.GetSwitchName(id) },
		"getswitchvalue":       func(id int) (any, error) { return sw.GetSwitchValue(id) },
		"maxswitchvalue":       func(id int) (any, error) { return sw.MaxSwitchValue(id) },
		"minswitchvalue":       func(id int) (any, error) { return sw.MinSwitchValue(id) },
		"switchstep":           func(id int) (any, error) { return sw.SwitchStep(id) },
		"canasync":             func(id int) (any, error) { return sw.CanAsync(id) },
		"statechangecomplete":  func(id int) (any, error) { return sw.StateChangeComplete(id) },
	}
	fn, ok := idMembers[member]
	if !ok {
		return nil, false, nil
	}
	id, err := p.reqInt("Id")
	if err != nil {
		return nil, true, err
	}
	if err := GateSwitchID(sw, id); err != nil {
		return nil, true, err
	}
	v, err := fn(id)
	return v, true, err
}

// validSwitchID enforces the ISwitchV3 rule that every Id-taking member
// rejects an out-of-range Id (0..MaxSwitch-1) with InvalidValue, before any
// other processing.
func validSwitchID(sw Switch, id int) error {
	if max := sw.MaxSwitch(); id < 0 || id >= max {
		return invalidValuef("switch Id %d is outside the valid range 0 to %d", id, max-1)
	}
	return nil
}

// switchAsyncGate enforces the ISwitchV3 rule that the asynchronous members
// (SetAsync, SetAsyncValue, CancelAsync) return NotImplemented for switches
// whose CanAsync is false.
func switchAsyncGate(sw Switch, id int, member string) error {
	can, err := sw.CanAsync(id)
	if err != nil {
		return err
	}
	if !can {
		return notImplErr(member)
	}
	return nil
}

func switchPut(member string, sw Switch, p params) (bool, error) {
	// All Switch PUT members take an Id. Parameters are parsed first (a
	// malformed request is a protocol error), then the Id is range-checked
	// per ISwitchV3 before the driver is called.
	switch member {
	case "setswitch", "setswitchname", "setswitchvalue",
		"setasync", "setasyncvalue", "cancelasync":
	default:
		return false, nil
	}
	id, err := p.reqInt("Id")
	if err != nil {
		return true, err
	}
	switch member {
	case "setswitch":
		state, err := p.reqBool("State")
		if err != nil {
			return true, err
		}
		if err := GateSwitchID(sw, id); err != nil {
			return true, err
		}
		return true, sw.SetSwitch(id, state)
	case "setswitchname":
		name, _ := p.get("Name")
		if err := GateSwitchID(sw, id); err != nil {
			return true, err
		}
		return true, sw.SetSwitchName(id, name)
	case "setswitchvalue":
		value, err := p.reqFloat("Value")
		if err != nil {
			return true, err
		}
		if err := GateSwitchValue(sw, id, value); err != nil {
			return true, err
		}
		return true, sw.SetSwitchValue(id, value)
	case "setasync":
		state, err := p.reqBool("State")
		if err != nil {
			return true, err
		}
		if err := GateSwitchID(sw, id); err != nil {
			return true, err
		}
		if err := GateSwitchAsync(sw, id, "SetAsync"); err != nil {
			return true, err
		}
		return true, sw.SetAsync(id, state)
	case "setasyncvalue":
		value, err := p.reqFloat("Value")
		if err != nil {
			return true, err
		}
		if err := GateSwitchID(sw, id); err != nil {
			return true, err
		}
		if err := GateSwitchAsync(sw, id, "SetAsyncValue"); err != nil {
			return true, err
		}
		if min, err := sw.MinSwitchValue(id); err == nil {
			if max, merr := sw.MaxSwitchValue(id); merr == nil && (value < min || value > max) {
				return true, invalidValuef("switch %d value %g is outside the valid range %g to %g", id, value, min, max)
			}
		}
		return true, sw.SetAsyncValue(id, value)
	case "cancelasync":
		if err := GateSwitchID(sw, id); err != nil {
			return true, err
		}
		if err := GateSwitchAsync(sw, id, "CancelAsync"); err != nil {
			return true, err
		}
		return true, sw.CancelAsync(id)
	}
	return true, nil // unreachable: member list checked above
}
