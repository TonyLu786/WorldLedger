package main

import (
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

func differences(details ...string) []archive.Difference {
	out := make([]archive.Difference, 0, len(details))
	for _, detail := range details {
		out = append(out, archive.Difference{Server: "example.org", Detail: detail})
	}
	return out
}

func explanationOf(details ...string) string {
	return strings.Join(classifyDifferences(differences(details...)).explain("./archive"), "\n")
}

// Two archives that have exchanged in one direction are unequal on purpose, and
// comparing them lists every chunk the other has not got yet. That is the
// working midpoint of an exchange and it used to be printed as a bare list of
// differences, which reads as damage.

func TestOneDirectionOfDifferenceNamesThatOneDirection(t *testing.T) {
	ahead := explanationOf(archive.DetailOnlyLocal, archive.DetailOnlyLocal)
	if !strings.Contains(ahead, "One transfer settles it.") {
		t.Errorf("local-only differences did not say one transfer settles it:\n%s", ahead)
	}
	if !strings.Contains(ahead, "worldledger send") {
		t.Errorf("nothing to run:\n%s", ahead)
	}
	if strings.Contains(ahead, "worldledger fingerprint") {
		t.Errorf("asked for a fingerprint when nothing needs receiving:\n%s", ahead)
	}

	behind := explanationOf(archive.DetailOnlyRemote)
	if !strings.Contains(behind, "sent by them") {
		t.Errorf("remote-only differences did not say who sends:\n%s", behind)
	}
	if !strings.Contains(behind, "worldledger fingerprint") {
		t.Errorf("did not say to hand over a fingerprint:\n%s", behind)
	}
	if strings.Contains(behind, "worldledger send --archive") {
		t.Errorf("told this archive to send when it is the one behind:\n%s", behind)
	}
}

// A chunk both archives hold with different contents cannot be attributed to
// one side: the manifest carries digests, not identities. Claiming each holds
// what the other lacks would be asserting something the data cannot support.
func TestAChunkDifferingOnBothSidesDoesNotClaimWhoHoldsWhat(t *testing.T) {
	text := explanationOf("different observations (local 2, remote 1)")
	if !strings.Contains(text, "without saying which") {
		t.Errorf("the explanation over-claims:\n%s", text)
	}
	for _, want := range []string{"worldledger send", "worldledger fingerprint"} {
		if !strings.Contains(text, want) {
			t.Errorf("both directions should be offered, %q missing:\n%s", want, text)
		}
	}
}

func TestNoDifferencesExplainsNothing(t *testing.T) {
	if lines := classifyDifferences(nil).explain("./archive"); len(lines) != 0 {
		t.Fatalf("two archives that agree were given advice: %v", lines)
	}
}

func TestTheArchivePathIsTheOneTheCallerUsed(t *testing.T) {
	text := strings.Join(
		classifyDifferences(differences(archive.DetailOnlyLocal)).explain("/srv/worldledger"), "\n")
	if !strings.Contains(text, "/srv/worldledger") {
		t.Errorf("the command lines do not use the archive that was compared:\n%s", text)
	}
}

// The classifier reads the two details the archive package writes. Matching
// prose is fragile, so this fails if either constant is reworded without the
// classifier being updated with it.
func TestTheClassifierRecognisesWhatCompareActuallyWrites(t *testing.T) {
	local := classifyDifferences(differences(archive.DetailOnlyLocal))
	if local.localAhead != 1 || local.remoteAhead != 0 || local.both != 0 {
		t.Errorf("%q was not read as local-only: %+v", archive.DetailOnlyLocal, local)
	}
	remote := classifyDifferences(differences(archive.DetailOnlyRemote))
	if remote.remoteAhead != 1 || remote.localAhead != 0 || remote.both != 0 {
		t.Errorf("%q was not read as remote-only: %+v", archive.DetailOnlyRemote, remote)
	}
}
