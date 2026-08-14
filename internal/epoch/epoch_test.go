package epoch

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

var testChunk = model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: 1, Z: 2}

func at(minute int) time.Time {
	return time.Date(2026, 8, 9, 12, minute, 0, 0, time.UTC)
}

// state selects which canonical state an observation reports; different runes
// produce different state digests.
func newObservation(t *testing.T, contributor string, observedAt time.Time, state rune) model.Observation {
	t.Helper()
	o := model.Observation{
		Chunk:      testChunk,
		ObservedAt: observedAt,
		Protocol:   "java/test-v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{
			"mcjava.shape": {Algorithm: "sha256", Digest: strings.Repeat(string(state), 64), Size: 53},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestSelectChunkReportsUnknownBeforeAnyObservation(t *testing.T) {
	observations := []model.Observation{newObservation(t, "alice", at(30), 'a')}

	selection := SelectChunk(testChunk, observations, at(10))
	if selection.Status != StatusUnknown {
		t.Fatalf("status = %q; want %q", selection.Status, StatusUnknown)
	}
	if selection.Known() || selection.Selected != nil {
		t.Fatal("an epoch before every observation must not select a state")
	}
}

func TestSelectChunkSingleSource(t *testing.T) {
	observations := []model.Observation{newObservation(t, "alice", at(10), 'a')}

	selection := SelectChunk(testChunk, observations, at(20))
	if selection.Status != StatusSingleSource {
		t.Fatalf("status = %q; want %q", selection.Status, StatusSingleSource)
	}
	if len(selection.Rejected) != 0 {
		t.Fatalf("unexpected rejected states: %#v", selection.Rejected)
	}
}

func TestSelectChunkCorroboratesAgreeingContributors(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "alice", at(10), 'a'),
		newObservation(t, "bob", at(11), 'a'),
	}

	selection := SelectChunk(testChunk, observations, at(20))
	if selection.Status != StatusCorroborated {
		t.Fatalf("status = %q; want %q", selection.Status, StatusCorroborated)
	}
	if !reflect.DeepEqual(selection.Contributors, []string{"alice", "bob"}) {
		t.Fatalf("contributors = %#v", selection.Contributors)
	}
}

func TestSelectChunkPrefersCorroboratedStateOverMoreRecentSingleSource(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "alice", at(10), 'a'),
		newObservation(t, "bob", at(11), 'a'),
		newObservation(t, "mallory", at(19), 'b'),
	}

	selection := SelectChunk(testChunk, observations, at(20))
	if selection.Status != StatusCorroborated {
		t.Fatalf("status = %q; want %q", selection.Status, StatusCorroborated)
	}
	if selection.Selected.Source.Contributor == "mallory" {
		t.Fatal("a lone later contributor overrode a corroborated state")
	}
	if len(selection.Rejected) != 1 || selection.Rejected[0].Contributors[0] != "mallory" {
		t.Fatalf("the rejected state was not preserved as evidence: %#v", selection.Rejected)
	}
}

// A Minecraft world is mutable, so two different states minutes apart are the
// expected case, not a disagreement. Labelling those as conflicts would bury the
// few that actually need a human.
func TestDisagreementFarApartInTimeIsSupersededNotConflict(t *testing.T) {
	older := newObservation(t, "alice", at(10), 'a')
	newer := newObservation(t, "bob", at(15), 'b')

	selection := SelectChunk(testChunk, []model.Observation{older, newer}, at(20))
	if selection.Status != StatusSuperseded {
		t.Fatalf("status = %q; want %q for states five minutes apart", selection.Status, StatusSuperseded)
	}
	if selection.Selected.ID != newer.ID {
		t.Fatal("the later state should have been selected")
	}
	// The earlier state is still evidence, not garbage.
	if len(selection.Rejected) != 1 || selection.Rejected[0].StateDigest != older.StateDigest {
		t.Fatalf("the superseded state was not preserved: %#v", selection.Rejected)
	}
}

func TestDisagreementWithinTheWindowIsAConflict(t *testing.T) {
	first := newObservation(t, "alice", at(10), 'a')
	second := newObservation(t, "bob", at(10).Add(5*time.Second), 'b')

	selection := SelectChunkWithin(testChunk, []model.Observation{first, second}, at(20), 30*time.Second)
	if selection.Status != StatusConflict {
		t.Fatalf("status = %q; want %q for states five seconds apart", selection.Status, StatusConflict)
	}
}

// The boundary is inclusive, and widening the window converts a change into a
// conflict, which is the knob's whole purpose.
func TestTheWindowDecidesHowDisagreementIsLabelled(t *testing.T) {
	first := newObservation(t, "alice", at(10), 'a')
	second := newObservation(t, "bob", at(10).Add(20*time.Second), 'b')
	observations := []model.Observation{first, second}

	if got := SelectChunkWithin(testChunk, observations, at(30), 20*time.Second).Status; got != StatusConflict {
		t.Fatalf("at exactly the window, status = %q; want %q", got, StatusConflict)
	}
	if got := SelectChunkWithin(testChunk, observations, at(30), 19*time.Second).Status; got != StatusSuperseded {
		t.Fatalf("just outside the window, status = %q; want %q", got, StatusSuperseded)
	}
}

// Agreement is unaffected by timing: contributors who saw the same state
// corroborate each other however far apart they looked.
func TestCorroborationIsNotAffectedByTheWindow(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "alice", at(0), 'a'),
		newObservation(t, "bob", at(600), 'a'),
	}
	if got := SelectChunkWithin(testChunk, observations, at(700), time.Second).Status; got != StatusCorroborated {
		t.Fatalf("status = %q; want %q", got, StatusCorroborated)
	}
}

