//go:build darwin

package alpacadev

import "syscall"

// soReusePort is the Darwin/BSD SO_REUSEPORT option value.
const soReusePort = 0x0200

// reuseControl sets SO_REUSEADDR and SO_REUSEPORT so multiple device processes
// can co-bind UDP 32227. See the linux variant for the unicast caveat.
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
