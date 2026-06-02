package alpacadev

import (
	"context"
	"sync"
)

// BaseDevice provides the identity fields and the logical-connection
// bookkeeping shared by every device. Authors embed it in their concrete type
// and set the exported fields (typically in a constructor). It satisfies the
// common Device interface, so a concrete type only needs to add the members of
// its per-type interface (Camera, Focuser, ...).
//
// Connection state here is LOGICAL (per the spec §3.1): MarkConnected /
// MarkDisconnected flip a flag only. They never touch hardware — that is owned
// by Hardware.Open/Close for the life of the process.
type BaseDevice struct {
	// Identity — set these when constructing the device.
	ID       string // UniqueID: stable GUID
	DevName  string
	Desc     string
	Info     string
	Version  string
	IfaceVer int

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

// ConnectOp exposes the async-connect Op for authors implementing Platform 7
// async Connect/Disconnect.
func (b *BaseDevice) ConnectOp() *Op { return &b.connectOp }

func (b *BaseDevice) MarkConnected() {
	b.mu.Lock()
	b.connected = true
	b.mu.Unlock()
}

func (b *BaseDevice) MarkDisconnected() {
	b.mu.Lock()
	b.connected = false
	b.mu.Unlock()
}

// SupportedActions/Action/Command* default to "not implemented". Authors with
// non-standard functionality override them.
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

// DeviceState defaults to just the connection flag. Per-type devices override
// this to batch their commonly-polled getters.
func (b *BaseDevice) DeviceState() []StateValue {
	return []StateValue{{Name: "Connected", Value: b.Connected()}}
}