func TestSelectChunkFallsBackToLatestWhenNothingIsCorroborated(t *testing.T) {
	older := newObservation(t, "alice", at(10), 'a')
	newer := newObservation(t, "bob", at(15), 'b')

	selection := SelectChunk(testChunk, []model.Observation{older, newer}, at(20))
	if selection.Status != StatusSuperseded {
		t.Fatalf("status = %q; want %q", selection.Status, StatusSuperseded)
	}
	if selection.Selected.ID != newer.ID {
		t.Fatal("fallback did not select the most recent state")
	}
	if len(selection.Rejected) != 1 || selection.Rejected[0].StateDigest != older.StateDigest {
		t.Fatalf("the losing state was not preserved: %#v", selection.Rejected)
	}
}

func TestSelectChunkTreatsTiedCorroborationAsConflict(t *testing.T) {
	// Spaced in seconds so the disagreement is genuinely simultaneous: two pairs
	// of contributors reporting different states at the same moment is the case
	// that deserves the conflict label.
	base := at(10)
	observations := []model.Observation{
		newObservation(t, "alice", base, 'a'),
		newObservation(t, "bob", base.Add(time.Second), 'a'),
		newObservation(t, "carol", base.Add(2*time.Second), 'b'),
		newObservation(t, "dave", base.Add(3*time.Second), 'b'),
	}

	selection := SelectChunk(testChunk, observations, at(20))
	if selection.Status != StatusConflict {
		t.Fatalf("status = %q; want %q - equally corroborated states must not pick a winner by corroboration", selection.Status, StatusConflict)
	}
	if selection.Selected.ObservedAt != base.Add(3*time.Second) {
		t.Fatalf("selected observed_at = %v; want the most recent state", selection.Selected.ObservedAt)
	}
}

func TestSelectChunkIgnoresRepeatedSubmissionsFromOneContributor(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "mallory", at(10), 'b'),
		newObservation(t, "mallory", at(11), 'b'),
		newObservation(t, "mallory", at(12), 'b'),
		newObservation(t, "alice", at(13), 'a'),
	}

	selection := SelectChunk(testChunk, observations, at(20))
	if selection.Status != StatusSuperseded {
		t.Fatalf("status = %q; want %q", selection.Status, StatusSuperseded)
	}
	for _, group := range append([]StateGroup{{Contributors: selection.Contributors}}, selection.Rejected...) {
		if len(group.Contributors) > 1 {
			t.Fatalf("repeated submissions from one contributor became corroboration: %#v", group.Contributors)
		}
	}
}

func TestSelectChunkUsesTheLatestObservationWithinTheSelectedState(t *testing.T) {
	first := newObservation(t, "alice", at(10), 'a')
	second := newObservation(t, "bob", at(14), 'a')

	selection := SelectChunk(testChunk, []model.Observation{first, second}, at(20))
	if selection.Selected.ID != second.ID {
		t.Fatal("selection did not use the most recent observation of the chosen state")
	}
}

func TestSelectChunkIsIndependentOfInputOrder(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "alice", at(10), 'a'),
		newObservation(t, "bob", at(11), 'a'),
		newObservation(t, "carol", at(12), 'b'),
		newObservation(t, "dave", at(13), 'c'),
		newObservation(t, "erin", at(14), 'c'),
	}
	want := SelectChunk(testChunk, observations, at(20))

	generator := rand.New(rand.NewSource(1))
	for attempt := 0; attempt < 32; attempt++ {
		shuffled := append([]model.Observation(nil), observations...)
		generator.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := SelectChunk(testChunk, shuffled, at(20)); !reflect.DeepEqual(got, want) {
			t.Fatalf("selection depends on input order (attempt %d)", attempt)
		}
	}
}

func TestBuildSnapshotSummarizesCoverage(t *testing.T) {
	corroborated := model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: 0, Z: 0}
	conflicted := model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: 0, Z: 1}
	future := model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: 0, Z: 2}

	snapshot := BuildSnapshot("Example.ORG", "Minecraft:Overworld", at(20), []ChunkInput{
		{Chunk: corroborated, Observations: []model.Observation{
			newObservation(t, "alice", at(10), 'a'),
			newObservation(t, "bob", at(11), 'a'),
		}},
		{Chunk: conflicted, Observations: []model.Observation{
			newObservation(t, "alice", at(10), 'a'),
			newObservation(t, "bob", at(11), 'b'),
		}},
		{Chunk: future, Observations: []model.Observation{
			newObservation(t, "alice", at(30), 'a'),
		}},
	})

	// The disagreeing chunk's two states are a minute apart, so the world
	// changing explains it and it is superseded rather than a conflict.
	want := Summary{Chunks: 3, Corroborated: 1, SingleSource: 0, Conflict: 0, Superseded: 1, Unknown: 1}
	if snapshot.Summary != want {
		t.Fatalf("summary = %#v; want %#v", snapshot.Summary, want)
	}
	if snapshot.Server != "example.org" || snapshot.Dimension != "minecraft:overworld" {
		t.Fatalf("snapshot did not normalize its identity: %q / %q", snapshot.Server, snapshot.Dimension)
	}
	if snapshot.Policy != PolicyCorroboratedFirst {
		t.Fatalf("policy = %q", snapshot.Policy)
	}
}
