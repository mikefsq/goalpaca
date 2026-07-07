package alpaca

// Telescope enums (mirror the ASCOM definitions).

// AlignmentMode mirrors the ASCOM AlignmentModes enum (mount geometry).
type AlignmentMode int

const (
	AlignAltAz       AlignmentMode = 0
	AlignPolar       AlignmentMode = 1
	AlignGermanPolar AlignmentMode = 2
)

// EquatorialCoordinateType for the EquatorialSystem property.
type EquatorialCoordinateType int

const (
	EquOther       EquatorialCoordinateType = 0
	EquTopocentric EquatorialCoordinateType = 1
	EquJ2000       EquatorialCoordinateType = 2
	EquJ2050       EquatorialCoordinateType = 3
	EquB1950       EquatorialCoordinateType = 4
)

// PierSide for SideOfPier / DestinationSideOfPier.
type PierSide int

const (
	PierUnknown PierSide = -1
	PierEast    PierSide = 0
	PierWest    PierSide = 1
)

// DriveRate for TrackingRate / TrackingRates.
type DriveRate int

const (
	DriveSidereal DriveRate = 0
	DriveLunar    DriveRate = 1
	DriveSolar    DriveRate = 2
	DriveKing     DriveRate = 3
)

// TelescopeAxis for MoveAxis / AxisRates / CanMoveAxis.
type TelescopeAxis int

const (
	AxisPrimary   TelescopeAxis = 0
	AxisSecondary TelescopeAxis = 1
	AxisTertiary  TelescopeAxis = 2
)

// AxisRate is one allowed rate range for MoveAxis (degrees/second).
type AxisRate struct {
	Minimum float64 `json:"Minimum"`
	Maximum float64 `json:"Maximum"`
}
