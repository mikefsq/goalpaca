package server

// The wire vocabulary of the protocol — device-type names, per-type enums,
// the error model, and the image transport types/codec — lives in the leaf
// package github.com/mikefsq/goalpaca/alpaca, shared with the client. It is
// re-exported here under the original names (identity-preserving type aliases
// and the very same sentinel values), so code written against this package is
// unaffected by the split.

import "github.com/mikefsq/goalpaca/alpaca"

// Device types and DeviceState entries.

type (
	DeviceType = alpaca.DeviceType
	StateValue = alpaca.StateValue
)

const (
	CameraType              = alpaca.CameraType
	CoverCalibratorType     = alpaca.CoverCalibratorType
	DomeType                = alpaca.DomeType
	FilterWheelType         = alpaca.FilterWheelType
	FocuserType             = alpaca.FocuserType
	ObservingConditionsType = alpaca.ObservingConditionsType
	RotatorType             = alpaca.RotatorType
	SafetyMonitorType       = alpaca.SafetyMonitorType
	SwitchType              = alpaca.SwitchType
	TelescopeType           = alpaca.TelescopeType
)

// Error model.

type AlpacaError = alpaca.AlpacaError

const (
	ErrNumNotImplemented        = alpaca.ErrNumNotImplemented
	ErrNumInvalidValue          = alpaca.ErrNumInvalidValue
	ErrNumValueNotSet           = alpaca.ErrNumValueNotSet
	ErrNumNotConnected          = alpaca.ErrNumNotConnected
	ErrNumParked                = alpaca.ErrNumParked
	ErrNumSlaved                = alpaca.ErrNumSlaved
	ErrNumSettingsProviderError = alpaca.ErrNumSettingsProviderError
	ErrNumInvalidOperation      = alpaca.ErrNumInvalidOperation
	ErrNumActionNotImplemented  = alpaca.ErrNumActionNotImplemented
	ErrNumOperationCancelled    = alpaca.ErrNumOperationCancelled
	ErrNumUnspecified           = alpaca.ErrNumUnspecified
	ErrNumInvalidWhileParked    = alpaca.ErrNumInvalidWhileParked
	ErrNumInvalidWhileSlaved    = alpaca.ErrNumInvalidWhileSlaved
	ErrNumDriverBase            = alpaca.ErrNumDriverBase
	ErrNumDriverMax             = alpaca.ErrNumDriverMax
)

// The sentinels are the SAME values as the leaf package's, so errors.Is
// matches across both.
var (
	ErrNotImplemented       = alpaca.ErrNotImplemented
	ErrInvalidValue         = alpaca.ErrInvalidValue
	ErrValueNotSet          = alpaca.ErrValueNotSet
	ErrNotConnected         = alpaca.ErrNotConnected
	ErrParked               = alpaca.ErrParked
	ErrSlaved               = alpaca.ErrSlaved
	ErrInvalidOperation     = alpaca.ErrInvalidOperation
	ErrActionNotImplemented = alpaca.ErrActionNotImplemented
	ErrOperationCancelled   = alpaca.ErrOperationCancelled
	ErrInvalidWhileParked   = alpaca.ErrInvalidWhileParked
	ErrInvalidWhileSlaved   = alpaca.ErrInvalidWhileSlaved
)

// NewError builds an AlpacaError with a driver-defined number (clamped into
// the reserved 0x500–0xFFF range). Alias of [alpaca.NewError].
func NewError(number int, message string) *AlpacaError { return alpaca.NewError(number, message) }

// ErrorNumberFor maps a Go error to an ASCOM (number, message) pair for the
// in-band envelope. Alias of [alpaca.ErrorNumberFor].
func ErrorNumberFor(err error) (int, string) { return alpaca.ErrorNumberFor(err) }

// Camera / image transport.

type (
	CameraState      = alpaca.CameraState
	SensorType       = alpaca.SensorType
	ImageElementType = alpaca.ImageElementType
	GuideDirection   = alpaca.GuideDirection
	ImageFrame       = alpaca.ImageFrame
)

const (
	CameraIdle     = alpaca.CameraIdle
	CameraWaiting  = alpaca.CameraWaiting
	CameraExposing = alpaca.CameraExposing
	CameraReading  = alpaca.CameraReading
	CameraDownload = alpaca.CameraDownload
	CameraError    = alpaca.CameraError

	SensorMonochrome = alpaca.SensorMonochrome
	SensorColor      = alpaca.SensorColor
	SensorRGGB       = alpaca.SensorRGGB
	SensorCMYG       = alpaca.SensorCMYG
	SensorCMYG2      = alpaca.SensorCMYG2
	SensorLRGB       = alpaca.SensorLRGB

	ImgUnknown = alpaca.ImgUnknown
	ImgInt16   = alpaca.ImgInt16
	ImgInt32   = alpaca.ImgInt32
	ImgDouble  = alpaca.ImgDouble
	ImgSingle  = alpaca.ImgSingle
	ImgUInt64  = alpaca.ImgUInt64
	ImgByte    = alpaca.ImgByte
	ImgInt64   = alpaca.ImgInt64
	ImgUInt16  = alpaca.ImgUInt16
	ImgUInt32  = alpaca.ImgUInt32

	GuideNorth = alpaca.GuideNorth
	GuideSouth = alpaca.GuideSouth
	GuideEast  = alpaca.GuideEast
	GuideWest  = alpaca.GuideWest
)

