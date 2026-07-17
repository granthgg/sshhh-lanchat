//go:build darwin

package notify

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// send posts one Notification Center banner through osascript, which ships
// with every macOS. The title and body travel as environment variables and
// are read back with AppleScript's `system attribute`, so chat text is never
// spliced into the script source — quotes, backslashes and emoji can neither
// break the script nor inject into it. No sound is requested: the banner is
// deliberately quiet. Failures (permission denied, timeout) are dropped;
// delivery is best-effort by design.
func send(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript",
		"-e", `display notification (system attribute "LANCHAT_BODY") with title (system attribute "LANCHAT_TITLE")`)
	cmd.Env = append(os.Environ(), "LANCHAT_TITLE="+title, "LANCHAT_BODY="+body)
	_ = cmd.Run()
}
