package epoch

import (
	"reflect"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// Corroboration is this project's central claim about confidence, and it used
// to count labels instead of people. One contributor writing their own name two
// ways was two independent witnesses, and a chunk nobody else had ever seen
// came out corroborated.
//
// redact had already decided the other way, on the reasoning that a label
// differing only in capitalisation must not be allowed to survive a withdrawal
// of consent. Both cannot be right about whether two labels are one person.

func TestOneContributorCannotCorroborateThemselvesByChangingCase(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "Alice", at(10), 'a'),
		newObservation(t, "alice", at(20), 'a'),
	}

	selection := SelectChunk(testChunk, observations, at(30))
	if selection.Status != StatusSingleSource {
		t.Errorf("status = %q; want %q", selection.Status, StatusSingleSource)
	}
	if want := []string{"alice"}; !reflect.DeepEqual(selection.Contributors, want) {
		t.Errorf("contributors = %v; want %v", selection.Contributors, want)
	}
}

func TestSurroundingSpaceIsNotAContributor(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "bob", at(10), 'b'),
		newObservation(t, " bob ", at(20), 'b'),
	}

	if selection := SelectChunk(testChunk, observations, at(30)); selection.Status != StatusSingleSource {
		t.Errorf("status = %q; want %q", selection.Status, StatusSingleSource)
	}
}

// The other half. Folding must not merge two people who genuinely differ.
func TestTwoContributorsStillCorroborate(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "Alice", at(10), 'a'),
		newObservation(t, "bob", at(20), 'a'),
	}

	selection := SelectChunk(testChunk, observations, at(30))
	if selection.Status != StatusCorroborated {
		t.Errorf("status = %q; want %q", selection.Status, StatusCorroborated)
	}
	if want := []string{"alice", "bob"}; !reflect.DeepEqual(selection.Contributors, want) {
		t.Errorf("contributors = %v; want %v", selection.Contributors, want)
	}
}

// The label somebody typed is still on the record. Only the counting is folded,
// so attribution shows what they wrote and confidence counts who they are.
func TestTheObservationKeepsTheLabelAsItWasWritten(t *testing.T) {
	observations := []model.Observation{newObservation(t, "Alice", at(10), 'a')}

	selection := SelectChunk(testChunk, observations, at(30))
	if selection.Selected == nil {
		t.Fatal("nothing was selected")
	}
	if got := selection.Selected.Source.Contributor; got != "Alice" {
		t.Errorf("the stored label became %q; it should still read %q", got, "Alice")
	}
}

// The case ADR 0003 was written from, kept as the thing that must not come
// back. Four contributors last looked between twenty and sixty minutes ago;
// two watched the chunk change a minute before the epoch.
func TestAnOldMajorityNoLongerOutvotesAFreshChange(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "ann", at(0), 'a'),
		newObservation(t, "bob", at(15), 'a'),
		newObservation(t, "cat", at(30), 'a'),
		newObservation(t, "dan", at(40), 'a'),
		newObservation(t, "eve", at(59), 'b'),
		newObservation(t, "fay", at(59), 'b'),
	}

	selection := SelectChunk(testChunk, observations, at(60))

	// What changed is which state is being talked about, not whether the word
	// is used. Two people did see this one, a minute ago, independently, so
	// corroborated is exactly the right thing to say about it. What the old
	// rule said it about was the state the other four had not seen since.
	stale, fresh := observations[0].StateDigest, observations[4].StateDigest
	if selection.Selected.StateDigest != fresh {
		t.Errorf("selected %s; the two most recent observations both say %s",
			selection.Selected.StateDigest[:6], fresh[:6])
	}
	if selection.Selected.StateDigest == stale {
		t.Error("the chunk was written as the state nobody had seen for twenty minutes")
	}
	if selection.Status != StatusCorroborated {
		t.Errorf("status = %q; two contributors saw this state a minute apart", selection.Status)
	}
	if len(selection.Contributors) != 2 {
		t.Errorf("contributors = %v; only the two who saw the selected state may count",
			selection.Contributors)
	}
	// The four are not discarded. They are what the chunk used to hold.
	if len(selection.Rejected) != 1 || len(selection.Rejected[0].Contributors) != 4 {
		t.Errorf("the earlier state was not preserved with everyone who saw it: %#v", selection.Rejected)
	}
}

// Two states three seconds apart is still a conflict, and an old observation
// agreeing with one of them no longer decides it.
func TestAStaleObservationNoLongerSettlesASimultaneousConflict(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "eve", at(0), 'a'),
		newObservation(t, "ann", at(40), 'a'),
		newObservation(t, "bob", at(40), 'a'),
		newObservation(t, "cat", at(40), 'b'),
		newObservation(t, "dan", at(40), 'b'),
	}

	if selection := SelectChunk(testChunk, observations, at(50)); selection.Status != StatusConflict {
		t.Errorf("status = %q; four contributors split two against two at the same moment is a conflict",
			selection.Status)
	}
}
