package alpaca

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
