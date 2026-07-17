//go:build windows

package notify

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// psScript shows one balloon tip via the stock .NET NotifyIcon — rendered as
// a regular toast on Windows 10/11 — with no helper app and no AppUserModelID
// registration. Title and body arrive as environment variables, never spliced
// into the script, so chat text can't escape into PowerShell syntax. The
// trailing sleep keeps the owning process alive long enough for the balloon
// to render before the icon is disposed.
const psScript = `Add-Type -AssemblyName System.Windows.Forms;` +
	`Add-Type -AssemblyName System.Drawing;` +
	`$n = New-Object System.Windows.Forms.NotifyIcon;` +
	`$n.Icon = [System.Drawing.SystemIcons]::Information;` +
	`$n.Visible = $true;` +
	`$n.BalloonTipTitle = $env:LANCHAT_TITLE;` +
	`$n.BalloonTipText = $env:LANCHAT_BODY;` +
	`$n.ShowBalloonTip(5000);` +
	`Start-Sleep -Seconds 6;` +
	`$n.Dispose()`

// send runs the balloon script in a windowless PowerShell (present on every
// Windows 10/11). Best-effort: any failure — notifications disabled, focus
// assist, timeout — is silently dropped.
func send(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psScript)
	cmd.Env = append(os.Environ(), "LANCHAT_TITLE="+title, "LANCHAT_BODY="+body)
	// -WindowStyle Hidden still flashes a console frame on some setups;
	// CREATE_NO_WINDOW prevents the console from ever existing.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	_ = cmd.Run()
}
