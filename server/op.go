package alpacadev

import "sync"

// OpState is the lifecycle of an async ASCOM operation.
type OpState int

const (
	OpIdle OpState = iota
	OpBusy
	OpDone
	OpFailed
)

// Op backs the ASCOM async pattern: an initiator (StartExposure, SlewToXxxAsync,
// async Connect) calls Begin and runs the work in a goroutine, returning
// immediately; the completion property (ImageReady, Slewing, Connecting) reads
// the Op state. Op is safe for concurrent use.
type Op struct {
	mu    sync.Mutex
	state OpState
	err   error
}

// Begin marks the operation in-flight. It is a no-op guard against starting an
// already-busy op is the caller's responsibility (check Busy first if needed).
func (o *Op) Begin() {
	o.mu.Lock()
	o.state = OpBusy
	o.err = nil
	o.mu.Unlock()
}

// Complete marks success.
func (o *Op) Complete() {
	o.mu.Lock()
	o.state = OpDone
	o.mu.Unlock()
}

// Fail marks failure and records err (surfaced to the next status read).
func (o *Op) Fail(err error) {
	o.mu.Lock()
	o.state = OpFailed
	o.err = err
	o.mu.Unlock()
}

// Reset returns the op to idle.
func (o *Op) Reset() {
	o.mu.Lock()
	o.state = OpIdle
	o.err = nil
	o.mu.Unlock()
}

// Busy reports whether the operation is in-flight. Backs Slewing,
// Connecting, and the inverse of ImageReady.
func (o *Op) Busy() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state == OpBusy
}

// State returns the current state.
func (o *Op) State() OpState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state
}

// Err returns the failure error, if the last run failed.
func (o *Op) Err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}
