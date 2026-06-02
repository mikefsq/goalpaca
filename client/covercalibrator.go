package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// CoverCalibrator is a client for an ASCOM CoverCalibrator device.
type CoverCalibrator struct{ Device }

// NewCoverCalibrator returns a client for the cover/calibrator at the given
// Alpaca address and device number.
func NewCoverCalibrator(address string, deviceNumber int, opts ...Option) *CoverCalibrator {
	return &CoverCalibrator{newDevice(address, alpaca.CoverCalibratorType, deviceNumber, opts...)}
}

func (c *CoverCalibrator) Brightness() (int, error)          { return c.getInt("brightness") }
func (c *CoverCalibrator) MaxBrightness() (int, error)       { return c.getInt("maxbrightness") }
func (c *CoverCalibrator) CalibratorChanging() (bool, error) { return c.getBool("calibratorchanging") }
func (c *CoverCalibrator) CoverMoving() (bool, error)        { return c.getBool("covermoving") }

func (c *CoverCalibrator) CalibratorState() (alpaca.CalibratorStatus, error) {
	v, err := c.getInt("calibratorstate")
	return alpaca.CalibratorStatus(v), err
}
func (c *CoverCalibrator) CoverState() (alpaca.CoverStatus, error) {
	v, err := c.getInt("coverstate")
	return alpaca.CoverStatus(v), err
}

func (c *CoverCalibrator) CalibratorOff() error { return c.put("calibratoroff", nil) }
func (c *CoverCalibrator) CalibratorOn(brightness int) error {
	return c.put("calibratoron", url.Values{"Brightness": {intParam(brightness)}})
}
func (c *CoverCalibrator) CloseCover() error { return c.put("closecover", nil) }
func (c *CoverCalibrator) HaltCover() error  { return c.put("haltcover", nil) }
func (c *CoverCalibrator) OpenCover() error  { return c.put("opencover", nil) }
