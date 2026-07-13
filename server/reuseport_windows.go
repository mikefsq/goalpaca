//go:build windows

package server

import "syscall"

// reuseControl sets SO_REUSEADDR on the discovery socket so multiple device
// processes on one host can co-bind UDP 32227 — the same property SO_REUSEPORT
// gives on Linux/BSD, which Windows does not have.
//
// Windows SO_REUSEADDR is not the Unix option of the same name: on Unix it only
// relaxes the TIME_WAIT check and does NOT permit two live sockets on one port
// (hence SO_REUSEPORT), whereas on Windows it genuinely shares the port. Verified
// behaviour: a *broadcast* datagram — which is what an Alpaca discovery probe is —
// is delivered to every socket co-bound this way, so each responder answers with
// its own Alpaca port and all of them appear in a Discover list.
//
// Caveat, the same one that applies to SO_REUSEPORT: a *directed unicast* probe
// reaches only one of the co-bound sockets (on Windows, the last bound). The usual
// "Discover" button broadcasts, so this is not normally a problem; use
// DiscoveryRegister (discovery_proxy) when unicast discovery to a multi-device host
// must work.
//
// Note also that Windows SO_REUSEADDR lets an unrelated process bind a port already
// in use (the classic hijack hole SO_EXCLUSIVEADDRUSE exists to close). We accept
// that here: discovery is an unauthenticated LAN broadcast responder on a fixed,
// well-known port, so port sharing is the intended behaviour, not a leak.
func reuseControl(network, address string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	}); err != nil {
		return err
	}
	return serr
}
