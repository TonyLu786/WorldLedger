package archive

import (
	"os"
	"path/filepath"
	"testing"
)

// The index is a restatement of the observations, arranged for reading. That is
// worth a function that proves it: it repairs an archive whose index is damaged,
// which was otherwise permanent, and it is what makes a change to the layout
// affordable, because a layout change is a change to the arrangement and not to
// the records.

func TestAnIndexThrownAwayEntirelyComesBack(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		storedObservation(t, a, "minecraft:overworld", int32(i), 0, "alice", i+1, "state")
	}
	before, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(dir, "index", "chunks")); err != nil {
		t.Fatal(err)
	}
	if report := a.Check(); len(report.Errors) == 0 {
		t.Fatal("an archive with no index passed its own check, so this test would prove nothing")
	}

	report, err := a.RebuildIndex()
	if err != nil {
		t.Fatal(err)
	}
	if report.Observations != 5 {
		t.Errorf("rebuilt from %d observations, want 5", report.Observations)
	}
	if report.Chunks != 5 {
		t.Errorf("rebuilt %d chunks, want 5", report.Chunks)
	}

	if check := a.Check(); len(check.Errors) != 0 {
		t.Errorf("the archive does not pass its own check after a rebuild: %v", check.Errors)
	}
	after, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	if after.Root != before.Root {
		t.Errorf("the rebuild changed what the archive holds: %s became %s", before.Root[:12], after.Root[:12])
	}
}

// A stale entry naming an observation that is gone is the other half of the
// same damage, and it used to be permanent in the same way.
func TestAnIndexNamingSomethingThatIsGoneIsCorrected(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	kept := storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "one")

	path := a.chunkIndexPath(kept.Chunk)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ghost := "0000000000000000000000000000000000000000000000000000000000000000"
	if err := os.WriteFile(path, append(body, []byte(ghost+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if report := a.Check(); len(report.Errors) == 0 {
		t.Fatal("an index naming a missing observation passed the check")
	}

	if _, err := a.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	if check := a.Check(); len(check.Errors) != 0 {
		t.Errorf("the stale entry survived a rebuild: %v", check.Errors)
	}
}

// A record that cannot be placed is left where it is and reported. An index can
// be built again; a record cannot, so a rebuild that tidied away what it could
// not parse would trade the recoverable thing for the unrecoverable one.
func TestARecordThatCannotBePlacedIsReportedAndKept(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "one")

	damaged := filepath.Join(dir, "observations", "ab", "ab"+
		"00000000000000000000000000000000000000000000000000000000000000.json")
	if err := os.MkdirAll(filepath.Dir(damaged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(damaged, []byte("{ this is not an observation"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := a.RebuildIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unreadable) != 1 {
		t.Errorf("reported %d unreadable record(s), want 1: %v", len(report.Unreadable), report.Unreadable)
	}
	if _, err := os.Stat(damaged); err != nil {
		t.Errorf("the rebuild removed a record it could not read: %v", err)
	}
}
