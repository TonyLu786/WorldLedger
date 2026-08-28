package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// What a crash leaves behind, and whether the archive can still be used
// afterwards. All three of these were found by auditing for the shape rather
// than by anything failing, and each one is silent: the integrity check
// reported the archive clean in every case below.

// An atomic write creates its temporary in the directory it will be renamed
// into, because a rename across filesystems is not atomic. The index
// enumeration refused every entry it did not recognise, so a crash in that
// window made every read of the archive fail forever with "unexpected entry in
// chunk index" -- while fsck, which only looks at .idx files, said nothing.
func TestATemporaryLeftInTheIndexDoesNotBreakTheArchive(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "one")

	// The residue a killed process leaves, at each level the index enumerates.
	var columns []string
	filepath.WalkDir(filepath.Join(dir, "index"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			columns = append(columns, path)
		}
		return nil
	})
	if len(columns) == 0 {
		t.Fatal("the index has no directories, so this test would prove nothing")
	}
	for _, column := range columns {
		if err := os.WriteFile(filepath.Join(column, ".tmp-1234"), []byte("half a write"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("the archive could not be opened after a crashed write: %v", err)
	}
	manifest, err := reopened.Manifest()
	if err != nil {
		t.Fatalf("the archive could not be read after a crashed write: %v", err)
	}
	if manifest.Observations != 1 {
		t.Errorf("manifest reports %d observation(s), want 1", manifest.Observations)
	}

	// And the residue is gone, rather than stepped over forever: an abandoned
	// object temporary can be tens of megabytes.
	for _, column := range columns {
		if _, err := os.Stat(filepath.Join(column, ".tmp-1234")); !os.IsNotExist(err) {
			t.Errorf("%s still holds the abandoned temporary", column)
		}
	}
}

func TestAnAbandonedObjectTemporaryIsSweptUp(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "objects", "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	stranded := filepath.Join(tmp, "object-999")
	if err := os.WriteFile(stranded, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stranded); !os.IsNotExist(err) {
		t.Error("an object temporary left by a killed import was still there after opening the archive")
	}
}

// The journal used to carry only ids. Once the observations were removed, an id
// no longer related to anything on disk, so a replay found nothing to do,
// skipped the object half, and discarded the journal as finished. The bytes
// somebody had asked to have removed stayed on disk, after the command had
// reported success.
func TestAPurgeInterruptedBeforeItsObjectsIsFinishedOnReopen(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	observation := storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "one")
	var digests []string
	for _, ref := range observation.Components {
		digests = append(digests, ref.Digest)
		if _, err := os.Stat(a.CAS.Path(ref)); err != nil {
			t.Fatalf("the object was not stored to begin with: %v", err)
		}
	}
	if len(digests) == 0 {
		t.Fatal("the sample observation references no object, so this test would prove nothing")
	}

	// The state a crash leaves between removing the observation and removing
	// its objects: the journal is on disk, the observation is not.
	refs, err := a.doomedRefsLocked([]string{observation.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.writePurgeJournal([]string{observation.ID}, refs); err != nil {
		t.Fatal(err)
	}
	if err := a.removeFromIndexLocked(observation); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a.observationPath(observation.ID)); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("the archive could not be opened after an interrupted purge: %v", err)
	}
	for _, ref := range observation.Components {
		if _, err := os.Stat(reopened.CAS.Path(ref)); !os.IsNotExist(err) {
			t.Errorf("object %s survived a purge that had already reported success", ref.Digest)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, purgeDirectory, "pending.json")); !os.IsNotExist(err) {
		t.Error("the journal was left behind after a successful replay")
	}
}

// The journal is the one input to recovery that is a file rather than something
// the archive just computed, and observationPath slices its first two
// characters. A journal holding a one-character id made every Open panic or
// fail forever, with no way to reach fsck to find out why.
func TestAJournalNamingSomethingThatIsNotAnObservationIsRefused(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(dir, purgeDirectory)
	if err := os.MkdirAll(journalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(purgeJournal{IDs: []string{"../../escape"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, "pending.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dir); err == nil {
		t.Fatal("a journal naming something that is not an observation id was replayed")
	}
}

// A journal from before the references were recorded is still a journal. It
// can finish the half it was able to finish, rather than making the archive
// unopenable.
func TestAJournalFromBeforeTheReferencesWereRecordedStillReplays(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	observation := storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "one")

	journalDir := filepath.Join(dir, purgeDirectory)
	if err := os.MkdirAll(journalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal([]string{observation.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, "pending.json"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("an older journal made the archive unopenable: %v", err)
	}
	if report := reopened.Check(); len(report.Errors) != 0 {
		t.Errorf("the archive does not pass its own check after replaying an older journal: %v", report.Errors)
	}
}
