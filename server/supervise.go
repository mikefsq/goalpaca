package server

import (
	"context"
	"log"
	"time"
)

// Supervise restarts fn after a panic, logging through log.Printf and waiting
// one second between attempts. A normal return or cancelled ctx ends supervision.
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

// RunLoop starts fn under Supervise with a child context. The returned stop
// function cancels that context and waits for fn to end, up to timeout.
// Call stop before releasing hardware in Close. The loop must honor cancellation;
// a timeout is logged but does not prevent stop from returning.
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
