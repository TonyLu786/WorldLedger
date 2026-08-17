package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// Where to look is checked in internal/mcpath, which owns it. What is left here
// is what this command says when there is nothing there, which is the part that
// names this command's own flag and would be wrong in any other caller's mouth.

// "Not found" on its own leaves someone with no way to tell a missing capture
// from a Minecraft that lives somewhere unusual.
func TestNotFindingASpoolNamesEveryPlaceItLooked(t *testing.T) {
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "nowhere"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "nowhere"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "nowhere"))

	_, err := findSpool()
	if err == nil {
		t.Fatal("a spool was found under directories that do not exist")
	}
	candidates := mcpath.Spools()
	if len(candidates) == 0 {
		t.Fatal("no candidates were produced, so this test would pass vacuously")
	}
	for _, candidate := range candidates {
		if !strings.Contains(err.Error(), candidate) {
			t.Errorf("the error does not name %q:\n%s", candidate, err)
		}
	}
	if !strings.Contains(err.Error(), "ingest-spool") {
		t.Errorf("the error does not say how to pass a directory:\n%s", err)
	}
}
