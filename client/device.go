package client

import (
	"net/http"
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// Common Device (ICommon) members shared by every device type.

func (d *Device) Connected() (bool, error) { return d.getBool("connected") }
func (d *Device) SetConnected(c bool) error {
	return d.put("connected", url.Values{"Connected": {boolParam(c)}})
}
func (d *Device) Connect() error                      { return d.put("connect", nil) } // Platform 7 async initiator
func (d *Device) Disconnect() error                   { return d.put("disconnect", nil) }
func (d *Device) Connecting() (bool, error)           { return d.getBool("connecting") }
func (d *Device) Name() (string, error)               { return d.getString("name") }
func (d *Device) Description() (string, error)        { return d.getString("description") }
func (d *Device) DriverInfo() (string, error)         { return d.getString("driverinfo") }
func (d *Device) DriverVersion() (string, error)      { return d.getString("driverversion") }
func (d *Device) InterfaceVersion() (int, error)      { return d.getInt("interfaceversion") }
func (d *Device) SupportedActions() ([]string, error) { return d.getStringList("supportedactions") }

// Action invokes a device-specific action and returns its string result.
func (d *Device) Action(name, parameters string) (string, error) {
	var v string
	err := d.call(http.MethodPut, "action", url.Values{"Action": {name}, "Parameters": {parameters}}, &v)
	return v, err
}

func (d *Device) CommandString(command string, raw bool) (string, error) {
	var v string
	err := d.call(http.MethodPut, "commandstring", url.Values{"Command": {command}, "Raw": {boolParam(raw)}}, &v)
	return v, err
}

func (d *Device) CommandBool(command string, raw bool) (bool, error) {
	var v bool
	err := d.call(http.MethodPut, "commandbool", url.Values{"Command": {command}, "Raw": {boolParam(raw)}}, &v)
	return v, err
}

func (d *Device) CommandBlind(command string, raw bool) error {
	return d.put("commandblind", url.Values{"Command": {command}, "Raw": {boolParam(raw)}})
}

// DeviceState returns the Platform 7 operational snapshot ({Name, Value} pairs).
func (d *Device) DeviceState() ([]alpaca.StateValue, error) {
	var v []alpaca.StateValue
	err := d.getInto("devicestate", nil, &v)
	return v, err
}
