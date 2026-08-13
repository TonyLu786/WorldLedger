package archive

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func observationAt(t *testing.T, server, dimension string, x, z int32, contributor string, minute int) model.Observation {
	t.Helper()
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: server, Dimension: dimension, X: x, Z: z},
		ObservedAt: time.Date(2026, 8, 9, 12, minute, 0, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{
			"chunk": {Algorithm: "sha256", Digest: repeatHex(byte('a') + byte(minute%6)), Size: int64(minute + 1)},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	return o
}

func repeatHex(r byte) string {
	if r > 'f' {
		r = 'f'
	}
	b := make([]byte, 64)
	for i := range b {
		b[i] = r
	}
	return string(b)
}

func archiveWith(t *testing.T, observations ...model.Observation) Archive {
	t.Helper()
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range observations {
		if err := a.AddObservation(o); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestManifestSummarizesTheArchive(t *testing.T) {
	a := archiveWith(t,
		observationAt(t, "example.org", "minecraft:overworld", 0, 0, "alice", 1),
		observationAt(t, "example.org", "minecraft:overworld", 0, 1, "alice", 2),
		observationAt(t, "example.org", "minecraft:the_nether", 5, 5, "bob", 3),
		observationAt(t, "other.net", "minecraft:overworld", 0, 0, "carol", 4),
	)

	manifest, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != ManifestSchema || manifest.FormatVersion != FormatVersion {
		t.Fatalf("header = %#v", manifest)
	}
	if manifest.Observations != 4 {
		t.Fatalf("observations = %d; want 4", manifest.Observations)
	}
	if len(manifest.Servers) != 2 {
		t.Fatalf("servers = %d; want 2", len(manifest.Servers))
	}
	if manifest.Root == "" {
		t.Fatal("manifest has no root digest")
	}
	if manifest.ObjectBytes <= 0 {
		t.Fatal("manifest reports no object bytes")
	}
}

// A manifest is a fingerprint, so the same archive must always produce the same
// one regardless of the order things were imported in.
func TestManifestIsIndependentOfImportOrder(t *testing.T) {
	forward := archiveWith(t,
		observationAt(t, "example.org", "minecraft:overworld", 0, 0, "alice", 1),
		observationAt(t, "example.org", "minecraft:overworld", 0, 1, "bob", 2),
		observationAt(t, "example.org", "minecraft:overworld", 1, 0, "carol", 3),
	)
	backward := archiveWith(t,
		observationAt(t, "example.org", "minecraft:overworld", 1, 0, "carol", 3),
		observationAt(t, "example.org", "minecraft:overworld", 0, 1, "bob", 2),
		observationAt(t, "example.org", "minecraft:overworld", 0, 0, "alice", 1),
	)

	first, err := forward.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := backward.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root {
		t.Fatal("import order changed the manifest root")
	}
	if len(Compare(first, second)) != 0 {
		t.Fatal("identical archives compared as different")
	}
}

func TestCompareLocalisesAMissingChunk(t *testing.T) {
	shared := []model.Observation{
		observationAt(t, "example.org", "minecraft:overworld", 0, 0, "alice", 1),
		observationAt(t, "example.org", "minecraft:overworld", 0, 1, "bob", 2),
	}
	complete := archiveWith(t, append(append([]model.Observation{}, shared...),
		observationAt(t, "example.org", "minecraft:overworld", 9, 9, "carol", 3))...)
	partial := archiveWith(t, shared...)

	full, err := complete.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	fewer, err := partial.Manifest()
	if err != nil {
		t.Fatal(err)
	}

	differences := Compare(full, fewer)
	if len(differences) != 1 {
		t.Fatalf("differences = %#v; want exactly the missing chunk", differences)
	}
	d := differences[0]
	if d.Chunk == nil || d.Chunk.X != 9 || d.Chunk.Z != 9 {
		t.Fatalf("difference did not localise to chunk (9,9): %#v", d)
	}
}

// The point of per-chunk digests is that a mirror can find where it differs
// without transferring anything.
func TestCompareDetectsDifferentObservationsForTheSameChunk(t *testing.T) {
	mine := archiveWith(t,
		observationAt(t, "example.org", "minecraft:overworld", 0, 0, "alice", 1))
	theirs := archiveWith(t,
		observationAt(t, "example.org", "minecraft:overworld", 0, 0, "alice", 1),
		observationAt(t, "example.org", "minecraft:overworld", 0, 0, "bob", 2))

	local, err := mine.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := theirs.Manifest()
	if err != nil {
		t.Fatal(err)
	}

	differences := Compare(local, remote)
	if len(differences) != 1 || differences[0].Chunk == nil {
		t.Fatalf("differences = %#v", differences)
	}
	if differences[0].Detail == "" {
		t.Fatal("difference carries no detail")
	}
}

func TestCompareReportsAServerOnlyOneSideHas(t *testing.T) {
	mine := archiveWith(t, observationAt(t, "a.example", "minecraft:overworld", 0, 0, "alice", 1))
	theirs := archiveWith(t, observationAt(t, "b.example", "minecraft:overworld", 0, 0, "alice", 1))

	local, _ := mine.Manifest()
	remote, _ := theirs.Manifest()
	differences := Compare(local, remote)
	if len(differences) != 2 {
		t.Fatalf("differences = %#v; want one per server", differences)
	}
}

func TestManifestSurvivesASaveLoadRoundTrip(t *testing.T) {
	a := archiveWith(t, observationAt(t, "example.org", "minecraft:overworld", 0, 0, "alice", 1))
	manifest, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := manifest.Save(path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Root != manifest.Root || reloaded.Observations != manifest.Observations {
		t.Fatal("manifest changed across a save/load round trip")
	}
	if len(Compare(manifest, reloaded)) != 0 {
		t.Fatal("a manifest compared as different from its own reload")
	}
}

func TestManifestOfAnEmptyArchive(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Observations != 0 || len(manifest.Servers) != 0 {
		t.Fatalf("empty archive manifest = %#v", manifest)
	}
	if manifest.Root == "" {
		t.Fatal("even an empty archive needs a root digest to compare against")
	}
}
