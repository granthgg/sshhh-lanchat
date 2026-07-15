//go:build !windows

package ui

// EnableVirtualTerminal is a no-op on Unix, where terminals already handle ANSI
// escapes and UTF-8.
func EnableVirtualTerminal() {}
