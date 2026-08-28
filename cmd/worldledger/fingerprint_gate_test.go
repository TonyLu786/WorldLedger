package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

// The cross-platform gate is this comparison and nothing else.
//
// CI captures the same pinned world on Linux, fingerprints it, and runs
// `fingerprint --file ... --compare ...` against the reference committed from a
// Windows run. If that command ever stops turning a disagreement into a failure,
// the gate reports the divergence in its output and the build goes green, which
// is worse than having no gate: the project's claim that two platforms
// canonicalize identically would be resting on a step that cannot say no.
//
// internal/archive tests the comparison itself thoroughly. What was untested is
// the half that decides the exit status, so that is what these are about.

func fingerprintFile(t *testing.T, fingerprint archive.Fingerprint) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fingerprint.txt")
	handle, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := fingerprint.WriteText(handle); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func stateAt(server string, x, z int32, digest string) archive.FingerprintState {
	return archive.FingerprintState{
		Server: server, Dimension: "minecraft:overworld", X: x, Z: z, Digest: digest,
	}
}

// built produces a fingerprint whose root matches its entries, the way one
// written by the tool does. A file whose root does not match is refused before
// any comparison happens, which is a different check.
func built(t *testing.T, states ...archive.FingerprintState) archive.Fingerprint {
	t.Helper()
	fingerprint := archive.Fingerprint{Schema: archive.FingerprintSchema, States: states}
	path := filepath.Join(t.TempDir(), "seed.txt")
	handle, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := fingerprint.WriteText(handle); err != nil {
		t.Fatal(err)
	}
	handle.Close()

	// Reading it back is what fills in the root, so the value under test is the
	// one the tool itself would compute rather than one this test invented.
	reopened, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	parsed, err := archive.ParseFingerprint(reopened)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestTwoPlatformsDisagreeingAboutAChunkFailsTheGate(t *testing.T) {
	ours := built(t, stateAt("s", 0, 0, strings.Repeat("a", 64)))
	theirs := built(t, stateAt("s", 0, 0, strings.Repeat("b", 64)))

	err := reportFingerprintComparison(ours, fingerprintFile(t, theirs))
	if err == nil {
		t.Fatal("a chunk the two captures disagree about was reported and the command still succeeded")
	}
	if !strings.Contains(err.Error(), "disagree") {
		t.Errorf("the failure does not say what happened: %v", err)
	}
}

// Two captures with nothing in common cannot be evidence that they agree, and a
// gate that passes on them would go green for a run that captured a different
// world entirely.
func TestCapturesWithNoChunkInCommonFailTheGate(t *testing.T) {
	ours := built(t, stateAt("s", 0, 0, strings.Repeat("a", 64)))
	theirs := built(t, stateAt("s", 99, 99, strings.Repeat("a", 64)))

	if err := reportFingerprintComparison(ours, fingerprintFile(t, theirs)); err == nil {
		t.Fatal("two captures sharing no chunk compared clean")
	}
}

// The case that must stay quiet, so the gate is not one that fails on
// everything and therefore says nothing.
func TestAgreementPassesTheGate(t *testing.T) {
	digest := strings.Repeat("c", 64)
	ours := built(t, stateAt("s", 0, 0, digest), stateAt("s", 0, 1, digest))
	theirs := built(t, stateAt("s", 0, 0, digest), stateAt("s", 0, 1, digest))

	if err := reportFingerprintComparison(ours, fingerprintFile(t, theirs)); err != nil {
		t.Fatalf("two identical captures were reported as a disagreement: %v", err)
	}
}

// A chunk one capture saw change and the other did not is a difference in what
// was caught, not in how it was encoded. Failing on it would make the gate
// depend on both sessions being the same length, and it would be red for a
// reason that says nothing about the encoder.
func TestOneCaptureSeeingAnExtraStateDoesNotFailTheGate(t *testing.T) {
	first := strings.Repeat("d", 64)
	second := strings.Repeat("e", 64)
	ours := built(t, stateAt("s", 0, 0, first), stateAt("s", 0, 0, second))
	theirs := built(t, stateAt("s", 0, 0, first))

	if err := reportFingerprintComparison(ours, fingerprintFile(t, theirs)); err != nil {
		t.Fatalf("a state only one capture stayed long enough to see was called a disagreement: %v", err)
	}
}
