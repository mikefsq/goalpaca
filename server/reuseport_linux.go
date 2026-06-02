//go:build linux

package alpacadev

import "syscall"

// soReusePort is the Linux SO_REUSEPORT option value (not exported by the
// std syscall package on Linux).
const soReusePort = 0x0F

// reuseControl sets SO_REUSEADDR and SO_REUSEPORT on the discovery socket so
// multiple device processes on one host can co-bind UDP 32227. Each then
// answers broadcast discovery probes with its own Alpaca port.
//
// Caveat (verified): broadcast/multicast probes are fanned out to every
// co-bound socket, but a DIRECTED UNICAST probe is load-balanced by the kernel
// to a single responder — so unicast discovery to a multi-device host reaches
// only one device. Use DiscoveryRegister (discovery_proxy) when that matters.
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
