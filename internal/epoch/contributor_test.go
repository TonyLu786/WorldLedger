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
