//go:build windows

package main

import "golang.org/x/sys/windows"

// enableVirtualTerminal prepares the Windows console for our terminal UI.
//
// The UI drives the screen with ANSI/VT escape sequences (line redraw, clear,
// colors) and prints UTF-8 glyphs (» · →). Legacy Windows consoles need both
// of these turned on explicitly, or the escapes show up as literal garbage and
// the glyphs render as mojibake. This is a best-effort call: any step that
// fails is ignored, leaving behavior no worse than before.
func enableVirtualTerminal() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil {
		var mode uint32
		if windows.GetConsoleMode(h, &mode) == nil {
			_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
		}
	}
	// Render output/input as UTF-8 (code page 65001) on consoles that default
	// to a legacy code page. Not wrapped by x/sys, so call kernel32 directly.
	k := windows.NewLazySystemDLL("kernel32.dll")
	_, _, _ = k.NewProc("SetConsoleOutputCP").Call(65001)
	_, _, _ = k.NewProc("SetConsoleCP").Call(65001)
}
