package ui

import (
	"fmt"
	"os"
)

// printDecoy fills the screen with innocuous-looking build output so a glance
// over your shoulder reads as "compiling", not "chatting". It ends on a fake
// shell prompt. The next keystroke restores the real chat (handled by the UI),
// so this is a panic-hide, not an interactive shell.
//
// The decoy is deliberately generic and boring; it mimics a typical front-end
// build.
func printDecoy(out *os.File, raw bool) {
	end := "\n"
	if raw {
		end = "\r\n"
	}
	for _, l := range decoyLines {
		fmt.Fprint(out, l, end)
	}
	fmt.Fprint(out, "$ ") // fake prompt, cursor rests here
}

var decoyLines = []string{
	"$ npm run build",
	"",
	"> app@1.4.2 build",
	"> next build",
	"",
	"  ▲ Next.js 15.1.0",
	"",
	"   Creating an optimized production build ...",
	" ✓ Compiled successfully",
	"   Linting and checking validity of types ...",
	"   Collecting page data ...",
	" ✓ Generating static pages (18/18)",
	"   Finalizing page optimization ...",
	"   Collecting build traces ...",
	"",
	"Route (app)                              Size     First Load JS",
	"┌ ○ /                                    1.31 kB         96.4 kB",
	"├ ○ /_not-found                          896 B           89.1 kB",
	"├ ○ /dashboard                           3.42 kB          102 kB",
	"├ λ /api/health                          0 B                0 B",
	"└ ○ /settings                            2.08 kB         95.7 kB",
	"",
	"○  (Static)   prerendered as static content",
	"λ  (Dynamic)  server-rendered on demand",
	"",
	"   Build completed in 9.13s",
	"",
}
