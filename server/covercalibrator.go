package server

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

// coverCalibratorPut dispatches CoverCalibrator PUT members. A device that
// reports its calibrator or cover as NotPresent must return NotImplemented
// from the corresponding methods (ICoverCalibratorV1), and CalibratorOn's
// brightness is bounded by MaxBrightness.
func coverCalibratorPut(member string, cc CoverCalibrator, p params) (bool, error) {
	switch member {
	case "calibratoroff":
		if cc.CalibratorState() == CalibratorNotPresent {
			return true, notImplErr("CalibratorOff")
		}
		return true, cc.CalibratorOff()
	case "calibratoron":
		n, err := p.reqInt("Brightness")
		if err != nil {
			return true, err
		}
		if cc.CalibratorState() == CalibratorNotPresent {
			return true, notImplErr("CalibratorOn")
		}
		if max := cc.MaxBrightness(); n < 0 || n > max {
			return true, invalidValuef("Brightness %d is outside the valid range 0 to %d", n, max)
		}
		return true, cc.CalibratorOn(n)
	case "closecover":
		if cc.CoverState() == CoverNotPresent {
			return true, notImplErr("CloseCover")
		}
		return true, cc.CloseCover()
	case "haltcover":
		if cc.CoverState() == CoverNotPresent {
			return true, notImplErr("HaltCover")
		}
		return true, cc.HaltCover()
	case "opencover":
		if cc.CoverState() == CoverNotPresent {
			return true, notImplErr("OpenCover")
		}
		return true, cc.OpenCover()
	}
	return false, nil
}
