package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/epoch"
	"github.com/worldledger/worldledger-mc/internal/model"
	"github.com/worldledger/worldledger-mc/internal/redact"
)

// diff reports who observed what and when, which is exactly the kind of output
// a withheld observation must not reach. It gets its data through the same
// filter export and coverage use; this proves that rather than assuming it,
// because the filter is one function call away from being left out of the next
// command somebody adds.

func observationForDiff(t *testing.T, contributor string, x, z int32, at time.Time, state string) model.Observation {
	t.Helper()
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: x, Z: z},
		ObservedAt: at,
		Protocol:   "java/test-v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{
			"mcjava.shape": {Algorithm: "sha256", Digest: strings.Repeat(state, 64), Size: 53},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestDiffDoesNotReportWithheldObservations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	a, err := archive.Init(root)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	for _, o := range []model.Observation{
		observationForDiff(t, "alice", 0, 0, base, "a"),
		observationForDiff(t, "alice", 0, 0, base.Add(20*time.Minute), "b"),
		observationForDiff(t, "mallory", 1, 0, base, "c"),
		observationForDiff(t, "mallory", 1, 0, base.Add(20*time.Minute), "d"),
	} {
		if err := a.AddObservation(o); err != nil {
			t.Fatal(err)
		}
	}

	// Without a redaction both chunks are comparable, so the assertion below
	// tests the filter rather than an archive that never had the data.
	before, err := dimensionInputs(a, "example.org", "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if got := epoch.BuildDiff("example.org", "minecraft:overworld",
		base.Add(10*time.Minute), base.Add(30*time.Minute), before); got.Summary.Changed != 2 {
		t.Fatalf("changed = %d before any redaction; want 2", got.Summary.Changed)
	}

	if _, err := redact.NewStore(a.Root).Declare(redact.Redaction{
		Server:      "example.org",
		Contributor: "mallory",
		Reason:      "withdrawn by the contributor",
		DeclaredBy:  "operator",
	}); err != nil {
		t.Fatal(err)
	}

	inputs, err := dimensionInputs(a, "example.org", "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	diff := epoch.BuildDiff("example.org", "minecraft:overworld",
		base.Add(10*time.Minute), base.Add(30*time.Minute), inputs)

	if diff.Summary.Changed != 1 {
		t.Fatalf("changed = %d after redacting one contributor; want 1", diff.Summary.Changed)
	}
	for _, name := range diff.Summary.Contributors {
		if name == "mallory" {
			t.Fatal("a withheld contributor was named in the diff summary")
		}
	}

	// Nothing about the withheld observations may survive anywhere in the
	// output, including the digests and observation ids the JSON form carries.
	encoded, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"mallory", strings.Repeat("c", 64), strings.Repeat("d", 64)} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("the diff carries %q, which belongs to a withheld observation", forbidden)
		}
	}
}
