package api

import "testing"

// The one sentence the whole window is steered by. It is worth testing on its
// own because every screen leads with it and because two of its answers are
// judgements rather than lookups: which state is urgent, and which is merely
// incomplete.

func waiting(ready int) *SpoolState { return &SpoolState{Ready: ready} }

func TestWhatIsWaitingComesBeforeEverythingElse(t *testing.T) {
	// Even with nothing set up and nothing declared, an unimported capture is
	// the only state where observed data still exists in one copy.
	status := Status{Spool: waiting(3), Capturing: false}
	if next := nextStep(status); next != "import" {
		t.Errorf("next = %q with captures waiting, want import", next)
	}
}

func TestAMachineWithNothingInPlaceIsSentToSetUp(t *testing.T) {
	if next := nextStep(Status{Capturing: false}); next != "install" {
		t.Errorf("next = %q on a bare machine, want install", next)
	}
}

// The spool folder outlives the mod that made it. Reading the folder's presence
// as "you are recording" was what sent somebody off to play while nothing was
// being kept.
func TestALeftoverCaptureFolderIsNotProofThatCaptureWorks(t *testing.T) {
	status := Status{Spool: &SpoolState{Ready: 0, Imported: 40}, Capturing: false}
	if next := nextStep(status); next != "install" {
		t.Errorf("next = %q with a folder but no mod, want install", next)
	}
}

func TestAReadyMachineWithNothingRecordedIsSentToPlay(t *testing.T) {
	status := Status{Spool: waiting(0), Capturing: true}
	if next := nextStep(status); next != "play" {
		t.Errorf("next = %q with capture working and nothing recorded, want play", next)
	}
}

func TestAnUndeclaredServerBlocksEverythingAfterIt(t *testing.T) {
	status := Status{
		Observations: 12,
		Servers:      []Server{{ID: "a", Declared: true}, {ID: "b", Declared: false}},
		Capturing:    true,
	}
	if next := nextStep(status); next != "declare" {
		t.Errorf("next = %q with one server undeclared, want declare", next)
	}
}

// Somebody who has removed the mod still has an archive they can declare and
// build from, and sending them back to Set up would read as the application
// having forgotten all of it.
func TestWhatCanAlreadyBeDoneComesBeforeSettingUpAgain(t *testing.T) {
	status := Status{
		Observations: 12,
		Servers:      []Server{{ID: "a", Declared: true}},
		Capturing:    false,
	}
	if next := nextStep(status); next != "export" {
		t.Errorf("next = %q with an archive ready and no mod, want export", next)
	}
}
