package spool

import (
	"os"
	"path/filepath"
	"testing"
)

func makeSpool(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The three prefixes are a protocol with the adapter, and each one sends a
// person somewhere different.
func TestEachPrefixIsCountedAsItsOwnThing(t *testing.T) {
	dir := makeSpool(t,
		"ready-0001", "ready-0002",
		".tmp-0003",
		"quarantine-0004", "quarantine-0005", "quarantine-0006")

	contents, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents.Ready) != 2 {
		t.Errorf("ready = %d, want 2: %v", len(contents.Ready), contents.Ready)
	}
	if contents.InProgress != 1 {
		t.Errorf("in progress = %d, want 1", contents.InProgress)
	}
	if contents.Quarantined != 3 {
		t.Errorf("quarantined = %d, want 3", contents.Quarantined)
	}
}

// The adapter names bundles so that sorting by name sorts by capture order, and
// importing them out of order would file later observations before earlier ones.
func TestReadyBundlesComeBackInOrder(t *testing.T) {
	dir := makeSpool(t, "ready-0003", "ready-0001", "ready-0002")

	contents, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"ready-0001", "ready-0002", "ready-0003"} {
		if got := filepath.Base(contents.Ready[index]); got != want {
			t.Errorf("position %d = %s, want %s", index, got, want)
		}
	}
}

// An empty spool is a real answer: the mod ran and recorded nothing, which is a
// different problem from the mod never having run.
func TestAnEmptySpoolIsNotAnError(t *testing.T) {
	contents, err := Read(makeSpool(t))
	if err != nil {
		t.Fatalf("an empty spool was reported as an error: %v", err)
	}
	if len(contents.Ready) != 0 {
		t.Errorf("ready = %v, want none", contents.Ready)
	}
}

// Keeping a bundle after importing it and leaving it counted as outstanding are
// two different promises, and only the first one was wanted. A count that never
// goes down reads as an application that did nothing.
func TestAnImportedBundleStopsCountingAsOutstanding(t *testing.T) {
	dir := makeSpool(t, "ready-0001", "ready-0002")

	before, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkImported(before.Ready[0]); err != nil {
		t.Fatal(err)
	}

	after, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Ready) != 1 {
		t.Errorf("ready = %d, want 1", len(after.Ready))
	}
	if after.Imported != 1 {
		t.Errorf("imported = %d, want 1", after.Imported)
	}
	// The bytes are the point: this is somebody's only copy until they say
	// otherwise.
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 2 {
		t.Errorf("the spool holds %v entries, want both bundles still present", len(entries))
	}
}

func TestMarkingSomethingAlreadyMarkedIsNotAnError(t *testing.T) {
	dir := makeSpool(t, "imported-0001")
	if err := MarkImported(filepath.Join(dir, "imported-0001")); err != nil {
		t.Fatalf("marking an already-marked bundle failed: %v", err)
	}
	contents, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if contents.Imported != 1 {
		t.Errorf("imported = %d, want 1", contents.Imported)
	}
}

func TestAMissingSpoolIsAnError(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a spool that does not exist was read as an empty one")
	}
}

// Loose files are not bundles. The adapter writes directories, and a stray log
// or a thumbnail cache would otherwise be counted as something to import.
func TestLooseFilesAreNotMistakenForBundles(t *testing.T) {
	dir := makeSpool(t, "ready-0001")
	for _, name := range []string{"ready-0002.txt", "quarantine-notes.log", "desktop.ini"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents.Ready) != 1 || contents.Quarantined != 0 {
		t.Fatalf("files were counted: ready=%v quarantined=%d", contents.Ready, contents.Quarantined)
	}
}
