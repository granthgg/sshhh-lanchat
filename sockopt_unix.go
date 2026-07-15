//go:build !windows

package main

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// reuseControl lets several instances bind the same UDP port (needed so more
// than one chat can run on one machine, and so joining a multicast group works
// cleanly) and enables sending to the subnet broadcast address as a fallback
// for networks that drop multicast.
func reuseControl(network, address string, c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(fd uintptr) {
		if opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); opErr != nil {
			return
		}
		if opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); opErr != nil {
			return
		}
		opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
	})
	if err != nil {
		return err
	}
	return opErr
}
