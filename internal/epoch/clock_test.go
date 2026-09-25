package epoch

import (
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// What the window does when a clock is wrong.
//
// Consulting the window before the vote put a new thing in the hands of
// whoever supplies an observed time. The window is measured back from the most
// recent eligible observation, so a contributor whose clock runs fast moves it,
// and observations that were genuinely simultaneous with theirs fall outside
// and stop deciding. The trust model already says an observed time comes from
// whoever captured it and can be wrong or malicious; this is a new place where
// that matters, and it is recorded here rather than left to be found.
//
// These tests do not assert that it is defended against, because it is not and
// cannot be from in here. They assert what it does, and that the numbers a
// reader would need in order to notice are present.

func TestAFastClockCanTurnAConflictIntoAChange(t *testing.T) {
	// Two contributors look at the same chunk at the same real moment and see
	// different things. That is a conflict and it is the case worth a person's
	// attention.
	honest := []model.Observation{
		newObservation(t, "alice", at(10), 'a'),
		newObservation(t, "bob", at(10).Add(2*time.Second), 'b'),
	}
	if got := SelectChunk(testChunk, honest, at(30)).Status; got != StatusConflict {
		t.Fatalf("two contributors disagreeing two seconds apart = %q; want %q", got, StatusConflict)
	}

	// The same two observations, with bob's clock five minutes fast. Nothing
	// about what either of them saw has changed.
	skewed := []model.Observation{
		newObservation(t, "alice", at(10), 'a'),
		newObservation(t, "bob", at(15).Add(2*time.Second), 'b'),
	}
	selection := SelectChunk(testChunk, skewed, at(30))
	if selection.Status == StatusConflict {
		t.Skip("a five-minute skew no longer moves the window; the rest of this test is about the case where it does")
	}
	if selection.Status != StatusSuperseded {
		t.Fatalf("status = %q; a skew this size makes the disagreement look like a change", selection.Status)
	}

	// And the numbers say an observation was left out of deciding, which is the
	// only signal there is that this happened.
	if selection.Support.Earlier == 0 {
		t.Error("nothing records that the window excluded an observation, so the skew is invisible")
	}
	if selection.Support.Counted != 1 {
		t.Errorf("counted = %d; only bob's observation was inside the window", selection.Support.Counted)
	}
}

// The other direction is already closed and it is worth knowing why. An
// observation from the future cannot move the window at all, because it is not
// eligible: latestPerContributor drops anything after the epoch, and for an
// export of "now" that is every observation a fast clock produces.
func TestAClockAheadOfTheEpochCannotMoveTheWindowAtAll(t *testing.T) {
	observations := []model.Observation{
		newObservation(t, "alice", at(10), 'a'),
		newObservation(t, "bob", at(10).Add(2*time.Second), 'a'),
		newObservation(t, "mallory", at(50), 'b'),
	}

	// An epoch before mallory's claimed time. Their observation is not eligible
	// and the two honest ones decide, as they would have anyway.
	selection := SelectChunk(testChunk, observations, at(20))
	if selection.Status != StatusCorroborated {
		t.Errorf("status = %q; the two eligible observations agree", selection.Status)
	}
	if selection.Support.Observations != 2 {
		t.Errorf("support counts %d; an observation after the epoch is not eligible",
			selection.Support.Observations)
	}
}

// A skew smaller than the window changes nothing, which bounds how much of the
// exposure is real: it takes a clock wrong by more than the simultaneity window
// to move anybody out of it.
func TestASkewInsideTheWindowChangesNothing(t *testing.T) {
	skewed := []model.Observation{
		newObservation(t, "alice", at(10), 'a'),
		newObservation(t, "bob", at(10).Add(10*time.Second), 'b'),
	}
	if got := SelectChunk(testChunk, skewed, at(30)).Status; got != StatusConflict {
		t.Errorf("status = %q; ten seconds is inside the thirty-second window", got)
	}
}
