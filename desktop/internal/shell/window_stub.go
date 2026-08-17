package shell

// openWindow is where a native window would be created.
//
// Nothing creates one yet, and this returns "not attempted" rather than a
// failure so that Open goes straight to the browser without printing an
// apology for something it never tried. When a window is added it replaces
// this file behind a build tag, and the browser path stays exactly as it is:
// that is the point of the split.
//
// The second return value distinguishes "a window is showing, stop here" from
// "no window, carry on".
func openWindow(url string) (note string, handled bool) {
	return "", false
}
