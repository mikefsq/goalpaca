package client_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/mikefsq/goalpaca/client"
)

// Alpaca is initiate-then-poll by design: slow operations (exposures, slews,
// connects) return immediately and completion is polled, so cancellation
// lives in the caller's poll loop — no per-method context is needed. The one
// unbounded transfer, the image download, takes a context (ImageArrayCtx).
func Example_pollWithCancellation() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cam := client.NewCamera("127.0.0.1:11111", 0)
	if err := cam.SetConnected(true); err != nil {
		log.Fatal(err)
	}
	defer cam.SetConnected(false)

	if err := cam.StartExposure(30, true); err != nil { // initiator: returns at once
		log.Fatal(err)
	}

	// Poll for completion; ctx bounds the wait and cancels promptly.
	for {
		ready, err := cam.ImageReady()
		if err != nil {
			log.Fatal(err)
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			_ = cam.AbortExposure() // stop the device before giving up
			log.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}

	// The download is the one long transfer: ctx can abort it mid-flight.
	frame, err := cam.ImageArrayCtx(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(frame.Width, frame.Height)
}

// Discovery blocks for its listen window; DiscoverContext ends it early when
// the caller moves on (a closed dialog, an early match).
func ExampleDiscoverContext() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	servers, err := client.DiscoverContext(ctx, 3*time.Second)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
	for _, s := range servers {
		devs, err := s.ConfiguredDevicesContext(ctx)
		if err != nil {
			continue
		}
		for _, d := range devs {
			fmt.Printf("%s: %s %d (%s)\n", s.Address, d.DeviceType, d.DeviceNumber, d.DeviceName)
		}
	}
}
