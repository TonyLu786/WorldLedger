// Package shell puts the page in front of the person.
//
// The window is deliberately not load-bearing. Every way of hosting a web view
// depends on something that can be absent or blocked -- a runtime, a graphics
// stack, a policy -- and an application that fails when one of them is missing
// has chosen its own convenience over the person using it. So opening returns a
// note rather than an error, and the browser is always there behind it.
package shell

import (
	"os/exec"
	"runtime"
)

// Open shows the page and returns a note worth printing, or the empty string
// when there is nothing to say.
//
// preferBrowser skips the window entirely, which is what somebody passes when
// their window works badly and they would rather have a tab.
func Open(url string, preferBrowser bool) string {
	if !preferBrowser {
		if note, handled := openWindow(url); handled {
			return note
		} else if note != "" {
			// The window was attempted and could not be had. Say why once, then
			// fall through rather than stopping.
			if err := openBrowser(url); err != nil {
				return note + "; and the browser could not be opened either: " + err.Error() +
					"\nopen this address yourself: " + url
			}
			return note + "; opened in the browser instead"
		}
	}
	if err := openBrowser(url); err != nil {
		return "could not open a browser: " + err.Error() + "\nopen this address yourself: " + url
	}
	return ""
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
