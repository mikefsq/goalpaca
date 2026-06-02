//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package client

import "syscall"

// setBroadcast enables SO_BROADCAST on the UDP socket so discovery probes may be
// sent to broadcast addresses.
func setBroadcast(network, address string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return serr
}
