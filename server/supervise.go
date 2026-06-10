package alpacadev

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
func Supervise(ctx context.Context, name string, fn func()) {
	for ctx.Err() == nil {
		returned := func() (returned bool) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("alpacadev: %s panicked: %v; restarting", name, r)
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
