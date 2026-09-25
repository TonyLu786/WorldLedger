package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A committed archive at the layout this build reads.
//
// ADR 0004 was accepted on the condition that one of these exists for every
// layout the code claims to read, because "reads N and N-1" is a cheap sentence
// to write and rots unseen: nothing exercises the older path until the day
// somebody needs it, which is the day it has to work. This is layout 1, and it
// exists now rather than from whenever there is a layout 2 to compare against.
//
// It is also the only test here that opens an archive this process did not
// create. Everything else builds one and reads it back, which cannot catch a
// change that alters what is written and what is read in the same way.

const layoutFixture = "../../testdata/archive-layout-1"

// The archive is opened through a copy, because opening one recovers journals
// and sweeps residue, and a test must not write into committed data.
func openFixture(t *testing.T) Archive {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, layoutFixture, dir)
	a, err := Open(dir)
	if err != nil {
		t.Fatalf("a committed layout %s archive could not be opened by this build: %v", FormatVersion, err)
	}
	return a
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTheCommittedLayoutFixtureIsStillReadable(t *testing.T) {
	version, err := os.ReadFile(filepath.Join(layoutFixture, "VERSION"))
	if err != nil {
		t.Fatalf("the fixture has no VERSION, so it is not an archive: %v", err)
	}
	if got := strings.TrimSpace(string(version)); got != "1" {
		t.Fatalf("the fixture is layout %q; this test is the one for layout 1", got)
	}

	a := openFixture(t)
	if report := a.Check(); len(report.Errors) != 0 {
		t.Errorf("a committed archive does not pass this build's integrity check: %v", report.Errors)
	}
	manifest, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Observations != 1 {
		t.Errorf("the fixture holds %d observation(s), want 1", manifest.Observations)
	}
	if manifest.Objects != 4 {
		t.Errorf("the fixture holds %d object(s), want 4", manifest.Objects)
	}
}

// And the arrangement in it can be derived again, which is what ADR 0004 says
// a layout change costs. If this stops being true, the migration story stops
// being true with it.
func TestTheCommittedFixtureCanHaveItsIndexRebuilt(t *testing.T) {
	a := openFixture(t)
	before, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(a.Root, "index", "chunks")); err != nil {
		t.Fatal(err)
	}
	report, err := a.RebuildIndex()
	if err != nil {
		t.Fatal(err)
	}
	if report.Observations != 1 || len(report.Unreadable) != 0 {
		t.Errorf("rebuilt %d observation(s), %d unreadable", report.Observations, len(report.Unreadable))
	}

	after, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	if after.Root != before.Root {
		t.Errorf("rebuilding changed what the archive holds: %s became %s",
			before.Root[:12], after.Root[:12])
	}
}
