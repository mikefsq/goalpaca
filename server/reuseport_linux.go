//go:build linux

package server

import "syscall"

// soReusePort is the Linux SO_REUSEPORT option value (not exported by the
// std syscall package on Linux).
const soReusePort = 0x0F

// reuseControl enables SO_REUSEADDR and SO_REUSEPORT for shared discovery.
// Broadcast probes reach each responder; directed unicast may reach only one.
// Use DiscoveryRegister when unicast discovery must list every server.
func reuseControl(network, address string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		if e := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); e != nil {
			serr = e
			return
		}
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	}); err != nil {
		return err
	}
	return serr
}
