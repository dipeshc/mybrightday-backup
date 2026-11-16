// Package browser opens URLs in the user's default web browser.
package browser

import (
	"os/exec"
	"runtime"
)

// Open launches the user's default browser at url. It returns once the
// launcher process has started; it does not wait for the browser.
func Open(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
