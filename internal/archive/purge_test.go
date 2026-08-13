package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// storedObservation writes the component bytes into the archive's object store
// and records an observation that references them.
//
// The other tests in this package are content-agnostic and use fabricated
// digests, which is fine for anything that only reads observation records.
// Purge deletes objects and the archive's own integrity check reads them, so
// these tests need an archive that is genuinely well formed rather than one
// that merely looks right in the index.
func storedObservation(t *testing.T, a Archive, dimension string, x, z int32, contributor string, minute int, content string) model.Observation {
	t.Helper()
	ref, err := a.CAS.Put(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: "s", Dimension: dimension, X: x, Z: z},
		ObservedAt: time.Date(2026, 8, 9, 12, minute, 0, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{"mcjava.shape": ref},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := a.AddObservation(o); err != nil {
		t.Fatal(err)
	}
	return o
}

func emptyArchive(t *testing.T) Archive {
	t.Helper()
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPurgeRemovesObservationsAndLeavesTheArchiveValid(t *testing.T) {
	a := emptyArchive(t)
	alice := storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "alice-only-bytes")
	storedObservation(t, a, "minecraft:overworld", 5, 5, "bob", 2, "bob-only-bytes")

	result, err := a.RemoveObservations([]string{alice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservationsRemoved != 1 {
		t.Fatalf("expected 1 observation removed, got %d", result.ObservationsRemoved)
	}
	if result.ObjectsRemoved != 1 {
		t.Fatalf("alice's object was hers alone, so it should have gone; removed %d", result.ObjectsRemoved)
	}

	report := a.Check()
	if len(report.Errors) != 0 {
		t.Fatalf("archive failed its own check after a purge: %v", report.Errors)
	}
	if report.Observations != 1 {
		t.Fatalf("expected 1 surviving observation, got %d", report.Observations)
	}
}

// The point of the feature, and the part that cannot be quietly dropped.
// Content addressing means two contributors who saw the same state share one
// object, so withdrawing one of them removes their records and not their bytes.
// Measured capture data had two contributors holding 50 of 52 components in
// common, so this is the ordinary case rather than a corner.
func TestPurgeRetainsObjectsAnotherContributorStillNeeds(t *testing.T) {
	a := emptyArchive(t)
	alice := storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "the same observed state")
	bob := storedObservation(t, a, "minecraft:overworld", 0, 0, "bob", 2, "the same observed state")
	if alice.StateDigest != bob.StateDigest {
		t.Fatal("this test needs both contributors to have observed the same state")
	}

	result, err := a.RemoveObservations([]string{alice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservationsRemoved != 1 {
		t.Fatalf("expected alice's observation removed, got %d", result.ObservationsRemoved)
	}
	if result.ObjectsRemoved != 0 {
		t.Fatalf("bob still references those bytes, so nothing should have been deleted; removed %d", result.ObjectsRemoved)
	}
	if len(result.ObjectsRetained) != 1 {
		t.Fatalf("expected the retained object reported, got %v", result.ObjectsRetained)
	}
	retained := result.ObjectsRetained[0]
	if len(retained.Contributors) != 1 || retained.Contributors[0] != "bob" {
		t.Fatalf("expected bob named as the reason it stayed, got %v", retained.Contributors)
	}

	if report := a.Check(); len(report.Errors) != 0 {
		t.Fatalf("archive failed its own check: %v", report.Errors)
	}
	// Bob's observation must still resolve, which it cannot do if the shared
	// object was removed.
	if err := a.CAS.Verify(bob.Components["mcjava.shape"]); err != nil {
		t.Fatalf("the object bob depends on is unusable: %v", err)
	}
}

func TestPurgeDropsAChunkIndexThatNoLongerHoldsAnything(t *testing.T) {
	a := emptyArchive(t)
	only := storedObservation(t, a, "minecraft:overworld", 3, 4, "alice", 1, "sole observation")

	if _, err := a.RemoveObservations([]string{only.ID}); err != nil {
		t.Fatal(err)
	}

	chunks, err := a.Chunks("s", "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	// A chunk listed with no observations would read as observed and empty,
	// which is precisely the confusion the archive exists to avoid.
	for _, chunk := range chunks {
		if chunk.X == 3 && chunk.Z == 4 {
			t.Fatal("the emptied chunk is still listed as one the archive knows about")
		}
	}
	if report := a.Check(); len(report.Errors) != 0 {
		t.Fatalf("archive failed its own check: %v", report.Errors)
	}
}

// An interrupted purge leaves the archive in a state its own check rejects, so
// opening it must finish the job rather than hand back something broken.
func TestAnInterruptedPurgeIsFinishedOnOpen(t *testing.T) {
	a := emptyArchive(t)
	alice := storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "alice bytes")
	storedObservation(t, a, "minecraft:overworld", 1, 1, "bob", 2, "bob bytes")

	// Simulate a crash after the journal was written and before anything was
	// removed.
	journal := filepath.Join(a.Root, purgeDirectory, "pending.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal([]string{alice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(a.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("the purge journal survived recovery")
	}
	report := reopened.Check()
	if len(report.Errors) != 0 {
		t.Fatalf("recovered archive failed its check: %v", report.Errors)
	}
	if report.Observations != 1 {
		t.Fatalf("expected the journalled removal to have been applied, %d observations remain", report.Observations)
	}
}

func TestPurgeIsIdempotent(t *testing.T) {
	a := emptyArchive(t)
	alice := storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "alice bytes")

	if _, err := a.RemoveObservations([]string{alice.ID}); err != nil {
		t.Fatal(err)
	}
	second, err := a.RemoveObservations([]string{alice.ID})
	if err != nil {
		t.Fatalf("removing something already gone must not be an error: %v", err)
	}
	if second.ObservationsRemoved != 0 {
		t.Fatalf("expected nothing removed the second time, got %d", second.ObservationsRemoved)
	}
	if report := a.Check(); len(report.Errors) != 0 {
		t.Fatalf("archive failed its own check: %v", report.Errors)
	}
}
