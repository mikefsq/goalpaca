package alpacadev

// CameraState mirrors the ASCOM CameraStates enum.
type CameraState int

const (
	CameraIdle     CameraState = 0
	CameraWaiting  CameraState = 1
	CameraExposing CameraState = 2
	CameraReading  CameraState = 3
	CameraDownload CameraState = 4
	CameraError    CameraState = 5
)

// SensorType mirrors the ASCOM SensorType enum.
type SensorType int

const (
	SensorMonochrome SensorType = 0
	SensorColor      SensorType = 1 // single-shot color returning RGB (rank 3)
	SensorRGGB       SensorType = 2 // Bayer-mosaiced, client debayers
	SensorCMYG       SensorType = 3
	SensorCMYG2      SensorType = 4
	SensorLRGB       SensorType = 5
)

// ImageElementType mirrors the ASCOM ImageArrayElementTypes enum. Used for both
// the logical element type and the on-the-wire transmission type in ImageBytes.
type ImageElementType int32

const (
	ImgUnknown ImageElementType = 0
	ImgInt16   ImageElementType = 1
	ImgInt32   ImageElementType = 2
	ImgDouble  ImageElementType = 3
	ImgSingle  ImageElementType = 4
	ImgUInt64  ImageElementType = 5
	ImgByte    ImageElementType = 6
	ImgInt64   ImageElementType = 7
	ImgUInt16  ImageElementType = 8
	ImgUInt32  ImageElementType = 9
)

// GuideDirection mirrors the ASCOM GuideDirections enum (used by PulseGuide).
type GuideDirection int

const (
	GuideNorth GuideDirection = 0
	GuideSouth GuideDirection = 1
	GuideEast  GuideDirection = 2
	GuideWest  GuideDirection = 3
)

// ImageFrame is one image ready for transport. The driver fills it (typically
// from its SDK's raw buffer); the library encodes it as ImageBytes.
//
// Pixels are raw little-endian in TransmissionElementType order, in natural
// sensor ROW-MAJOR order (X fastest) — the encoder transposes a Rank-2 frame to
// ASCOM's column-major ImageBytes wire order, so drivers just hand over the SDK
// buffer as-is. For a Bayer/mono sensor: Rank 2, Planes 0. For RGB color: Rank 3,
// Planes 3, pixels laid out per the ImageBytes plane convention (see imagebytes.go).
type ImageFrame struct {
	Rank                    int              // 2 (mono/Bayer) or 3 (color planes)
	Width                   int              // dimension 1
	Height                  int              // dimension 2
	Planes                  int              // dimension 3, when Rank == 3
	ElementType             ImageElementType // what the client should present (e.g. Int32)
	TransmissionElementType ImageElementType // what is on the wire (e.g. UInt16); 0 => same as ElementType
	Pixels                  []byte           // raw little-endian, in TransmissionElementType
}

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
	LastExposureStartTime() (string, error) // FITS-format UTC, or error if none
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
	SetSetCCDTemperature(float64) error  // set the setpoint
	CanSetCCDTemperature() bool

	// Guiding
	CanPulseGuide() bool
	IsPulseGuiding() bool
	PulseGuide(direction GuideDirection, duration int) error // initiator
}
