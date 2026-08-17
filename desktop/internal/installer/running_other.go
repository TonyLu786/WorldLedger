//go:build !windows

package installer

// launcherIsRunning has no answer on this platform yet.
//
// Reporting "not running" rather than guessing is the safe direction for a
// check that only ever refuses: the worst case is the same behaviour this had
// before the check existed, on a platform where nothing has been run anyway.
func launcherIsRunning() (bool, string) { return false, "" }
