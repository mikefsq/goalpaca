// Package alpacadev is a library for exposing a single hardware device as a
// standalone ASCOM Alpaca server (HTTP/JSON REST + UDP discovery).
//
// A device author implements a typed per-type interface (Camera, Focuser, ...)
// plus the Hardware lifecycle interface; this library handles the wire
// protocol, discovery participation, async semantics, image transport, and
// liveness. One process owns the hardware for its entire life — the Alpaca
// Connected flag is a logical per-client session marker, never a hardware
// open/close (see BaseDevice and the Hardware interface).
package alpacadev

import "context"

// DeviceType is the ASCOM device-type path segment (lowercased on the wire,
// e.g. "camera"). The set is fixed by ASCOM; anything outside it is modeled as
// Switch or Action.
type DeviceType string

const (
	CameraType              DeviceType = "camera"
	CoverCalibratorType     DeviceType = "covercalibrator"
	DomeType                DeviceType = "dome"
	FilterWheelType         DeviceType = "filterwheel"
	FocuserType             DeviceType = "focuser"
	ObservingConditionsType DeviceType = "observingconditions"
	RotatorType             DeviceType = "rotator"
	SafetyMonitorType       DeviceType = "safetymonitor"
	SwitchType              DeviceType = "switch"
	TelescopeType           DeviceType = "telescope"
)

// StateValue is one entry in a DeviceState batch snapshot.
type StateValue struct {
	Name  string
	Value any
}

// Device is the common interface every Alpaca device implements. Per-type
// interfaces (Camera, Focuser, ...) embed this and add their ASCOM members.
//
// Most identity/connection bookkeeping is provided by embedding BaseDevice;
// authors typically only override the fields they care about.
type Device interface {
	// Identity
	UniqueID() string // stable GUID; keys registration and client identity
	Name() string
	Description() string
	DriverInfo() string
	DriverVersion() string
	InterfaceVersion() int

	// Logical connection — NOT hardware open/close (see Hardware).
	Connect(ctx context.Context) error    // marks this client session connected
	Disconnect(ctx context.Context) error // marks it disconnected; hardware stays up
	Connected() bool
	Connecting() bool // Platform 7 async-connect state

	// Non-standard functionality (the Alpaca escape hatch).
	SupportedActions() []string
	Action(name, params string) (string, error)
	CommandString(cmd string, raw bool) (string, error)
	CommandBool(cmd string, raw bool) (bool, error)
	CommandBlind(cmd string, raw bool) error

	// DeviceState batches getters into one response.
	DeviceState() []StateValue
}

// Hardware is the persistent-owner lifecycle. If a registered Device also
// implements Hardware, Open is called exactly once when the Server Runs and
// Close exactly once at graceful shutdown. The SDK handle / cooling loop lives
// for the whole process, independent of any Alpaca client's Connected state.
type Hardware interface {
	Open(ctx context.Context) error  // open SDK, start regulation goroutine
	Close(ctx context.Context) error // release on shutdown only
}

// Busyable is an optional interface. If a registered Device implements it, the
// server rejects mutating PUTs with InvalidOperation while Busy() is true — i.e.
// while the device is in a transitory state (a camera exposing/reading, a
// focuser/rotator/wheel moving). Reads are never gated, and the interrupt
// members (abortexposure/stopexposure/halt) are exempt so the operation can
// always be stopped. Busy() must be cheap and non-blocking: it is consulted on
// every write request.
type Busyable interface {
	Busy() bool
}
