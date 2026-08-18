//go:build !windows

package devicemain

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// onReloadSignal runs fn on every SIGHUP until ctx ends or the returned stop
// is called. systemd's ExecReload= and a hand-typed kill -HUP both land here;
// the setup page's reload is the same operation over HTTP.
func onReloadSignal(ctx context.Context, fn func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				fn()
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
