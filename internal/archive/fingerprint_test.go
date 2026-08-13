package archive

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// observationOf separates content from identity: the component digest is chosen
// by the caller and nothing else about the observation influences it. The tests
// below depend on being able to vary who observed and when while holding the
// observed bytes fixed.
func observationOf(t *testing.T, server, dimension string, x, z int32, contributor string, minute int, content byte) model.Observation {
	t.Helper()
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: server, Dimension: dimension, X: x, Z: z},
		ObservedAt: time.Date(2026, 8, 9, 12, minute, 0, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{
			"mcjava.shape": {Algorithm: "sha256", Digest: repeatHex(content), Size: 16},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	return o
}

// This is the property the cross-platform gate rests on. Two machines capturing
// the same world produce different observation identities by construction,
// because an identity carries the instant and the contributor. If a fingerprint
// moved with those, it could never show agreement between platforms and the
// comparison would be worthless.
func TestFingerprintIgnoresWhoObservedAndWhen(t *testing.T) {
	first := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "windows-runner", 1, 'a'),
		observationOf(t, "s", "minecraft:overworld", 1, 0, "windows-runner", 2, 'b'),
	)
	// Same observed content, different observer, different instants, and the
	// chunks arrive in the opposite order.
	second := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 1, 0, "linux-runner", 47, 'b'),
		observationOf(t, "s", "minecraft:overworld", 0, 0, "linux-runner", 59, 'a'),
	)

	firstFingerprint, err := first.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := second.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	if firstFingerprint.Root != secondFingerprint.Root {
		t.Fatalf("fingerprints differ for identical content:\n%s\n%s",
			firstFingerprint.Root, secondFingerprint.Root)
	}
	comparison := CompareFingerprints(firstFingerprint, secondFingerprint)
	if len(comparison.Differences) != 0 {
		t.Fatalf("expected no differences, got %v", comparison.Differences)
	}
	if comparison.Shared != 2 {
		t.Fatalf("expected both chunks compared, got %d shared", comparison.Shared)
	}

	// The manifest must still disagree, which is what makes it the wrong tool
	// for this comparison and this type necessary.
	firstManifest, err := first.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := second.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if firstManifest.Root == secondManifest.Root {
		t.Fatal("manifest roots matched; this test no longer demonstrates why a fingerprint is needed")
	}
}

func TestFingerprintDetectsDifferentContent(t *testing.T) {
	first := archiveWith(t, observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'))
	second := archiveWith(t, observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'c'))

	a, err := first.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	if a.Root == b.Root {
		t.Fatal("fingerprints matched despite different component bytes")
	}

	comparison := CompareFingerprints(a, b)
	content := comparison.ContentDifferences()
	if len(content) != 1 {
		t.Fatalf("expected exactly one content difference, got %v", comparison.Differences)
	}
	if content[0].X != 0 || content[0].Z != 0 {
		t.Fatalf("difference reported at the wrong chunk: %+v", content[0])
	}
	if comparison.Shared != 1 {
		t.Fatalf("both captures saw chunk (0,0), so it should count as shared; got %d", comparison.Shared)
	}
}

// Taken from real capture data. Two sessions against the same server both saw
// chunk (0,0), but only the longer one stayed for the change applied to it. The
// state they share is byte-identical, so this is a difference in what was
// caught rather than in how it was encoded, and calling it a content defect
// would be wrong.
func TestCompareSeparatesAMissedChangeFromADisagreement(t *testing.T) {
	full := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'),
		observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 2, 'b'),
	)
	partial := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "bob", 5, 'a'),
	)

	fullFingerprint, err := full.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	partialFingerprint, err := partial.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	comparison := CompareFingerprints(fullFingerprint, partialFingerprint)
	if got := len(comparison.ContentDifferences()); got != 0 {
		t.Fatalf("a missed change is not a content defect; got %d content differences: %v",
			got, comparison.Differences)
	}
	if len(comparison.Differences) != 1 || comparison.Differences[0].Kind != FingerprintStatesDifference {
		t.Fatalf("expected one states difference, got %v", comparison.Differences)
	}
}

