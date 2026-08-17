//go:build !windows

package shell

// runWindow is where a native window would be created on this platform.
//
// Nothing creates one yet, and this returns "not attempted" rather than a
// failure so Present goes straight to the browser without printing an apology
// for something it never tried. The desktop application targets Windows first
// and says so; the browser path here is the whole application, not a degraded
// one.
func runWindow(url string) (note string, ran bool) {
	return "", false
}
