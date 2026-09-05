//go:build windows

package server

import "syscall"

// reuseControl enables SO_REUSEADDR to share the discovery port.
// Broadcast probes reach each responder; directed unicast may reach only one.
// Use DiscoveryRegister when unicast discovery must list every server.
func reuseControl(network, address string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	}); err != nil {
		return err
	}
	return serr
}
