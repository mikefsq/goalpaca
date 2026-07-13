//go:build !linux && !darwin && !windows

package server

import "syscall"

// reuseControl is a no-op on platforms with no port-sharing option we know of
// (linux/darwin use SO_REUSEPORT, windows SO_REUSEADDR; each has its own file).
// Direct discovery still works for a single device per host; co-binding multiple
// device processes on one host requires DiscoveryRegister (discovery_proxy) there —
// a second bind of the discovery port fails with address-in-use.
func reuseControl(network, address string, c syscall.RawConn) error {
	return nil
}
