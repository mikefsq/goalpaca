// Package server is a library for hosting one or more hardware devices as a
// standalone ASCOM Alpaca server (HTTP/JSON REST + UDP discovery).
//
// A device author implements a typed per-type interface (Camera, Focuser, ...)
// plus the Hardware lifecycle interface; this library handles the wire
// protocol, discovery participation, async semantics, image transport, and
// liveness. One process owns the hardware for its entire life — the Alpaca
// Connected flag is a logical per-client session marker, never a hardware
// open/close (see BaseDevice and the Hardware interface).
//
// # Protocol compliance
//
// The library enforces the device-independent ASCOM rules in its HTTP dispatch
// layer, before a driver method runs: parameter-range validation (returning
// InvalidValue), capability-flag gating (a member returns NotImplemented when
// its CanXxx reports false), telescope parked gating (ParkedException from
// movement members while AtPark), target read-before-set and image-not-ready
// gating (InvalidOperation), and the ITelescopeV4 sidereal-only rate-offset,
// axis-rate, and drive-rate rules. A driver that implements the typed
// interfaces is therefore Alpaca/ConformU-conformant without writing any of
// this — it implements only hardware-specific behavior, and may impose its
// own stricter hardware limits, which run after these gates. All ten device
// types built on this library pass ASCOM ConformU v4.4.0 with zero issues.
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

	// DeviceState contributes driver-specific entries to the Platform 7
	// DeviceState batch. The library builds the standard per-type operational
	// set from the typed getters; whatever this returns is merged on top
	// (same-name entries override, new names append). Return nil (the
	// BaseDevice default) when the standard set suffices.
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
// members that end an in-flight operation are exempt so it can always be
// stopped — e.g. abortexposure, stopexposure, abortslew, haltcover,
// calibratoroff, halt, and the async-cancel members (see interruptMembers for
// the full set). Busy() must be cheap and non-blocking: it is consulted on
// every write request.
type Busyable interface {
	Busy() bool
}

// ConnectErrorReporter is an optional interface for the Platform 7 async
// connect pattern: when a device implements it and an async Connect/Disconnect
// has FAILED (no longer in flight, error recorded), a GET of the `connecting`
// completion property reports that error in-band instead of a bare false — so
// a client polling for completion learns WHY the connect failed rather than
// seeing a device that silently never became connected.
//
// BaseDevice implements it via ConnectOp: drivers that run async connects
// through ConnectOp().Begin/Complete/Fail get failure surfacing for free; the
// error clears on the next Begin or Reset.
type ConnectErrorReporter interface {
	ConnectError() error
}
