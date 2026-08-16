package epoch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func snapshotOf(t *testing.T, inputs ...ChunkInput) Snapshot {
	t.Helper()
	return BuildSnapshot("example.org", "minecraft:overworld", at(50), inputs)
}

func oneChunk(t *testing.T, x, z int32, observations ...model.Observation) ChunkInput {
	t.Helper()
	return ChunkInput{Chunk: chunkAt(x, z), Observations: observations}
}

// The root answers one question: would two archives export the same world. Two
// readings that chose the same state at every position have to agree on it,
// whatever else differs about how they got there.

func TestTheSameStateThroughDifferentContributorsHasTheSameRoot(t *testing.T) {
	chunk := chunkAt(0, 0)
	alice := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'))))
	bob := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "bob", at(20), 'a'))))

	if alice.Root != bob.Root {
		t.Fatalf("two archives that would export the same world disagree:\n %s\n %s",
			alice.Root, bob.Root)
	}
	if alice.Chunks[0].Contributors[0] == bob.Chunks[0].Contributors[0] {
		t.Fatal("the test no longer covers two different contributors")
	}
}

// Corroboration changes confidence, not blocks. An archive with two agreeing
// observations exports what an archive with one of them exports.
func TestCorroborationDoesNotChangeTheRoot(t *testing.T) {
	chunk := chunkAt(0, 0)
	single := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'))))
	corroborated := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "bob", at(11), 'a'))))

	if single.Root != corroborated.Root {
		t.Fatal("confidence changed the root, so an archive with more evidence looks like a different world")
	}
	if single.Chunks[0].Status == corroborated.Chunks[0].Status {
		t.Fatalf("the test no longer covers two different statuses (%s)", single.Chunks[0].Status)
	}
}

func TestADifferentStateChangesTheRoot(t *testing.T) {
	chunk := chunkAt(0, 0)
	one := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0, observationOf(t, chunk, "alice", at(10), 'a'))))
	other := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0, observationOf(t, chunk, "alice", at(10), 'b'))))
	if one.Root == other.Root {
		t.Fatal("two different worlds share a root")
	}
}

// A chunk nobody observed is an answer, not an absence, so it has to be
// digested as one.
func TestAnUnobservedChunkIsNotTheSameAsAnObservedOne(t *testing.T) {
	chunk := chunkAt(0, 0)
	unknown := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(90), 'a'))))
	known := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'))))

	if unknown.Chunks[0].StateDigest != "" {
		t.Fatal("a chunk observed only after the instant reported a state")
	}
	if unknown.Root == known.Root {
		t.Fatal("an archive that has never seen a chunk matches one that has")
	}
}

// The same archive read at two moments is two different worlds.
func TestTheInstantIsPartOfTheRoot(t *testing.T) {
	chunk := chunkAt(0, 0)
	input := oneChunk(t, 0, 0, observationOf(t, chunk, "alice", at(10), 'a'))
	early := BuildManifest(BuildSnapshot("example.org", "minecraft:overworld", at(20), []ChunkInput{input}))
	late := BuildManifest(BuildSnapshot("example.org", "minecraft:overworld", at(50), []ChunkInput{input}))
	if early.Root == late.Root {
		t.Fatal("two instants produced one root")
	}
}

// Import order must not reach the bytes.
func TestChunkOrderDoesNotChangeTheRoot(t *testing.T) {
	a, b := chunkAt(2, 1), chunkAt(0, 5)
	forward := BuildManifest(snapshotOf(t,
		oneChunk(t, 2, 1, observationOf(t, a, "alice", at(10), 'a')),
		oneChunk(t, 0, 5, observationOf(t, b, "alice", at(10), 'b'))))
	backward := BuildManifest(snapshotOf(t,
		oneChunk(t, 0, 5, observationOf(t, b, "alice", at(10), 'b')),
		oneChunk(t, 2, 1, observationOf(t, a, "alice", at(10), 'a'))))
	if forward.Root != backward.Root {
		t.Fatal("the order chunks were enumerated in reached the root")
	}
	if forward.Chunks[0].X != 0 || backward.Chunks[0].X != 0 {
		t.Fatal("chunks were not sorted into position order")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	chunk := chunkAt(0, 0)
	manifest := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'))))
	path := filepath.Join(t.TempDir(), "epoch.json")
	if err := manifest.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Root != manifest.Root {
		t.Fatalf("root changed across a round trip: %s then %s", manifest.Root, loaded.Root)
	}
}

// A file whose recorded root disagrees with its own entries has been edited or
// truncated. Comparing against it would compare against something that never
// existed, so it is refused rather than recomputed silently.
func TestALoadedManifestWhoseRootDoesNotMatchIsRefused(t *testing.T) {
	chunk := chunkAt(0, 0)
	manifest := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'))))
	path := filepath.Join(t.TempDir(), "epoch.json")
	if err := manifest.Save(path); err != nil {
		t.Fatal(err)
	}
	tamperFile(t, path, manifest.Chunks[0].StateDigest, strings.Repeat("f", 64))

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("an edited manifest was accepted")
	} else if !strings.Contains(err.Error(), "does not match its entries") {
		t.Fatalf("err = %v", err)
	}
}

