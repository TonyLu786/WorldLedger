// Package mcpath works out where Minecraft keeps its files.
//
// Nothing about these paths is a decision anybody made: they follow from where
// the launcher put Minecraft. Until now the knowledge lived inside the
// ingest-spool command, which meant the only way to use it was to be that
// command. Anything else that needs the same directories -- the desktop
// application needs the mods, config and saves directories as well as the spool
// -- would otherwise work them out a second time, and two answers that disagree
// is how somebody ends up being told their captures are missing while they sit
// in a directory nobody looked in.
//
// Finding returns the candidates it considered alongside the answer, rather
// than composing a message itself. What to say about a directory that is not
// there depends on who is asking: a command can name the flag that overrides
// it, and a window cannot.
package mcpath

import (
	"os"
	"path/filepath"
	"runtime"
)

// Where the capture adapter writes, relative to a Minecraft directory. Written
// with forward slashes and converted on use, so the constant reads the same on
// every platform.
const spoolSuffix = "config/worldledger/spool"

// Install is one Minecraft directory. It says where things are; it does not
// claim any of them exist.
type Install struct {
	Root string
}

func (i Install) Spool() string    { return i.sub(spoolSuffix) }
func (i Install) Mods() string     { return i.sub("mods") }
func (i Install) Config() string   { return i.sub("config") }
func (i Install) Versions() string { return i.sub("versions") }
func (i Install) Saves() string    { return i.sub("saves") }

// CaptureProperties is the file a contributor's name goes in. The adapter
// writes a default copy on first run, so this exists once the mod has started
// even if nobody has edited it.
func (i Install) CaptureProperties() string {
	return i.sub("config/worldledger/capture.properties")
}

// LauncherProfiles is the launcher's own list of installations. A version on
// disk that has no entry here exists and cannot be chosen.
func (i Install) LauncherProfiles() string { return i.sub("launcher_profiles.json") }

// VersionProfile is where a version's manifest lives. The launcher requires the
// directory and the file inside it to carry the same name.
func (i Install) VersionProfile(id string) string {
	return filepath.Join(i.Versions(), id, id+".json")
}

// Mod is a file inside the mods directory.
func (i Install) Mod(name string) string { return filepath.Join(i.Mods(), name) }

func (i Install) sub(suffix string) string {
	return filepath.Join(i.Root, filepath.FromSlash(suffix))
}

// RootsFor is the platform knowledge on its own, with the two values it needs
// passed in.
//
// Reading the environment inside would leave two of the three branches
// untestable anywhere: a test on Windows could never check what a macOS user
// gets.
func RootsFor(goos, appData, home string) []string {
	var roots []string
	switch goos {
	case "windows":
		if appData != "" {
			roots = append(roots, filepath.Join(appData, ".minecraft"))
		}
	case "darwin":
		if home != "" {
			roots = append(roots,
				filepath.Join(home, "Library", "Application Support", "minecraft"),
				filepath.Join(home, ".minecraft"))
		}
	default:
		if home != "" {
			roots = append(roots, filepath.Join(home, ".minecraft"))
		}
	}
	return roots
}

// Roots lists where an unmodified launcher puts Minecraft on this machine, most
// likely first.
func Roots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return RootsFor(runtime.GOOS, os.Getenv("APPDATA"), home)
}

// InstallsFor is Roots as Install values.
func InstallsFor(goos, appData, home string) []Install {
	roots := RootsFor(goos, appData, home)
	out := make([]Install, 0, len(roots))
	for _, root := range roots {
		out = append(out, Install{Root: root})
	}
	return out
}

func Installs() []Install {
	roots := Roots()
	out := make([]Install, 0, len(roots))
	for _, root := range roots {
		out = append(out, Install{Root: root})
	}
	return out
}

// SpoolsFor is where the adapter would write under each candidate root.
func SpoolsFor(goos, appData, home string) []string {
	installs := InstallsFor(goos, appData, home)
	out := make([]string, 0, len(installs))
	for _, install := range installs {
		out = append(out, install.Spool())
	}
	return out
}

func Spools() []string {
	installs := Installs()
	out := make([]string, 0, len(installs))
	for _, install := range installs {
		out = append(out, install.Spool())
	}
	return out
}

// FindSpool returns the first spool directory that exists, along with every
// candidate it considered so the caller can say where it looked.
//
// A candidate that exists but holds nothing is still the answer: an empty spool
// means nothing was captured, which is a different problem from not finding
// Minecraft, and callers report them differently.
func FindSpool() (path string, candidates []string, found bool) {
	candidates = Spools()
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, candidates, true
		}
	}
	return "", candidates, false
}

// FindInstall returns the first Minecraft directory that exists. This is a
// different question from FindSpool: Minecraft can be installed with the mod
// never having run, which is exactly the state the desktop application has to
// recognise in order to offer to fix it.
func FindInstall() (install Install, candidates []Install, found bool) {
	candidates = Installs()
	for _, candidate := range candidates {
		info, err := os.Stat(candidate.Root)
		if err == nil && info.IsDir() {
			return candidate, candidates, true
		}
	}
	return Install{}, candidates, false
}
