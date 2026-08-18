//go:build windows

package devicemain

import "context"

// onReloadSignal is a no-op on Windows, which has no SIGHUP; a reload there
// comes through the setup page.
func onReloadSignal(context.Context, func()) (stop func()) { return func() {} }
