package server

import (
	"context"
	"log"
	"time"
)

// Supervise runs fn and, if it panics, recovers, logs, and restarts it after a
// short backoff — repeating until ctx is cancelled. It isolates a device's
// background loop so a panic in one device does not crash the process (and with
// it the other devices sharing it). A normal return from fn ends supervision
// (treated as graceful shutdown, e.g. on ctx cancel).
//
// Panics are reported through the standard logger (log.Printf) — deliberately
// independent of any Config.Logger, so a supervised crash is always loud;
// redirect it with log.SetOutput if needed.
func Supervise(ctx context.Context, name string, fn func()) {
	for ctx.Err() == nil {
		returned := func() (returned bool) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("goalpaca: %s panicked: %v; restarting", name, r)
				}
			}()
			fn()
			return true
		}()
		if returned || ctx.Err() != nil {
			return
		}
		time.Sleep(time.Second)
	}
}

// RunLoop starts fn under Supervise in the background, on a context derived
// from ctx, and returns a stop function for Close: it cancels that context and
// blocks until fn has ended, or until timeout. A device's Open runs its
// acquire-monitor loop through it and its Close stops the loop before
// releasing the handle, so the loop cannot re-acquire the hardware between the
// two, which is what a Reload needs; and Close needs no help from the caller
// to end the loop.
//
//	func (d *Dev) Open(ctx context.Context) error {
//		d.stopLoop = server.RunLoop(ctx, d.ID, d.manageHardware)
//		return nil
//	}
//	func (d *Dev) Close(context.Context) error {
//		d.stopLoop(10 * time.Second)
//		d.teardown()
//		return nil
//	}
func RunLoop(ctx context.Context, name string, fn func(ctx context.Context)) (stop func(timeout time.Duration)) {
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Supervise(loopCtx, name, func() { fn(loopCtx) })
	}()
	return func(timeout time.Duration) {
		cancel()
		select {
		case <-done:
		case <-time.After(timeout):
			log.Printf("goalpaca: %s: loop did not stop within %s; closing anyway", name, timeout)
		}
	}
}
