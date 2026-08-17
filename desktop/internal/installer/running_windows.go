//go:build windows

package installer

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// launcherIsRunning reports whether the Minecraft launcher is open.
//
// Only the launcher is looked for, and that is the point. The game itself can
// be running while this installs: mods and version profiles are read at start
// up, so writing them takes effect next launch and disturbs nothing in the
// meantime. launcher_profiles.json is different. It belongs to the launcher,
// the launcher writes it whenever it feels like it, and read-modify-write from
// two programs at once is how somebody's list of installations disappears.
//
// Looking for javaw instead would be both wider and less accurate: it would
// block on any Java program at all, including this project's own build.
func launcherIsRunning() (bool, string) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		// Not being able to look is not evidence of absence, and refusing to
		// install because a process listing failed would be worse than the risk
		// it guards against.
		return false, ""
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return false, ""
	}
	for {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		switch name {
		case "minecraft.exe", "minecraftlauncher.exe":
			return true, windows.UTF16ToString(entry.ExeFile[:])
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			return false, ""
		}
	}
}