// Telescope enums.

type (
	AlignmentMode            = alpaca.AlignmentMode
	EquatorialCoordinateType = alpaca.EquatorialCoordinateType
	PierSide                 = alpaca.PierSide
	DriveRate                = alpaca.DriveRate
	TelescopeAxis            = alpaca.TelescopeAxis
	AxisRate                 = alpaca.AxisRate
)

const (
	AlignAltAz       = alpaca.AlignAltAz
	AlignPolar       = alpaca.AlignPolar
	AlignGermanPolar = alpaca.AlignGermanPolar

	EquOther       = alpaca.EquOther
	EquTopocentric = alpaca.EquTopocentric
	EquJ2000       = alpaca.EquJ2000
	EquJ2050       = alpaca.EquJ2050
	EquB1950       = alpaca.EquB1950

	PierUnknown = alpaca.PierUnknown
	PierEast    = alpaca.PierEast
	PierWest    = alpaca.PierWest

	DriveSidereal = alpaca.DriveSidereal
	DriveLunar    = alpaca.DriveLunar
	DriveSolar    = alpaca.DriveSolar
	DriveKing     = alpaca.DriveKing

	AxisPrimary   = alpaca.AxisPrimary
	AxisSecondary = alpaca.AxisSecondary
	AxisTertiary  = alpaca.AxisTertiary
)

// Dome / CoverCalibrator enums.

type (
	ShutterState     = alpaca.ShutterState
	CoverStatus      = alpaca.CoverStatus
	CalibratorStatus = alpaca.CalibratorStatus
)

const (
	ShutterOpen    = alpaca.ShutterOpen
	ShutterClosed  = alpaca.ShutterClosed
	ShutterOpening = alpaca.ShutterOpening
	ShutterClosing = alpaca.ShutterClosing
	ShutterErr     = alpaca.ShutterErr

	CoverNotPresent = alpaca.CoverNotPresent
	CoverClosed     = alpaca.CoverClosed
	CoverMoving     = alpaca.CoverMoving
	CoverOpen       = alpaca.CoverOpen
	CoverUnknown    = alpaca.CoverUnknown
	CoverError      = alpaca.CoverError

	CalibratorNotPresent = alpaca.CalibratorNotPresent
	CalibratorOff        = alpaca.CalibratorOff
	CalibratorNotReady   = alpaca.CalibratorNotReady
	CalibratorReady      = alpaca.CalibratorReady
	CalibratorUnknown    = alpaca.CalibratorUnknown
	CalibratorError      = alpaca.CalibratorError
)

// ImageBytes codec.

// ImageBytesMIME is the Accept/Content-Type value for the ASCOM ImageBytes
// binary image transport. Alias of [alpaca.ImageBytesMIME].
const ImageBytesMIME = alpaca.ImageBytesMIME

// EncodeImageBytes serializes a successful image as ASCOM ImageBytes.
// Alias of [alpaca.EncodeImageBytes].
func EncodeImageBytes(frame ImageFrame, clientTxID, serverTxID uint32) []byte {
	return alpaca.EncodeImageBytes(frame, clientTxID, serverTxID)
}

// EncodeImageBytesError serializes an error in the ImageBytes envelope.
// Alias of [alpaca.EncodeImageBytesError].
func EncodeImageBytesError(errNum int, msg string, clientTxID, serverTxID uint32) []byte {
	return alpaca.EncodeImageBytesError(errNum, msg, clientTxID, serverTxID)
}

// DecodeImageBytes parses an ASCOM ImageBytes response.
// Alias of [alpaca.DecodeImageBytes].
func DecodeImageBytes(data []byte) (ImageFrame, error) { return alpaca.DecodeImageBytes(data) }

// Internal bridges so the serving code and tests keep their original names.
const (
	imageBytesMetadataLen     = alpaca.ImageBytesHeaderLen
	imageBytesMetadataVersion = alpaca.ImageBytesVersion
)

func elemBytes(t ImageElementType) int { return alpaca.ElementSize(t) }

func encodeImageBytesInto(dst []byte, frame ImageFrame, clientTxID, serverTxID uint32) {
	alpaca.EncodeImageBytesInto(dst, frame, clientTxID, serverTxID)
}
