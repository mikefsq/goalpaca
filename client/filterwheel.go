package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// FilterWheel is a client for an ASCOM FilterWheel device.
type FilterWheel struct{ Device }

// NewFilterWheel returns a client for the filter wheel at the given Alpaca
// address and device number.
func NewFilterWheel(address string, deviceNumber int, opts ...Option) *FilterWheel {
	return &FilterWheel{newDevice(address, alpaca.FilterWheelType, deviceNumber, opts...)}
}

func (f *FilterWheel) FocusOffsets() ([]int, error) {
	var v []int
	err := f.getInto("focusoffsets", nil, &v)
	return v, err
}
func (f *FilterWheel) Names() ([]string, error) { return f.getStringList("names") }
func (f *FilterWheel) Position() (int, error)   { return f.getInt("position") }

// SetPosition initiates a move to the given filter slot.
func (f *FilterWheel) SetPosition(slot int) error {
	return f.put("position", url.Values{"Position": {intParam(slot)}})
}
