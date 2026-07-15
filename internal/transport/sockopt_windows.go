//go:build windows

package transport

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// reuseControl is the Windows counterpart of the Unix version. Windows has no
// SO_REUSEPORT; SO_REUSEADDR alone allows the shared bind we need.
func reuseControl(network, address string, c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(fd uintptr) {
		if opErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1); opErr != nil {
			return
		}
		opErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_BROADCAST, 1)
	})
	if err != nil {
		return err
	}
	return opErr
}
