//go:build !darwin && !windows

package notify

import (
	"context"
	"os/exec"
	"time"
)

// send posts one desktop notification via notify-send (libnotify), present on
// most Linux desktops. Headless boxes won't have it, and that's fine: LookPath
// fails and the banner is silently dropped. Title and body are passed as argv
// after a "--" terminator — no shell, no option parsing — so a message that
// starts with "-" can't be read as a flag. Urgency low keeps it quiet.
func send(title, body string) {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, path, "-u", "low", "-a", "lanchat", "--", title, body).Run()
}
