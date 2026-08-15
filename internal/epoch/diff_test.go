package epoch

import (
	"reflect"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func chunkAt(x, z int32) model.ChunkRef {
	return model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: x, Z: z}
}

// observationOf is newObservation for a chunk other than testChunk, which the
// diff needs because it compares a whole dimension rather than one chunk.
func observationOf(t *testing.T, chunk model.ChunkRef, contributor string, observedAt time.Time, state rune) model.Observation {
	t.Helper()
	o := newObservation(t, contributor, observedAt, state)
	o.Chunk = chunk
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	return o
}

func kindOf(t *testing.T, diff Diff, chunk model.ChunkRef) ChunkChange {
	t.Helper()
	for _, change := range diff.Changes {
		if change.Chunk.X == chunk.X && change.Chunk.Z == chunk.Z {
			return change
		}
	}
	t.Fatalf("no change reported for chunk %d,%d", chunk.X, chunk.Z)
	return ChunkChange{}
}

// The distinction this whole comparison exists for. Both chunks report the same
// state at both moments. One was looked at again and found the same; the other
// was never looked at again and is reporting a state from before the interval
// even began. Calling both of them unchanged is the mistake.
func TestAStateCarriedForwardIsNotReportedAsUnchanged(t *testing.T) {
	revisited := chunkAt(0, 0)
	abandoned := chunkAt(1, 0)

	inputs := []ChunkInput{
		{Chunk: revisited, Observations: []model.Observation{
			observationOf(t, revisited, "alice", at(10), 'a'),
			observationOf(t, revisited, "alice", at(40), 'a'),
		}},
		{Chunk: abandoned, Observations: []model.Observation{
			observationOf(t, abandoned, "alice", at(10), 'a'),
		}},
	}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)

	if got := kindOf(t, diff, revisited); got.Kind != ChangeUnchanged {
		t.Fatalf("a chunk observed again with the same state = %q; want %q", got.Kind, ChangeUnchanged)
	}
	if got := kindOf(t, diff, abandoned); got.Kind != ChangeNotRevisited {
		t.Fatalf("a chunk nobody revisited = %q; want %q", got.Kind, ChangeNotRevisited)
	}

	// Both have identical digests on both sides. Anything comparing only the two
	// snapshots would have to call them the same thing.
	a, b := kindOf(t, diff, revisited), kindOf(t, diff, abandoned)
	if a.FromDigest != a.ToDigest || b.FromDigest != b.ToDigest || a.FromDigest != b.FromDigest {
		t.Fatal("the test no longer sets up two chunks with identical digests on both sides")
	}

	if diff.Summary.Unchanged != 1 || diff.Summary.NotRevisited != 1 {
		t.Fatalf("summary = %+v; want one unchanged and one not-revisited", diff.Summary)
	}
	if diff.Summary.Covered() != 1 {
		t.Fatalf("covered = %d; want 1, because the archive can only speak for one of the two",
			diff.Summary.Covered())
	}
}

func TestADifferentStateIsReportedAsChanged(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "bob", at(40), 'b'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	change := kindOf(t, diff, chunk)
	if change.Kind != ChangeChanged {
		t.Fatalf("kind = %q; want %q", change.Kind, ChangeChanged)
	}
	if change.FromDigest == change.ToDigest || change.FromDigest == "" || change.ToDigest == "" {
		t.Fatalf("expected two different non-empty digests, got %q and %q", change.FromDigest, change.ToDigest)
	}
	if got := change.Contributors(); !reflect.DeepEqual(got, []string{"bob"}) {
		t.Fatalf("contributors = %v; want [bob], the one who observed inside the interval", got)
	}
}

// A chunk first observed during the interval tells us something new about the
// archive, not about the world: nobody knows what was there before.
func TestAChunkFirstObservedInsideTheIntervalIsNotAChange(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(30), 'a'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	change := kindOf(t, diff, chunk)
	if change.Kind != ChangeFirstSeen {
		t.Fatalf("kind = %q; want %q", change.Kind, ChangeFirstSeen)
	}
	if change.FromDigest != "" {
		t.Fatalf("from_digest = %q; want empty, nothing was observed by then", change.FromDigest)
	}
	if change.Kind.Settled() {
		t.Fatal("first-seen must not count as a settled claim about the world")
	}
}