func TestComparingTheSameWorldReportsNoDifference(t *testing.T) {
	chunk := chunkAt(0, 0)
	one := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0, observationOf(t, chunk, "alice", at(10), 'a'))))
	other := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0, observationOf(t, chunk, "bob", at(20), 'a'))))

	comparison, err := CompareManifests(one, other)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.SameWorld || len(comparison.Mismatched) != 0 {
		t.Fatalf("%+v", comparison)
	}
}

// Agreeing on the blocks and disagreeing on the evidence is a real and separate
// thing to report: the exports are identical and one archive knows more.
func TestConfidenceDifferencesAreReportedApartFromTheWorld(t *testing.T) {
	chunk := chunkAt(0, 0)
	single := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'))))
	corroborated := BuildManifest(snapshotOf(t, oneChunk(t, 0, 0,
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "bob", at(11), 'a'))))

	comparison, err := CompareManifests(single, corroborated)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.SameWorld {
		t.Fatal("the same blocks were reported as a different world")
	}
	if len(comparison.Confidence) != 1 {
		t.Fatalf("the confidence difference was not reported: %+v", comparison)
	}
	if len(comparison.Mismatched) != 0 {
		t.Fatalf("a confidence difference was reported as a world difference: %+v", comparison)
	}
}

func TestAChunkOnlyOneSideHasIsReportedAsThat(t *testing.T) {
	a, b := chunkAt(0, 0), chunkAt(1, 0)
	mine := BuildManifest(snapshotOf(t,
		oneChunk(t, 0, 0, observationOf(t, a, "alice", at(10), 'a')),
		oneChunk(t, 1, 0, observationOf(t, b, "alice", at(10), 'b'))))
	theirs := BuildManifest(snapshotOf(t,
		oneChunk(t, 0, 0, observationOf(t, a, "alice", at(10), 'a'))))

	comparison, err := CompareManifests(mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.OnlyLocal) != 1 || comparison.OnlyLocal[0].X != 1 {
		t.Fatalf("%+v", comparison)
	}
	if comparison.SameWorld {
		t.Fatal("one archive holding a chunk the other does not is a different world")
	}
}

// Comparing two different places or moments is a mistake, not a finding.
func TestComparingDifferentPlacesOrMomentsIsRefused(t *testing.T) {
	chunk := chunkAt(0, 0)
	input := oneChunk(t, 0, 0, observationOf(t, chunk, "alice", at(10), 'a'))
	base := BuildManifest(BuildSnapshot("example.org", "minecraft:overworld", at(50), []ChunkInput{input}))

	other := BuildManifest(BuildSnapshot("other.net", "minecraft:overworld", at(50), []ChunkInput{input}))
	if _, err := CompareManifests(base, other); err == nil {
		t.Error("two servers compared without complaint")
	}
	later := BuildManifest(BuildSnapshot("example.org", "minecraft:overworld", at(51), []ChunkInput{input}))
	if _, err := CompareManifests(base, later); err == nil {
		t.Error("two instants compared without complaint")
	}
}

// tamperFile edits a saved manifest in place, which is how a truncated or
// hand-corrected file arrives in practice.
func tamperFile(t *testing.T, path, from, to string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), from, to, 1)
	if edited == string(data) {
		t.Fatalf("%q was not present to replace", from)
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
}
