package main

import (
	"os/exec"
	"runtime"
)

// openBrowser opens url in the OS default browser. No error checking
// beyond "did the launcher command start" -- if the browser itself fails
// to open (no display, no default browser configured), the caller prints
// url so the operator can open it by hand; see newUIOpenCmd.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// rundll32's url.dll entry point is the standard way to open a
		// URL in the default browser from a Windows console app without
		// invoking a shell, avoiding any shell-quoting/injection concerns
		// with the url string.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
