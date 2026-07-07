//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly || windows)

package client

import "syscall"

// setBroadcast is a no-op on platforms where enabling SO_BROADCAST this way is
// not portable; directed unicast discovery still works.
func setBroadcast(network, address string, c syscall.RawConn) error { return nil }
