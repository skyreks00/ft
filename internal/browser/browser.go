// Package browser opens URLs in the user's default browser.
package browser

import (
	"os/exec"
	"runtime"
)

// Open starts the default browser on u without waiting for it.
func Open(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}
