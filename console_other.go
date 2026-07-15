//go:build !windows

package main

// enableVirtualTerminal is a no-op on Unix, where terminals already handle
// ANSI escapes and UTF-8.
func enableVirtualTerminal() {}
