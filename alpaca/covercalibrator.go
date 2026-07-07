package alpaca

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
