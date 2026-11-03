// Package notify shows desktop notifications, best effort: notify-send on
// Linux, osascript on macOS, nothing elsewhere or when the tool is missing.
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Send shows a notification without waiting for it.
func Send(title, body string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd":
		if _, err := exec.LookPath("notify-send"); err != nil {
			return nil
		}
		cmd = exec.Command("notify-send", "--app-name=ft", title, body)
	case "darwin":
		script := "display notification " + quote(body) + " with title " + quote(title)
		cmd = exec.Command("osascript", "-e", script)
	default:
		return nil
	}
	return cmd.Start()
}

// quote makes an AppleScript string literal.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
