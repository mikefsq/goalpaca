//go:build !linux && !darwin

package alpacadev

import "syscall"

// reuseControl is a no-op on platforms where we don't set SO_REUSEPORT. Direct
// discovery still works for a single device per host; co-binding multiple
// device processes on one host requires DiscoveryRegister (discovery_proxy)
// there. (Windows port-sharing semantics differ and are not attempted here.)
func reuseControl(network, address string, c syscall.RawConn) error {
	return nil
}
