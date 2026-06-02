package alpacadev

// FilterWheel is the ASCOM FilterWheel interface (IFilterWheelV2/V3). Setting
// Position initiates a move; Position reads -1 while moving.
type FilterWheel interface {
	Device

	FocusOffsets() []int
	Names() []string
	Position() int
	SetPosition(int) error // initiator (move to slot)
}

// BaseFilterWheel provides not-implemented / zero defaults for FilterWheel.
type BaseFilterWheel struct {
	BaseDevice
}

func (b *BaseFilterWheel) FocusOffsets() []int   { return []int{} }
func (b *BaseFilterWheel) Names() []string       { return []string{} }
func (b *BaseFilterWheel) Position() int         { return 0 }
func (b *BaseFilterWheel) SetPosition(int) error { return ErrNotImplemented }

func filterWheelGet(member string, fw FilterWheel, _ params) (any, bool, error) {
	switch member {
	case "focusoffsets":
		return fw.FocusOffsets(), true, nil
	case "names":
		return fw.Names(), true, nil
	case "position":
		return fw.Position(), true, nil
	}
	return nil, false, nil
}

func filterWheelPut(member string, fw FilterWheel, p params) (bool, error) {
	switch member {
	case "position":
		n, err := p.reqInt("Position")
		if err != nil {
			return true, err
		}
		return true, fw.SetPosition(n)
	}
	return false, nil
}
