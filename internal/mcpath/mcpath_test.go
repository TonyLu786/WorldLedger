package mcpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Getting a platform wrong here does not look like a bug. It looks like a
// person's captures having vanished, while they sit in a directory nobody
// looked in. All three are checked from one machine because only one of them
// can ever be the machine running the test.

func TestEachPlatformLooksWhereItsLauncherPutsMinecraft(t *testing.T) {
	// The roots are given as components rather than as written-out paths. A
	// literal like `C:\Users\alice\.minecraft` composes correctly only on the
	// platform whose separator it used, and these run on whichever machine the
	// suite happens to be on: this test failed in Linux CI for exactly that
	// reason while passing on Windows. Which roots are chosen and in what order
	// is the platform knowledge worth checking. How they are joined is
	// filepath's job.
	const appData = "APPDATA"
	const home = "HOME"

	for _, test := range []struct {
		goos  string
		roots [][]string
	}{
		{goos: "windows", roots: [][]string{{appData, ".minecraft"}}},
		{goos: "darwin", roots: [][]string{
			{home, "Library", "Application Support", "minecraft"},
			{home, ".minecraft"},
		}},
		{goos: "linux", roots: [][]string{{home, ".minecraft"}}},
	} {
		got := SpoolsFor(test.goos, appData, home)
		if len(got) != len(test.roots) {
			t.Errorf("%s: got %d candidate(s) %v, want %d", test.goos, len(got), got, len(test.roots))
			continue
		}
		for index, parts := range test.roots {
			want := filepath.Join(append(parts, filepath.FromSlash(spoolSuffix))...)
			if got[index] != want {
				t.Errorf("%s candidate %d = %q, want %q", test.goos, index, got[index], want)
			}
		}
	}
}

// macOS moved the launcher's directory and both locations are still in use, so
// the older one has to be looked at as well rather than instead.
func TestMacOsLooksInBothLocations(t *testing.T) {
	got := SpoolsFor("darwin", "", "/Users/alice")
	if len(got) != 2 {
		t.Fatalf("got %v; want the Application Support path and the dot directory", got)
	}
	if !strings.Contains(got[0], "Application Support") {
		t.Errorf("the current location should be looked at first, got %q", got[0])
	}
}

// Nothing to build a path from is different from a path that does not exist.
// Guessing a relative directory would search whatever the caller happened to be
// standing in.
func TestNoHomeProducesNoCandidatesRatherThanARelativeGuess(t *testing.T) {
	for _, test := range []struct{ goos, appData, home string }{
		{"windows", "", ""},
		{"darwin", "", ""},
		{"linux", "", ""},
	} {
		if got := SpoolsFor(test.goos, test.appData, test.home); len(got) != 0 {
			t.Errorf("%s with nothing to go on returned %v", test.goos, got)
		}
	}
}

// A platform nobody listed still gets the location that is right almost
// everywhere, rather than nothing at all.
func TestAnUnlistedPlatformFallsBackToTheDotDirectory(t *testing.T) {
	got := SpoolsFor("freebsd", "", "/home/alice")
	want := filepath.Join("/home/alice/.minecraft", filepath.FromSlash(spoolSuffix))
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v; want [%s]", got, want)
	}
}

func TestSpoolCandidateIsUnderTheRootThatWasFound(t *testing.T) {
	home := t.TempDir()
	spool := filepath.Join(home, ".minecraft", filepath.FromSlash(spoolSuffix))
	if err := os.MkdirAll(spool, 0o755); err != nil {
		t.Fatal(err)
	}
	candidates := SpoolsFor("linux", "", home)
	if len(candidates) != 1 || candidates[0] != spool {
		t.Fatalf("candidates = %v; want [%s]", candidates, spool)
	}
}

// Finding nothing has to hand back the list anyway. A caller that cannot say
// where it looked can only report "not found", which leaves somebody unable to
// tell a missing capture from a Minecraft that lives somewhere unusual.
func TestNotFindingASpoolStillReportsEveryPlaceItLooked(t *testing.T) {
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "nowhere"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "nowhere"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "nowhere"))

	path, candidates, found := FindSpool()
	if found {
		t.Fatalf("a spool was found under directories that do not exist: %s", path)
	}
	if len(candidates) == 0 {
		t.Fatal("nothing was reported as having been looked at")
	}
	for _, candidate := range candidates {
		if !strings.Contains(candidate, "nowhere") {
			t.Errorf("candidate %q is not under the temporary home the test set", candidate)
		}
	}
}

// The desktop application has to tell "Minecraft is not installed" apart from
// "Minecraft is installed but the mod has never run", because the second is the
// state it exists to fix and the first is not something it can fix at all.
func TestAnInstallWithNoSpoolIsStillFound(t *testing.T) {
	// Every variable points at the same directory because which one is consulted
	// depends on the platform running the test: Windows takes the root from
	// APPDATA and the others from the home directory. Setting only one of them
	// passes on the machine it was written on and finds nothing on the others.
	home := t.TempDir()
	root := filepath.Join(home, ".minecraft")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	install, _, found := FindInstall()
	if !found {
		t.Fatal("a Minecraft directory that exists was not found")
	}
	if install.Root != root {
		t.Fatalf("root = %q, want %q", install.Root, root)
	}
	if _, _, spoolFound := FindSpool(); spoolFound {
		t.Error("a spool was reported under an install where the mod has never run")
	}
}

// Every directory the installer and the importer touch hangs off the root, so a
// wrong root has to be wrong everywhere rather than in one accessor.
func TestEveryDirectoryHangsOffTheRoot(t *testing.T) {
	install := Install{Root: filepath.Join("somewhere", ".minecraft")}
	for name, got := range map[string]string{
		"Spool":             install.Spool(),
		"Mods":              install.Mods(),
		"Config":            install.Config(),
		"Versions":          install.Versions(),
		"Saves":             install.Saves(),
		"CaptureProperties": install.CaptureProperties(),
	} {
		if !strings.HasPrefix(got, install.Root+string(filepath.Separator)) {
			t.Errorf("%s() = %q, which is not under %q", name, got, install.Root)
		}
	}
}
