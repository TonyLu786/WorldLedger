//go:build windows

package shell

import (
	"runtime"

	"github.com/jchv/go-webview2"
)

// runWindow hosts the page in a native window.
//
// It returns ran=false with a note when a window could not be made, which is
// the case Present falls back from. The runtime this needs ships with Windows
// 10 and 11 as part of Edge, but it can be absent on a stripped image or
// blocked by policy, and that is somebody's ordinary Tuesday rather than an
// exceptional condition.
func runWindow(url string) (note string, ran bool) {
	// The window's message loop has to be on the thread that created it, and Go
	// will otherwise move this goroutine between threads underneath it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// The library panics rather than returning an error when the runtime is not
	// there, so this is where that becomes a fallback instead of a crash.
	defer func() {
		if recovered := recover(); recovered != nil {
			note = "a window could not be opened on this computer"
			ran = false
		}
	}()

	// Before the window exists, and only useful before it exists.
	declareDPIAware()
	scale := windowScale()

	view := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: "WorldLedger",
			// Asked for in real pixels, so a display scaled above 100% needs a
			// bigger number for the same window.
			Width:  uint(1100 * scale),
			Height: uint(760 * scale),
			Center: true,
		},
	})
	if view == nil {
		return "a window could not be opened on this computer", false
	}
	defer view.Destroy()

	view.Navigate(url)
	// Blocks until the window is closed, which is what ends the program.
	view.Run()
	return "", true
}
