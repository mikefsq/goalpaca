package alpacadev

// CoverStatus mirrors the ASCOM CoverStatus enum.
type CoverStatus int

const (
	CoverNotPresent CoverStatus = 0
	CoverClosed     CoverStatus = 1
	CoverMoving     CoverStatus = 2
	CoverOpen       CoverStatus = 3
	CoverUnknown    CoverStatus = 4
	CoverError      CoverStatus = 5
)

// CalibratorStatus mirrors the ASCOM CalibratorStatus enum.
type CalibratorStatus int

const (
	CalibratorNotPresent CalibratorStatus = 0
	CalibratorOff        CalibratorStatus = 1
	CalibratorNotReady   CalibratorStatus = 2
	CalibratorReady      CalibratorStatus = 3
	CalibratorUnknown    CalibratorStatus = 4
	CalibratorError      CalibratorStatus = 5
)

// CoverCalibrator is the ASCOM CoverCalibrator interface (ICoverCalibratorV1/V2).
// CalibratorOn / OpenCover / CloseCover are initiators; CalibratorChanging /
// CoverMoving (V2) are completion properties.
type CoverCalibrator interface {
	Device

	Brightness() int
	CalibratorState() CalibratorStatus
	CoverState() CoverStatus
	MaxBrightness() int
	CalibratorChanging() bool // V2
	CoverMoving() bool        // V2

	CalibratorOff() error
	CalibratorOn(brightness int) error // initiator
	CloseCover() error                 // initiator
	HaltCover() error
	OpenCover() error // initiator
}

// BaseCoverCalibrator provides not-implemented / not-present defaults.
type BaseCoverCalibrator struct {
	BaseDevice
}

func (b *BaseCoverCalibrator) Brightness() int                   { return 0 }
func (b *BaseCoverCalibrator) CalibratorState() CalibratorStatus { return CalibratorNotPresent }
func (b *BaseCoverCalibrator) CoverState() CoverStatus           { return CoverNotPresent }
func (b *BaseCoverCalibrator) MaxBrightness() int                { return 0 }
func (b *BaseCoverCalibrator) CalibratorChanging() bool          { return false }
func (b *BaseCoverCalibrator) CoverMoving() bool                 { return false }
func (b *BaseCoverCalibrator) CalibratorOff() error              { return ErrNotImplemented }
func (b *BaseCoverCalibrator) CalibratorOn(int) error            { return ErrNotImplemented }
func (b *BaseCoverCalibrator) CloseCover() error                 { return ErrNotImplemented }
func (b *BaseCoverCalibrator) HaltCover() error                  { return ErrNotImplemented }
func (b *BaseCoverCalibrator) OpenCover() error                  { return ErrNotImplemented }

func coverCalibratorGet(member string, cc CoverCalibrator, _ params) (any, bool, error) {
	switch member {
	case "brightness":
		return cc.Brightness(), true, nil
	case "calibratorstate":
		return int(cc.CalibratorState()), true, nil
	case "coverstate":
		return int(cc.CoverState()), true, nil
	case "maxbrightness":
		return cc.MaxBrightness(), true, nil
	case "calibratorchanging":
		return cc.CalibratorChanging(), true, nil
	case "covermoving":
		return cc.CoverMoving(), true, nil
	}
	return nil, false, nil
}

func coverCalibratorPut(member string, cc CoverCalibrator, p params) (bool, error) {
	switch member {
	case "calibratoroff":
		return true, cc.CalibratorOff()
	case "calibratoron":
		n, err := p.reqInt("Brightness")
		if err != nil {
			return true, err
		}
		return true, cc.CalibratorOn(n)
	case "closecover":
		return true, cc.CloseCover()
	case "haltcover":
		return true, cc.HaltCover()
	case "opencover":
		return true, cc.OpenCover()
	}
	return false, nil
}
