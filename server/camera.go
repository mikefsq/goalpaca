package server

// Camera is the typed interface a camera driver implements (in addition to the
// common Device interface). Embed BaseCamera to get sane defaults for the
// members your hardware does not support, and override the rest.
//
// Member names follow the ASCOM Master Interface Definitions. Async members:
// StartExposure / PulseGuide are initiators; ImageReady / IsPulseGuiding /
// CameraState are the completion properties clients poll.
type Camera interface {
	Device

	// Sensor geometry & description
	CameraXSize() int
	CameraYSize() int
	PixelSizeX() float64
	PixelSizeY() float64
	MaxADU() int
	ElectronsPerADU() float64
	FullWellCapacity() float64
	SensorName() string
	SensorType() SensorType
	BayerOffsetX() (int, error) // NotImplemented for monochrome sensors
	BayerOffsetY() (int, error)

	// Binning
	BinX() int
	BinY() int
	SetBinX(int) error
	SetBinY(int) error
	MaxBinX() int
	MaxBinY() int
	CanAsymmetricBin() bool

	// Subframe (ROI), in binned pixels
	StartX() int
	StartY() int
	SetStartX(int) error
	SetStartY(int) error
	NumX() int
	NumY() int
	SetNumX(int) error
	SetNumY(int) error

	// Exposure
	StartExposure(duration float64, light bool) error // initiator
	StopExposure() error
	AbortExposure() error
	CanStopExposure() bool
	CanAbortExposure() bool
	ImageReady() bool // completion
	CameraState() CameraState
	PercentCompleted() int
	ExposureMin() float64
	ExposureMax() float64
	ExposureResolution() float64
	LastExposureDuration() (float64, error)
	LastExposureStartTime() (string, error) // "yyyy-mm-ddThh:mm:ss" in UTC (not local time), or error if none
	HasShutter() bool
	SubExposureDuration() (float64, error) // ICameraV3+, seconds
	SetSubExposureDuration(float64) error

	// Image transport
	ImageFrame() (ImageFrame, error)

	// Gain / Offset (value-mode or list-mode per ASCOM)
	Gain() int
	SetGain(int) error
	GainMin() int
	GainMax() int
	Gains() ([]string, error) // NotImplemented in value (Gain min/max) mode
	Offset() int
	SetOffset(int) error
	OffsetMin() int
	OffsetMax() int
	Offsets() ([]string, error) // NotImplemented in value (Offset min/max) mode
	// CanGain / CanOffset are capability flags consulted by the dispatch
	// layer, not Alpaca members: ASCOM expresses an absent gain by the Gain
	// property itself throwing NotImplemented, which the int-typed getters
	// here cannot do. When false, the whole family (value, min, max, list,
	// setter) answers NotImplemented without the driver being called. They
	// exist for devices whose gain support is only known at runtime (e.g. a
	// protocol bridge that could not resolve a gain source); BaseCamera
	// returns true, so drivers that implement gain by overriding only the
	// getters are unaffected.
	CanGain() bool
	CanOffset() bool

	// Readout modes
	ReadoutMode() int
	SetReadoutMode(int) error
	ReadoutModes() []string
	FastReadout() (bool, error) // NotImplemented when CanFastReadout is false
	SetFastReadout(bool) error
	CanFastReadout() bool

	// Cooling
	CCDTemperature() (float64, error)
	HeatSinkTemperature() (float64, error)
	CoolerOn() bool
	SetCoolerOn(bool) error
	CoolerPower() (float64, error)
	CanGetCoolerPower() bool
	SetCCDTemperature() (float64, error) // the setpoint
	// SetSetCCDTemperature sets the cooler set point in °C. The driver must
	// return ErrInvalidValue outside its achievable range; ConformU requires
	// the upper limit to be below 100 °C and the lower at or above −273.15 °C.
	SetSetCCDTemperature(float64) error
	CanSetCCDTemperature() bool

	// Guiding
	CanPulseGuide() bool
	IsPulseGuiding() bool
	PulseGuide(direction GuideDirection, duration int) error // initiator
}
