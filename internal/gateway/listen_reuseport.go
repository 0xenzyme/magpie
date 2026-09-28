//go:build darwin || linux

package gateway

import "syscall"

func reusePort(_, _ string, c syscall.RawConn) error {
	var err error
	if cerr := c.Control(func(fd uintptr) {
		err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
	}); cerr != nil {
		return cerr
	}
	return err
}
