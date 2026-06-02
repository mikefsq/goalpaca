package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// Dome is a client for an ASCOM Dome device.
type Dome struct{ Device }

// NewDome returns a client for the dome at the given Alpaca address and number.
func NewDome(address string, deviceNumber int, opts ...Option) *Dome {
	return &Dome{newDevice(address, alpaca.DomeType, deviceNumber, opts...)}
}

func (d *Dome) Altitude() (float64, error)    { return d.getFloat("altitude") }
func (d *Dome) Azimuth() (float64, error)     { return d.getFloat("azimuth") }
func (d *Dome) AtHome() (bool, error)         { return d.getBool("athome") }
func (d *Dome) AtPark() (bool, error)         { return d.getBool("atpark") }
func (d *Dome) CanFindHome() (bool, error)    { return d.getBool("canfindhome") }
func (d *Dome) CanPark() (bool, error)        { return d.getBool("canpark") }
func (d *Dome) CanSetAltitude() (bool, error) { return d.getBool("cansetaltitude") }
func (d *Dome) CanSetAzimuth() (bool, error)  { return d.getBool("cansetazimuth") }
func (d *Dome) CanSetPark() (bool, error)     { return d.getBool("cansetpark") }
func (d *Dome) CanSetShutter() (bool, error)  { return d.getBool("cansetshutter") }
func (d *Dome) CanSlave() (bool, error)       { return d.getBool("canslave") }
func (d *Dome) CanSyncAzimuth() (bool, error) { return d.getBool("cansyncazimuth") }
func (d *Dome) Slaved() (bool, error)         { return d.getBool("slaved") }
func (d *Dome) Slewing() (bool, error)        { return d.getBool("slewing") }

func (d *Dome) ShutterStatus() (alpaca.ShutterState, error) {
	v, err := d.getInt("shutterstatus")
	return alpaca.ShutterState(v), err
}

func (d *Dome) SetSlaved(s bool) error { return d.put("slaved", url.Values{"Slaved": {boolParam(s)}}) }
func (d *Dome) AbortSlew() error       { return d.put("abortslew", nil) }
func (d *Dome) CloseShutter() error    { return d.put("closeshutter", nil) }
func (d *Dome) FindHome() error        { return d.put("findhome", nil) }
func (d *Dome) OpenShutter() error     { return d.put("openshutter", nil) }
func (d *Dome) Park() error            { return d.put("park", nil) }
func (d *Dome) SetPark() error         { return d.put("setpark", nil) }
func (d *Dome) SlewToAltitude(altitude float64) error {
	return d.put("slewtoaltitude", url.Values{"Altitude": {floatParam(altitude)}})
}
func (d *Dome) SlewToAzimuth(azimuth float64) error {
	return d.put("slewtoazimuth", url.Values{"Azimuth": {floatParam(azimuth)}})
}
func (d *Dome) SyncToAzimuth(azimuth float64) error {
	return d.put("synctoazimuth", url.Values{"Azimuth": {floatParam(azimuth)}})
}
