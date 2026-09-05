package server

import (
	"context"
	"sync"
)

// BaseDevice provides identity and logical connection state for device drivers.
// Set its exported identity fields during construction. Hardware access belongs
// in Hardware.Open and Close, independently of the Connected flag.
type BaseDevice struct {
	// Identity — set these when constructing the device.
	ID       string // UniqueID: stable GUID
	DevName  string
	Desc     string
	Info     string
	Version  string
	IfaceVer int

	// Instance is the host's name for this device (a devices.d file's stem),
	// set from registry.Spec.Instance. Label returns it for log lines so a
	// driver's messages and the host's share one identifier.
	Instance string

	mu        sync.Mutex
	connected bool
	connectOp Op
}

func (b *BaseDevice) UniqueID() string      { return b.ID }
func (b *BaseDevice) Name() string          { return b.DevName }
func (b *BaseDevice) Description() string   { return b.Desc }
func (b *BaseDevice) DriverInfo() string    { return b.Info }
func (b *BaseDevice) DriverVersion() string { return b.Version }
func (b *BaseDevice) InterfaceVersion() int { return b.IfaceVer }

// Connect/Disconnect default to synchronous logical bookkeeping. Authors that
// need hooks override these (and may still call MarkConnected/MarkDisconnected).
func (b *BaseDevice) Connect(ctx context.Context) error {
	b.MarkConnected()
	return nil
}

func (b *BaseDevice) Disconnect(ctx context.Context) error {
	b.MarkDisconnected()
	return nil
}

func (b *BaseDevice) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

// Connecting reports the Platform 7 async-connect state. Default Connect is
// synchronous, so this is normally false; authors doing async connect drive
// ConnectOp.
func (b *BaseDevice) Connecting() bool { return b.connectOp.Busy() }

// ConnectError reports the failure of the last async Connect/Disconnect run
// through ConnectOp, or nil. Implements ConnectErrorReporter, so the HTTP
// layer surfaces the failure in-band on the `connecting` completion property.
func (b *BaseDevice) ConnectError() error { return b.connectOp.Err() }

// ConnectOp exposes the async-connect Op for authors implementing Platform 7
// async Connect/Disconnect.
func (b *BaseDevice) ConnectOp() *Op { return &b.connectOp }

// MarkConnected sets the logical Connected flag without touching hardware.
func (b *BaseDevice) MarkConnected() {
	b.mu.Lock()
	b.connected = true
	b.mu.Unlock()
}

// MarkDisconnected sets the logical Connected flag false. The mirror of
// MarkConnected; the hardware stays up.
func (b *BaseDevice) MarkDisconnected() {
	b.mu.Lock()
	b.connected = false
	b.mu.Unlock()
}

// Label returns the host's instance name, or UniqueID when no name is set.
func (b *BaseDevice) Label() string {
	if b.Instance != "" {
		return b.Instance
	}
	return b.ID
}

func (b *BaseDevice) SupportedActions() []string { return []string{} }

func (b *BaseDevice) Action(name, params string) (string, error) {
	return "", ErrActionNotImplemented
}

func (b *BaseDevice) CommandString(cmd string, raw bool) (string, error) {
	return "", ErrNotImplemented
}

func (b *BaseDevice) CommandBool(cmd string, raw bool) (bool, error) {
	return false, ErrNotImplemented
}

func (b *BaseDevice) CommandBlind(cmd string, raw bool) error {
	return ErrNotImplemented
}

// DeviceState returns additional state entries, merged with the standard typed
// getters. Entries with matching names override the standard values.
func (b *BaseDevice) DeviceState() []StateValue { return nil }
