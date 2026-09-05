// Package server hosts ASCOM Alpaca devices over HTTP with UDP discovery.
// Drivers implement a typed device interface and, for hardware access, Hardware.
// The server handles wire encoding, discovery, and common protocol validation.
package server

import "context"

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
	Connect(ctx context.Context) error    // sets logical connection state
	Disconnect(ctx context.Context) error // clears logical state; hardware stays open
	Connected() bool
	Connecting() bool // Platform 7 async-connect state

	// Non-standard functionality (the Alpaca escape hatch).
	SupportedActions() []string
	Action(name, params string) (string, error)
	CommandString(cmd string, raw bool) (string, error)
	CommandBool(cmd string, raw bool) (bool, error)
	CommandBlind(cmd string, raw bool) error

	// DeviceState contributes driver-specific entries to the Platform 7
	// DeviceState batch. The library builds the standard per-type operational
	// set from the typed getters; whatever this returns is merged on top
	// (same-name entries override, new names append). Return nil (the
	// BaseDevice default) when the standard set suffices.
	DeviceState() []StateValue
}

// Hardware manages a device's hardware independently of its logical connection.
// The server calls Open at startup or registration and Close at shutdown,
// unregistration, or reload. A reload opens the replacement after closing the old device.
//
// Open's context is cancelled before Close. Close must wait for background
// workers to stop before releasing hardware; see RunLoop.
type Hardware interface {
	Open(ctx context.Context) error  // open SDK, start regulation goroutine
	Close(ctx context.Context) error // release on shutdown only
}

// Busyable lets the server reject mutating PUTs while a device is busy.
// Reads and interrupt members remain available; see interruptMembers.
// Busy must be cheap and non-blocking. Drivers must also guard concurrent starts.
type Busyable interface {
	Busy() bool
}

// ConnectErrorReporter supplies the error returned by the connecting property
// after an asynchronous Connect or Disconnect fails. BaseDevice implements it
// through ConnectOp; Begin and Reset clear the error.
type ConnectErrorReporter interface {
	ConnectError() error
}
