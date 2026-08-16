package main

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
	for _, test := range []struct {
		goos    string
		appData string
		home    string
		want    []string
	}{
		{
			goos:    "windows",
			appData: `C:\Users\alice\AppData\Roaming`,
			home:    `C:\Users\alice`,
			want:    []string{`C:\Users\alice\AppData\Roaming\.minecraft`},
		},
		{
			goos: "darwin",
			home: "/Users/alice",
			want: []string{
				"/Users/alice/Library/Application Support/minecraft",
				"/Users/alice/.minecraft",
			},
		},
		{
			goos: "linux",
			home: "/home/alice",
			want: []string{"/home/alice/.minecraft"},
		},
	} {
		got := spoolCandidatesFor(test.goos, test.appData, test.home)
		if len(got) != len(test.want) {
			t.Errorf("%s: got %d candidate(s) %v, want %d", test.goos, len(got), got, len(test.want))
			continue
		}
		for index, want := range test.want {
			expected := filepath.Join(want, filepath.FromSlash(spoolSuffix))
			if got[index] != expected {
				t.Errorf("%s candidate %d = %q, want %q", test.goos, index, got[index], expected)
			}
		}
	}
}

// macOS moved the launcher's directory and both locations are still in use, so
// the older one has to be looked at as well rather than instead.
func TestMacOsLooksInBothLocations(t *testing.T) {
	got := spoolCandidatesFor("darwin", "", "/Users/alice")
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
		if got := spoolCandidatesFor(test.goos, test.appData, test.home); len(got) != 0 {
			t.Errorf("%s with nothing to go on returned %v", test.goos, got)
		}
	}
}

// A platform nobody listed still gets the location that is right almost
// everywhere, rather than nothing at all.
func TestAnUnlistedPlatformFallsBackToTheDotDirectory(t *testing.T) {
	got := spoolCandidatesFor("freebsd", "", "/home/alice")
	want := filepath.Join("/home/alice/.minecraft", filepath.FromSlash(spoolSuffix))
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v; want [%s]", got, want)
	}
}

func TestFindSpoolReturnsADirectoryThatExists(t *testing.T) {
	home := t.TempDir()
	spool := filepath.Join(home, ".minecraft", filepath.FromSlash(spoolSuffix))
	if err := os.MkdirAll(spool, 0o755); err != nil {
		t.Fatal(err)
	}
	candidates := spoolCandidatesFor("linux", "", home)
	if len(candidates) != 1 || candidates[0] != spool {
		t.Fatalf("candidates = %v; want [%s]", candidates, spool)
	}
}

// The message for a machine with no spool has to name where it looked. "Not
// found" on its own leaves someone with no way to tell a missing capture from a
// Minecraft that lives somewhere unusual.
func TestNotFindingASpoolNamesEveryPlaceItLooked(t *testing.T) {
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "nowhere"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "nowhere"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "nowhere"))

	_, err := findSpool()
	if err == nil {
		t.Fatal("a spool was found under directories that do not exist")
	}
	for _, candidate := range spoolCandidates() {
		if !strings.Contains(err.Error(), candidate) {
			t.Errorf("the error does not name %q:\n%s", candidate, err)
		}
	}
	if !strings.Contains(err.Error(), "ingest-spool") {
		t.Errorf("the error does not say how to pass a directory:\n%s", err)
	}
}
