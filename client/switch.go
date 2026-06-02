package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// Switch is a client for an ASCOM Switch device. Most members are indexed by a
// switch Id in the range 0..MaxSwitch-1.
type Switch struct{ Device }

// NewSwitch returns a client for the switch device at the given Alpaca address
// and device number.
func NewSwitch(address string, deviceNumber int, opts ...Option) *Switch {
	return &Switch{newDevice(address, alpaca.SwitchType, deviceNumber, opts...)}
}

// MaxSwitch is the only member without an Id parameter.
func (s *Switch) MaxSwitch() (int, error) { return s.getInt("maxswitch") }

func (s *Switch) idGetBool(member string, id int) (bool, error) {
	var v bool
	err := s.getInto(member, url.Values{"Id": {intParam(id)}}, &v)
	return v, err
}
func (s *Switch) idGetFloat(member string, id int) (float64, error) {
	var v float64
	err := s.getInto(member, url.Values{"Id": {intParam(id)}}, &v)
	return v, err
}
func (s *Switch) idGetString(member string, id int) (string, error) {
	var v string
	err := s.getInto(member, url.Values{"Id": {intParam(id)}}, &v)
	return v, err
}

func (s *Switch) CanWrite(id int) (bool, error)  { return s.idGetBool("canwrite", id) }
func (s *Switch) GetSwitch(id int) (bool, error) { return s.idGetBool("getswitch", id) }
func (s *Switch) CanAsync(id int) (bool, error)  { return s.idGetBool("canasync", id) }
func (s *Switch) StateChangeComplete(id int) (bool, error) {
	return s.idGetBool("statechangecomplete", id)
}
func (s *Switch) GetSwitchValue(id int) (float64, error) { return s.idGetFloat("getswitchvalue", id) }
func (s *Switch) MaxSwitchValue(id int) (float64, error) { return s.idGetFloat("maxswitchvalue", id) }
func (s *Switch) MinSwitchValue(id int) (float64, error) { return s.idGetFloat("minswitchvalue", id) }
func (s *Switch) SwitchStep(id int) (float64, error)     { return s.idGetFloat("switchstep", id) }
func (s *Switch) GetSwitchName(id int) (string, error)   { return s.idGetString("getswitchname", id) }
func (s *Switch) GetSwitchDescription(id int) (string, error) {
	return s.idGetString("getswitchdescription", id)
}

func (s *Switch) SetSwitch(id int, state bool) error {
	return s.put("setswitch", url.Values{"Id": {intParam(id)}, "State": {boolParam(state)}})
}
func (s *Switch) SetSwitchName(id int, name string) error {
	return s.put("setswitchname", url.Values{"Id": {intParam(id)}, "Name": {name}})
}
func (s *Switch) SetSwitchValue(id int, value float64) error {
	return s.put("setswitchvalue", url.Values{"Id": {intParam(id)}, "Value": {floatParam(value)}})
}
func (s *Switch) SetAsync(id int, state bool) error {
	return s.put("setasync", url.Values{"Id": {intParam(id)}, "State": {boolParam(state)}})
}
func (s *Switch) SetAsyncValue(id int, value float64) error {
	return s.put("setasyncvalue", url.Values{"Id": {intParam(id)}, "Value": {floatParam(value)}})
}
func (s *Switch) CancelAsync(id int) error {
	return s.put("cancelasync", url.Values{"Id": {intParam(id)}})
}
