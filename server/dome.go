package alpacadev

// ShutterState mirrors the ASCOM ShutterState enum.
type ShutterState int

const (
	ShutterOpen    ShutterState = 0
	ShutterClosed  ShutterState = 1
	ShutterOpening ShutterState = 2
	ShutterClosing ShutterState = 3
	ShutterErr     ShutterState = 4
)

// Dome is the ASCOM Dome interface (IDomeV2/V3). The shutter and slew methods
// are initiators; Slewing / ShutterStatus / AtHome / AtPark are completion
// properties. Altitude/Azimuth return an error when not supported.
type Dome interface {
	Device

	Altitude() (float64, error)
	AtHome() bool
	AtPark() bool
	Azimuth() (float64, error)
	CanFindHome() bool
	CanPark() bool
	CanSetAltitude() bool
	CanSetAzimuth() bool
	CanSetPark() bool
	CanSetShutter() bool
	CanSlave() bool
	CanSyncAzimuth() bool
	ShutterStatus() (ShutterState, error)
	Slaved() bool
	SetSlaved(bool) error
	Slewing() bool

	AbortSlew() error
	CloseShutter() error // initiator
	FindHome() error     // initiator
	OpenShutter() error  // initiator
	Park() error         // initiator
	SetPark() error
	SlewToAltitude(altitude float64) error // initiator
	SlewToAzimuth(azimuth float64) error   // initiator
	SyncToAzimuth(azimuth float64) error
}

// BaseDome provides not-implemented / incapable defaults for Dome.
type BaseDome struct {
	BaseDevice
}

func (b *BaseDome) Altitude() (float64, error)           { return 0, ErrNotImplemented }
func (b *BaseDome) AtHome() bool                         { return false }
func (b *BaseDome) AtPark() bool                         { return false }
func (b *BaseDome) Azimuth() (float64, error)            { return 0, ErrNotImplemented }
func (b *BaseDome) CanFindHome() bool                    { return false }
func (b *BaseDome) CanPark() bool                        { return false }
func (b *BaseDome) CanSetAltitude() bool                 { return false }
func (b *BaseDome) CanSetAzimuth() bool                  { return false }
func (b *BaseDome) CanSetPark() bool                     { return false }
func (b *BaseDome) CanSetShutter() bool                  { return false }
func (b *BaseDome) CanSlave() bool                       { return false }
func (b *BaseDome) CanSyncAzimuth() bool                 { return false }
func (b *BaseDome) ShutterStatus() (ShutterState, error) { return ShutterClosed, ErrNotImplemented }
func (b *BaseDome) Slaved() bool                         { return false }
func (b *BaseDome) SetSlaved(bool) error                 { return ErrNotImplemented }
func (b *BaseDome) Slewing() bool                        { return false }
func (b *BaseDome) AbortSlew() error                     { return ErrNotImplemented }
func (b *BaseDome) CloseShutter() error                  { return ErrNotImplemented }
func (b *BaseDome) FindHome() error                      { return ErrNotImplemented }
func (b *BaseDome) OpenShutter() error                   { return ErrNotImplemented }
func (b *BaseDome) Park() error                          { return ErrNotImplemented }
func (b *BaseDome) SetPark() error                       { return ErrNotImplemented }
func (b *BaseDome) SlewToAltitude(float64) error         { return ErrNotImplemented }
func (b *BaseDome) SlewToAzimuth(float64) error          { return ErrNotImplemented }
func (b *BaseDome) SyncToAzimuth(float64) error          { return ErrNotImplemented }

func domeGet(member string, d Dome, _ params) (any, bool, error) {
	switch member {
	case "altitude":
		v, err := d.Altitude()
		return v, true, err
	case "athome":
		return d.AtHome(), true, nil
	case "atpark":
		return d.AtPark(), true, nil
	case "azimuth":
		v, err := d.Azimuth()
		return v, true, err
	case "canfindhome":
		return d.CanFindHome(), true, nil
	case "canpark":
		return d.CanPark(), true, nil
	case "cansetaltitude":
		return d.CanSetAltitude(), true, nil
	case "cansetazimuth":
		return d.CanSetAzimuth(), true, nil
	case "cansetpark":
		return d.CanSetPark(), true, nil
	case "cansetshutter":
		return d.CanSetShutter(), true, nil
	case "canslave":
		return d.CanSlave(), true, nil
	case "cansyncazimuth":
		return d.CanSyncAzimuth(), true, nil
	case "shutterstatus":
		v, err := d.ShutterStatus()
		return int(v), true, err
	case "slaved":
		return d.Slaved(), true, nil
	case "slewing":
		return d.Slewing(), true, nil
	}
	return nil, false, nil
}

func domePut(member string, d Dome, p params) (bool, error) {
	switch member {
	case "slaved":
		b, err := p.reqBool("Slaved")
		if err != nil {
			return true, err
		}
		return true, d.SetSlaved(b)
	case "abortslew":
		return true, d.AbortSlew()
	case "closeshutter":
		return true, d.CloseShutter()
	case "findhome":
		return true, d.FindHome()
	case "openshutter":
		return true, d.OpenShutter()
	case "park":
		return true, d.Park()
	case "setpark":
		return true, d.SetPark()
	case "slewtoaltitude":
		f, err := p.reqFloat("Altitude")
		if err != nil {
			return true, err
		}
		return true, d.SlewToAltitude(f)
	case "slewtoazimuth":
		f, err := p.reqFloat("Azimuth")
		if err != nil {
			return true, err
		}
		return true, d.SlewToAzimuth(f)
	case "synctoazimuth":
		f, err := p.reqFloat("Azimuth")
		if err != nil {
			return true, err
		}
		return true, d.SyncToAzimuth(f)
	}
	return false, nil
}
