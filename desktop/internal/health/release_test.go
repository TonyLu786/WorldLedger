package health

import (
	"strings"
	"testing"
)

// Not having the pinned release is two different situations and the message
// described one of them.
//
// Somebody who has never played 26.2 needs their launcher. Somebody whose
// launcher has moved them on to a later release needs a later WorldLedger, and
// telling them 26.2 "is not installed. Select it in the launcher and play it
// once" says their setup is incomplete and asks them to go backwards. Every
// player ends up in the second situation, all of them on the day Minecraft
// ships, because a release does not wait for an adapter.

func TestNotHavingTheReleaseNamesThePinRatherThanBlamingTheSetup(t *testing.T) {
	detail := checkRelease([]version{{ID: "26.4"}}).Detail

	if !strings.Contains(detail, "26.4") {
		t.Errorf("what the player actually has is not named: %q", detail)
	}
	for _, needed := range []string{
		"one Minecraft release",
		"is made for " + MinecraftVersion,
		"a later WorldLedger",
	} {
		if !strings.Contains(detail, needed) {
			t.Errorf("the detail does not say %q, so it reads as the player's fault: %q", needed, detail)
		}
	}
	// The launcher route stays, because for somebody who simply has not played
	// it yet, it is the answer. It is no longer the only thing said.
	if !strings.Contains(detail, "Minecraft launcher") {
		t.Errorf("the launcher route was dropped: %q", detail)
	}
	if strings.Index(detail, "a later WorldLedger") > strings.Index(detail, "Minecraft launcher") {
		t.Errorf("the launcher route is offered before the pin is explained: %q", detail)
	}
}

// The version of this build is part of that answer, and a window has no
// --version to read it from.
func TestTheDetailNamesWhichBuildIsSpeaking(t *testing.T) {
	Build = "9.9.9"
	defer func() { Build = "dev" }()

	if detail := checkRelease(nil).Detail; !strings.Contains(detail, "9.9.9") {
		t.Errorf("the build is not named: %q", detail)
	}
}

// A build from source has no stamped version, and inventing one would be worse
// than saying nothing.
func TestAnUnstampedBuildDoesNotInventAVersion(t *testing.T) {
	Build = "dev"
	detail := checkRelease(nil).Detail
	if strings.Contains(detail, "(dev)") || strings.Contains(detail, "()") {
		t.Errorf("an unstamped build put a placeholder on the screen: %q", detail)
	}
	if !strings.Contains(detail, "this one is made for") {
		t.Errorf("an unstamped build lost the sentence entirely: %q", detail)
	}
}

// Having it is the ordinary case and must stay one line.
func TestHavingTheReleaseSaysSoAndNothingElse(t *testing.T) {
	check := checkRelease([]version{{ID: MinecraftVersion}})
	if check.State != OK {
		t.Errorf("state = %q, want %q", check.State, OK)
	}
	if check.Detail != "installed" {
		t.Errorf("detail = %q; the ordinary case should not lecture", check.Detail)
	}
}
