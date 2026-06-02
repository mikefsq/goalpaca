package client

import (
	"net/url"
	"strconv"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// Focuser is a client for an ASCOM Focuser device.
type Focuser struct{ Device }

// NewFocuser returns a client for the focuser at the given Alpaca address
// (host:port or a full URL) and device number.
func NewFocuser(address string, deviceNumber int, opts ...Option) *Focuser {
	return &Focuser{newDevice(address, alpaca.FocuserType, deviceNumber, opts...)}
}

func (f *Focuser) Absolute() (bool, error)          { return f.getBool("absolute") }
func (f *Focuser) IsMoving() (bool, error)          { return f.getBool("ismoving") }
func (f *Focuser) MaxIncrement() (int, error)       { return f.getInt("maxincrement") }
func (f *Focuser) MaxStep() (int, error)            { return f.getInt("maxstep") }
func (f *Focuser) Position() (int, error)           { return f.getInt("position") }
func (f *Focuser) StepSize() (float64, error)       { return f.getFloat("stepsize") }
func (f *Focuser) TempComp() (bool, error)          { return f.getBool("tempcomp") }
func (f *Focuser) TempCompAvailable() (bool, error) { return f.getBool("tempcompavailable") }
func (f *Focuser) Temperature() (float64, error)    { return f.getFloat("temperature") }

func (f *Focuser) SetTempComp(on bool) error {
	return f.put("tempcomp", url.Values{"TempComp": {boolParam(on)}})
}
func (f *Focuser) Halt() error { return f.put("halt", nil) }
func (f *Focuser) Move(position int) error {
	return f.put("move", url.Values{"Position": {strconv.Itoa(position)}})
}