func TestAChunkObservedOnlyAfterTheIntervalIsNeverSeen(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(90), 'a'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	change := kindOf(t, diff, chunk)
	if change.Kind != ChangeNeverSeen {
		t.Fatalf("kind = %q; want %q", change.Kind, ChangeNeverSeen)
	}
	if len(change.Revisits) != 0 {
		t.Fatalf("revisits = %v; want none", change.Revisits)
	}
}

// An observation exactly at the opening instant is the one the earlier side
// selected. Counting it as a revisit would let a chunk nobody returned to claim
// it had been checked.
func TestAnObservationAtTheOpeningInstantIsNotARevisit(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(20), 'a'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	change := kindOf(t, diff, chunk)
	if change.Kind != ChangeNotRevisited {
		t.Fatalf("kind = %q; want %q", change.Kind, ChangeNotRevisited)
	}
}

// The closing instant is inside the interval, because the later side selects it
// too and it is genuinely a second look.
func TestAnObservationAtTheClosingInstantIsARevisit(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "bob", at(50), 'a'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	change := kindOf(t, diff, chunk)
	if change.Kind != ChangeUnchanged {
		t.Fatalf("kind = %q; want %q", change.Kind, ChangeUnchanged)
	}
}

// Every observation inside the interval is kept, not just the one that decided
// the later state, because who looked is part of what a diff is for.
func TestRevisitsAreReportedOldestFirstWithTheirContributors(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "carol", at(45), 'c'),
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "bob", at(25), 'b'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	change := kindOf(t, diff, chunk)
	if len(change.Revisits) != 2 {
		t.Fatalf("revisits = %d; want 2", len(change.Revisits))
	}
	if !change.Revisits[0].ObservedAt.Equal(at(25)) || !change.Revisits[1].ObservedAt.Equal(at(45)) {
		t.Fatalf("revisits are not oldest first: %v", change.Revisits)
	}
	if got := change.Contributors(); !reflect.DeepEqual(got, []string{"bob", "carol"}) {
		t.Fatalf("contributors = %v; want [bob carol]", got)
	}
	if got := diff.Summary.Contributors; !reflect.DeepEqual(got, []string{"bob", "carol"}) {
		t.Fatalf("summary contributors = %v; want [bob carol], alice observed before the interval", got)
	}
}

// A contributor who reports the same chunk repeatedly is named once.
func TestAContributorIsNamedOncePerChunk(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(25), 'a'),
		observationOf(t, chunk, "alice", at(30), 'a'),
		observationOf(t, chunk, "alice", at(35), 'a'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	if got := kindOf(t, diff, chunk).Contributors(); !reflect.DeepEqual(got, []string{"alice"}) {
		t.Fatalf("contributors = %v; want [alice]", got)
	}
}

func TestChangesAreOrderedByChunkPosition(t *testing.T) {
	var inputs []ChunkInput
	for _, pos := range [][2]int32{{2, 1}, {0, 5}, {2, 0}, {0, 1}} {
		chunk := chunkAt(pos[0], pos[1])
		inputs = append(inputs, ChunkInput{Chunk: chunk, Observations: []model.Observation{
			observationOf(t, chunk, "alice", at(30), 'a'),
		}})
	}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	var got [][2]int32
	for _, change := range diff.Changes {
		got = append(got, [2]int32{change.Chunk.X, change.Chunk.Z})
	}
	want := [][2]int32{{0, 1}, {0, 5}, {2, 0}, {2, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v; want %v", got, want)
	}
}

// The summary has to account for every chunk exactly once, or a reader adding
// the numbers up gets a different total than the one printed.
func TestTheSummaryAccountsForEveryChunk(t *testing.T) {
	build := func(chunk model.ChunkRef, obs ...model.Observation) ChunkInput {
		return ChunkInput{Chunk: chunk, Observations: obs}
	}
	changed, unchanged, stale, fresh, absent :=
		chunkAt(0, 0), chunkAt(1, 0), chunkAt(2, 0), chunkAt(3, 0), chunkAt(4, 0)

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), []ChunkInput{
		build(changed, observationOf(t, changed, "alice", at(10), 'a'), observationOf(t, changed, "bob", at(40), 'b')),
		build(unchanged, observationOf(t, unchanged, "alice", at(10), 'a'), observationOf(t, unchanged, "bob", at(40), 'a')),
		build(stale, observationOf(t, stale, "alice", at(10), 'a')),
		build(fresh, observationOf(t, fresh, "alice", at(30), 'a')),
		build(absent, observationOf(t, absent, "alice", at(90), 'a')),
	})

	s := diff.Summary
	if s.Chunks != 5 {
		t.Fatalf("chunks = %d; want 5", s.Chunks)
	}
	if total := s.Changed + s.Unchanged + s.NotRevisited + s.FirstSeen + s.NeverSeen; total != s.Chunks {
		t.Fatalf("the categories sum to %d but there are %d chunks", total, s.Chunks)
	}
	if s.Changed != 1 || s.Unchanged != 1 || s.NotRevisited != 1 || s.FirstSeen != 1 || s.NeverSeen != 1 {
		t.Fatalf("summary = %+v; want one of each", s)
	}
}

