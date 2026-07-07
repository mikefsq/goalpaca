//go:build windows

package client

import "syscall"

// setBroadcast enables SO_BROADCAST on the UDP socket so discovery probes may be
// sent to broadcast addresses. Without it Windows rejects broadcast sends with
// WSAEACCES, leaving discovery loopback-only.
func setBroadcast(network, address string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return serr
}
