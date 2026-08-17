// Package shell puts the page in front of the person.
//
// The window is deliberately not load-bearing. Every way of hosting a web view
// depends on something that can be absent or blocked -- a runtime, a graphics
// stack, a policy -- and an application that fails when one of them is missing
// has chosen its own convenience over the person using it. So a window that
// cannot be created is reported once and the browser gets the same application,
// with nothing missing.
package shell

import (
	"os/exec"
	"runtime"
)

// Mode says which way the page was shown, because the two end differently. A
// window closing is the program being closed; a browser tab closing says
// nothing to anybody, so that case has to be watched for instead.
type Mode int

const (
	// InBrowser means Present returned immediately and the page is in a tab.
	InBrowser Mode = iota
	// InWindow means Present ran a window and has now returned because it was
	// closed.
	InWindow
)

// Present shows the page.
//
// In a window it blocks until the window is closed. In a browser it returns as
// soon as the browser has been asked to open. The note is worth printing and is
// empty when there is nothing to say.
//
// preferBrowser skips the window, which is what somebody passes when their
// window works badly and they would rather have a tab.
func Present(url string, preferBrowser bool) (Mode, string) {
	if !preferBrowser {
		note, ran := runWindow(url)
		if ran {
			return InWindow, note
		}
		if note != "" {
			if err := openBrowser(url); err != nil {
				return InBrowser, note + "; and the browser could not be opened either: " + err.Error() +
					"\nopen this address yourself: " + url
			}
			return InBrowser, note + "; opened in the browser instead"
		}
	}
	if err := openBrowser(url); err != nil {
		return InBrowser, "could not open a browser: " + err.Error() + "\nopen this address yourself: " + url
	}
	return InBrowser, ""
}

// openBrowser hands the address to whatever the system uses for one.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		// rundll32 rather than `cmd /c start`, which treats the first quoted
		// argument as a window title and would need an empty one placed just so.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