// A capture that simply ran longer covers more ground. That is a property of
// the session, not of the encoding, and reporting it as a defect would bury the
// disagreements that are real.
func TestCompareSeparatesCoverageFromContent(t *testing.T) {
	short := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'),
	)
	long := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "bob", 9, 'a'),
		observationOf(t, "s", "minecraft:overworld", 1, 0, "bob", 9, 'b'),
		observationOf(t, "s", "minecraft:overworld", 2, 0, "bob", 9, 'c'),
	)

	shortFingerprint, err := short.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	longFingerprint, err := long.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	comparison := CompareFingerprints(shortFingerprint, longFingerprint)
	if got := len(comparison.ContentDifferences()); got != 0 {
		t.Fatalf("the shared chunk holds identical bytes, so there is no content difference; got %d", got)
	}
	if comparison.Shared != 1 {
		t.Fatalf("expected 1 shared chunk, got %d", comparison.Shared)
	}
	if len(comparison.Differences) != 2 {
		t.Fatalf("expected the two uncovered chunks reported, got %v", comparison.Differences)
	}
	for _, difference := range comparison.Differences {
		if difference.Kind != FingerprintCoverageDifference {
			t.Fatalf("expected coverage differences only, got %+v", difference)
		}
	}
}

func TestFingerprintKeepsEveryDistinctStateOfAChunk(t *testing.T) {
	// A chunk that changed during a session was observed in two states. Both
	// belong in the fingerprint: collapsing them would hide a real difference in
	// what one platform captured.
	a := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'),
		observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 2, 'b'),
	)
	fingerprint, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fingerprint.States) != 2 {
		t.Fatalf("expected 2 distinct states, got %d", len(fingerprint.States))
	}
}

func TestFingerprintTextRoundTrips(t *testing.T) {
	a := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'),
		observationOf(t, "s", "minecraft:the_nether", -3, 7, "bob", 2, 'b'),
	)
	original, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	var buffer bytes.Buffer
	if err := original.WriteText(&buffer); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFingerprint(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Root != original.Root {
		t.Fatalf("root changed across the text form: %s then %s", original.Root, parsed.Root)
	}
	if len(parsed.States) != len(original.States) {
		t.Fatalf("state count changed: %d then %d", len(original.States), len(parsed.States))
	}
}

func TestParseFingerprintRejectsATamperedRoot(t *testing.T) {
	a := archiveWith(t, observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'))
	fingerprint, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := fingerprint.WriteText(&buffer); err != nil {
		t.Fatal(err)
	}

	// Someone edits a committed reference to make a failing comparison pass.
	tampered := strings.Replace(buffer.String(), "root "+fingerprint.Root, "root "+repeatHex('0'), 1)
	if _, err := ParseFingerprint(strings.NewReader(tampered)); err == nil {
		t.Fatal("a fingerprint whose root disagrees with its entries was accepted")
	}
}

func TestFingerprintFiltersByServer(t *testing.T) {
	a := archiveWith(t,
		observationOf(t, "wanted", "minecraft:overworld", 0, 0, "alice", 1, 'a'),
		observationOf(t, "other", "minecraft:overworld", 0, 0, "alice", 2, 'b'),
	)
	fingerprint, err := a.Fingerprint("wanted")
	if err != nil {
		t.Fatal(err)
	}
	if len(fingerprint.States) != 1 {
		t.Fatalf("expected only the requested server, got %d states", len(fingerprint.States))
	}
	if fingerprint.States[0].Server != "wanted" {
		t.Fatalf("got server %q", fingerprint.States[0].Server)
	}
}

// Two mirrors decide what to send each other from digests alone, without
// either opening the other's archive.
func TestNegotiateWorksOutTheTransferBothWays(t *testing.T) {
	mine := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'),
		observationOf(t, "s", "minecraft:overworld", 1, 0, "alice", 2, 'b'),
	)
	theirs := archiveWith(t,
		observationOf(t, "s", "minecraft:overworld", 1, 0, "bob", 3, 'b'),
		observationOf(t, "s", "minecraft:overworld", 2, 0, "bob", 4, 'c'),
	)

	local, err := mine.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	remote, err := theirs.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	negotiation := Negotiate(local, remote)
	if len(negotiation.Want) != 1 || negotiation.Want[0].Digest != repeatHex('c') {
		t.Fatalf("expected to want only the object we lack, got %v", negotiation.Want)
	}
	if len(negotiation.Offer) != 1 || negotiation.Offer[0].Digest != repeatHex('a') {
		t.Fatalf("expected to offer only the object they lack, got %v", negotiation.Offer)
	}
	if negotiation.Shared != 1 {
		t.Fatalf("both hold one object in common, got %d", negotiation.Shared)
	}
}

func TestNegotiateWithAnIdenticalMirrorTransfersNothing(t *testing.T) {
	a := archiveWith(t, observationOf(t, "s", "minecraft:overworld", 0, 0, "alice", 1, 'a'))
	fingerprint, err := a.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	negotiation := Negotiate(fingerprint, fingerprint)
	if len(negotiation.Want) != 0 || len(negotiation.Offer) != 0 {
		t.Fatalf("two identical mirrors have nothing to exchange, got want %d offer %d",
			len(negotiation.Want), len(negotiation.Offer))
	}
}
