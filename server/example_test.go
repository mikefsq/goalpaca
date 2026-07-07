package server_test

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/mikefsq/goalpaca/server"
)

// exampleFocuser is a minimal absolute focuser: embed the Base type for sane
// defaults and override what the hardware supports.
type exampleFocuser struct {
	server.BaseFocuser
	pos int
}

func (f *exampleFocuser) Absolute() bool         { return true }
func (f *exampleFocuser) MaxStep() int           { return 60000 }
func (f *exampleFocuser) Position() (int, error) { return f.pos, nil }
func (f *exampleFocuser) Move(target int) error {
	if target < 0 || target > f.MaxStep() {
		return server.ErrInvalidValue
	}
	f.pos = target
	return nil
}

// Hosting a device: register a driver and run. The library handles the wire
// protocol, discovery, connection/busy gating, and graceful shutdown.
func Example() {
	foc := &exampleFocuser{}
	foc.ID = "0f4bfbc8-0000-0000-0000-000000000001" // stable GUID
	foc.DevName = "Example Focuser"
	foc.IfaceVer = 4

	srv := server.New(server.Config{
		AlpacaPort: 11111,
		Discovery:  server.DiscoveryConfig{Mode: server.DiscoveryDirect},
		ServerName: "Example Alpaca Server",
	})
	if err := srv.Register(server.FocuserType, 0, foc); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx); err != nil { // blocks until ctx is cancelled
		log.Fatal(err)
	}
}
