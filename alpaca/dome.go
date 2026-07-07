package alpaca

// ShutterState mirrors the ASCOM ShutterState enum.
type ShutterState int

const (
	ShutterOpen    ShutterState = 0
	ShutterClosed  ShutterState = 1
	ShutterOpening ShutterState = 2
	ShutterClosing ShutterState = 3
	ShutterErr     ShutterState = 4
)
