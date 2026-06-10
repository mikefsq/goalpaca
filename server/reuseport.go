package alpacadev

import "syscall"

// ReuseControl sets SO_REUSEADDR/SO_REUSEPORT (where supported) on a socket so a
// caller can co-bind the discovery port (32227) alongside other Alpaca servers on
// the same host. It is the exported form of the control used by direct discovery.
func ReuseControl(network, address string, c syscall.RawConn) error {
	return reuseControl(network, address, c)
}
