package alpacadev

// BaseCamera provides not-implemented / zero-value defaults for every Camera
// member, so a driver embeds it and overrides only what the hardware supports.
// It embeds BaseDevice for the common identity/connection members.
//
// Defaults are conservative: all Can* flags are false, settable members return
// ErrNotImplemented, and (value, error) getters return ErrNotImplemented. A
// driver MUST override at least the geometry getters and the exposure members.
type BaseCamera struct {
	BaseDevice
}

// Geometry / description
func (c *BaseCamera) CameraXSize() int           { return 0 }
func (c *BaseCamera) CameraYSize() int           { return 0 }
func (c *BaseCamera) PixelSizeX() float64        { return 0 }
func (c *BaseCamera) PixelSizeY() float64        { return 0 }
func (c *BaseCamera) MaxADU() int                { return 0 }
func (c *BaseCamera) ElectronsPerADU() float64   { return 0 }
func (c *BaseCamera) FullWellCapacity() float64  { return 0 }
func (c *BaseCamera) SensorName() string         { return "" }
func (c *BaseCamera) SensorType() SensorType     { return SensorMonochrome }
func (c *BaseCamera) BayerOffsetX() (int, error) { return 0, ErrNotImplemented }
func (c *BaseCamera) BayerOffsetY() (int, error) { return 0, ErrNotImplemented }

// Binning
func (c *BaseCamera) BinX() int              { return 1 }
func (c *BaseCamera) BinY() int              { return 1 }
func (c *BaseCamera) SetBinX(int) error      { return ErrNotImplemented }
func (c *BaseCamera) SetBinY(int) error      { return ErrNotImplemented }
func (c *BaseCamera) MaxBinX() int           { return 1 }
func (c *BaseCamera) MaxBinY() int           { return 1 }
func (c *BaseCamera) CanAsymmetricBin() bool { return false }

// Subframe
func (c *BaseCamera) StartX() int         { return 0 }
func (c *BaseCamera) StartY() int         { return 0 }
func (c *BaseCamera) SetStartX(int) error { return ErrNotImplemented }
func (c *BaseCamera) SetStartY(int) error { return ErrNotImplemented }
func (c *BaseCamera) NumX() int           { return 0 }
func (c *BaseCamera) NumY() int           { return 0 }
func (c *BaseCamera) SetNumX(int) error   { return ErrNotImplemented }
func (c *BaseCamera) SetNumY(int) error   { return ErrNotImplemented }

// Exposure
func (c *BaseCamera) StartExposure(float64, bool) error { return ErrNotImplemented }
func (c *BaseCamera) StopExposure() error               { return ErrNotImplemented }
func (c *BaseCamera) AbortExposure() error              { return ErrNotImplemented }
func (c *BaseCamera) CanStopExposure() bool             { return false }
func (c *BaseCamera) CanAbortExposure() bool            { return false }
func (c *BaseCamera) ImageReady() bool                  { return false }
func (c *BaseCamera) CameraState() CameraState          { return CameraIdle }
func (c *BaseCamera) PercentCompleted() int             { return 0 }
func (c *BaseCamera) ExposureMin() float64              { return 0 }
func (c *BaseCamera) ExposureMax() float64              { return 0 }
func (c *BaseCamera) ExposureResolution() float64       { return 0 }
func (c *BaseCamera) LastExposureDuration() (float64, error) {
	return 0, ErrValueNotSet
}
func (c *BaseCamera) LastExposureStartTime() (string, error) {
	return "", ErrValueNotSet
}
func (c *BaseCamera) HasShutter() bool { return false }

// Sub-exposure interval (ICameraV3+); optional, off by default.
func (c *BaseCamera) SubExposureDuration() (float64, error) { return 0, ErrNotImplemented }
func (c *BaseCamera) SetSubExposureDuration(float64) error  { return ErrNotImplemented }

// Image transport
func (c *BaseCamera) ImageFrame() (ImageFrame, error) {
	return ImageFrame{}, ErrNotImplemented
}

// Gain / Offset
func (c *BaseCamera) Gain() int                  { return 0 }
func (c *BaseCamera) SetGain(int) error          { return ErrNotImplemented }
func (c *BaseCamera) GainMin() int               { return 0 }
func (c *BaseCamera) GainMax() int               { return 0 }
func (c *BaseCamera) Gains() ([]string, error)   { return nil, ErrNotImplemented }
func (c *BaseCamera) Offset() int                { return 0 }
func (c *BaseCamera) SetOffset(int) error        { return ErrNotImplemented }
func (c *BaseCamera) OffsetMin() int             { return 0 }
func (c *BaseCamera) OffsetMax() int             { return 0 }
func (c *BaseCamera) Offsets() ([]string, error) { return nil, ErrNotImplemented }

// Readout modes
func (c *BaseCamera) ReadoutMode() int           { return 0 }
func (c *BaseCamera) SetReadoutMode(int) error   { return ErrNotImplemented }
func (c *BaseCamera) ReadoutModes() []string     { return []string{"Default"} }
func (c *BaseCamera) FastReadout() (bool, error) { return false, ErrNotImplemented }
func (c *BaseCamera) SetFastReadout(bool) error  { return ErrNotImplemented }
func (c *BaseCamera) CanFastReadout() bool       { return false }

// Cooling
func (c *BaseCamera) CCDTemperature() (float64, error)      { return 0, ErrNotImplemented }
func (c *BaseCamera) HeatSinkTemperature() (float64, error) { return 0, ErrNotImplemented }
func (c *BaseCamera) CoolerOn() bool                        { return false }
func (c *BaseCamera) SetCoolerOn(bool) error                { return ErrNotImplemented }
func (c *BaseCamera) CoolerPower() (float64, error)         { return 0, ErrNotImplemented }
func (c *BaseCamera) CanGetCoolerPower() bool               { return false }
func (c *BaseCamera) SetCCDTemperature() (float64, error)   { return 0, ErrNotImplemented }
func (c *BaseCamera) SetSetCCDTemperature(float64) error    { return ErrNotImplemented }
func (c *BaseCamera) CanSetCCDTemperature() bool            { return false }

// Guiding
func (c *BaseCamera) CanPulseGuide() bool                  { return false }
func (c *BaseCamera) IsPulseGuiding() bool                 { return false }
func (c *BaseCamera) PulseGuide(GuideDirection, int) error { return ErrNotImplemented }
