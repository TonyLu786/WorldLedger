//go:build !windows

package shell

// showElsewhere has nowhere else to go on these platforms, and a message that
// could not be printed is lost, which is what this program has always done
// everywhere. The desktop application targets Windows first and says so, and
// Windows is where the missing console is a deliberate property of the build
// rather than somebody's unusual setup.
func showElsewhere(string) {}
