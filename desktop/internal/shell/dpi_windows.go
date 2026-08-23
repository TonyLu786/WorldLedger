//go:build windows

package shell

import (
	"golang.org/x/sys/windows"
)

// Telling Windows this program can draw at the screen's real resolution.
//
// Without it a process is treated as though it were written for 96 dots per
// inch, and on any display scaled above 100% Windows renders the window at that
// size and then stretches the result. Everything still works and every letter
// is soft, which is how this was found: somebody used the window and said the
// text was blurry.
//
// It has to happen before a window exists and can only be set once per process,
// so it is the first thing runWindow does.

var (
	user32                                = windows.NewLazySystemDLL("user32.dll")
	shcore                                = windows.NewLazySystemDLL("shcore.dll")
	setProcessDpiAwarenessContext         = user32.NewProc("SetProcessDpiAwarenessContext")
	setProcessDPIAware                    = user32.NewProc("SetProcessDPIAware")
	getDpiForSystem                       = user32.NewProc("GetDpiForSystem")
	setProcessDpiAwareness                = shcore.NewProc("SetProcessDpiAwareness")
	perMonitorAwareV2                     = ^uintptr(3) // the -4 handle constant
	processPerMonitorDpiAware             = uintptr(2)
	defaultDotsPerInch            float64 = 96
)

// declareDPIAware asks for the best awareness this Windows offers.
//
// The three calls are three generations of the same request, and a machine only
// answers one of them. None of them failing is not worth reporting: the window
// still opens and still works, it is only soft, and a program that refuses to
// start over that would be trading a real feature for a cosmetic one.
func declareDPIAware() {
	if err := setProcessDpiAwarenessContext.Find(); err == nil {
		if ok, _, _ := setProcessDpiAwarenessContext.Call(perMonitorAwareV2); ok != 0 {
			return
		}
	}
	if err := setProcessDpiAwareness.Find(); err == nil {
		if result, _, _ := setProcessDpiAwareness.Call(processPerMonitorDpiAware); result == 0 {
			return
		}
	}
	if err := setProcessDPIAware.Find(); err == nil {
		setProcessDPIAware.Call()
	}
}

// windowScale is how much bigger than its nominal size the window should be
// asked for.
//
// The size passed to the window is in real pixels, so once this process draws
// at the screen's resolution a window asked for in 96-dpi numbers comes out
// physically smaller on a scaled display -- the blur is gone and the window is
// two thirds of the size it was meant to be. Scaling the request puts it back.
func windowScale() float64 {
	if err := getDpiForSystem.Find(); err != nil {
		return 1
	}
	dpi, _, _ := getDpiForSystem.Call()
	if dpi == 0 {
		return 1
	}
	scale := float64(dpi) / defaultDotsPerInch
	// A display reporting something absurd should not produce a window that
	// cannot be reached or cannot be read.
	if scale < 1 {
		return 1
	}
	if scale > 4 {
		return 4
	}
	return scale
}