// The status each side carried is kept, so a change between two states that
// were themselves disputed is not presented as though both were settled.
func TestADisputedSideKeepsItsStatus(t *testing.T) {
	chunk := chunkAt(0, 0)
	// alice observes again inside the interval, so her state from before it is
	// no longer what represents her. That leaves two contributors disagreeing at
	// the same instant, which is a conflict rather than one state superseding an
	// older one.
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "alice", at(40), 'b'),
		observationOf(t, chunk, "bob", at(40), 'c'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	change := kindOf(t, diff, chunk)
	if change.ToStatus != StatusConflict {
		t.Fatalf("to_status = %q; want %q, two contributors disagreed at the same instant",
			change.ToStatus, StatusConflict)
	}
	if change.FromStatus != StatusSingleSource {
		t.Fatalf("from_status = %q; want %q", change.FromStatus, StatusSingleSource)
	}
	if change.Kind != ChangeChanged {
		t.Fatalf("kind = %q; want %q", change.Kind, ChangeChanged)
	}
}

// A state that an older observation still represents, far enough back that the
// world changing explains it, is superseded rather than disputed. The diff
// carries that verdict through untouched.
func TestASupersededSideKeepsItsStatus(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "bob", at(40), 'b'),
	}}}

	diff := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	if got := kindOf(t, diff, chunk).ToStatus; got != StatusSuperseded {
		t.Fatalf("to_status = %q; want %q", got, StatusSuperseded)
	}
}

// The two instants are ordered before anything is compared, so a caller who
// passes them the other way round is asking about the same interval and has to
// get the same answer rather than a mirror image of it.
func TestReversedInstantsDescribeTheSameInterval(t *testing.T) {
	chunk := chunkAt(0, 0)
	inputs := []ChunkInput{{Chunk: chunk, Observations: []model.Observation{
		observationOf(t, chunk, "alice", at(10), 'a'),
		observationOf(t, chunk, "bob", at(40), 'b'),
	}}}

	forward := BuildDiff("example.org", "minecraft:overworld", at(20), at(50), inputs)
	backward := BuildDiff("example.org", "minecraft:overworld", at(50), at(20), inputs)

	if !reflect.DeepEqual(forward, backward) {
		t.Fatalf("reversing the instants changed the answer:\n forward  %+v\n backward %+v",
			forward.Summary, backward.Summary)
	}
	if !backward.From.Equal(at(20)) || !backward.To.Equal(at(50)) {
		t.Fatalf("the reported interval was not put in order: %s .. %s", backward.From, backward.To)
	}
	if kindOf(t, forward, chunk).Kind != ChangeChanged {
		t.Fatal("the test no longer covers a chunk that actually changed")
	}
}

func TestOnlyChangedAndUnchangedAreSettled(t *testing.T) {
	settled := map[ChangeKind]bool{
		ChangeChanged:      true,
		ChangeUnchanged:    true,
		ChangeNotRevisited: false,
		ChangeFirstSeen:    false,
		ChangeNeverSeen:    false,
	}
	for kind, want := range settled {
		if got := kind.Settled(); got != want {
			t.Fatalf("%q.Settled() = %v; want %v", kind, got, want)
		}
	}
}
