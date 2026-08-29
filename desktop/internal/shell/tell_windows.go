//go:build windows

package shell

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var messageBoxW = user32.NewProc("MessageBoxW")

const (
	messageBoxOK            = 0x00000000
	messageBoxInformation   = 0x00000040
	messageBoxSetForeground = 0x00010000
)

// showElsewhere puts the message on screen, for the build that cannot print it.
//
// A box is the exception rather than how this program talks: it is reached only
// for a message somebody has to act on, and only once printing has already
// failed. The same binary run from a terminal prints, like everything else here
// does.
func showElsewhere(message string) {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	title, err := windows.UTF16PtrFromString("WorldLedger")
	if err != nil {
		return
	}
	// Blocks until it is dismissed, which is the point: the address stays on
	// screen for as long as the person needs to copy it.
	messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		messageBoxOK|messageBoxInformation|messageBoxSetForeground,
	)
}
