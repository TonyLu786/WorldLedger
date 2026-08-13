package transfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/model"
)

func newArchive(t *testing.T) archive.Archive {
	t.Helper()
	a, err := archive.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func add(t *testing.T, a archive.Archive, x, z int32, contributor string, minute int, content string) model.Observation {
	t.Helper()
	ref, err := a.CAS.Put(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: "s", Dimension: "minecraft:overworld", X: x, Z: z},
		ObservedAt: time.Date(2026, 8, 9, 12, minute, 0, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{"mcjava.shape": ref},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := a.AddObservation(o); err != nil {
		t.Fatal(err)
	}
	return o
}

// The exit criterion for the exchange: two archives that never shared a
// database end up holding the same observations, over a directory that could
// have arrived on a memory stick.
func TestTwoArchivesConvergeThroughABundle(t *testing.T) {
	mine := newArchive(t)
	add(t, mine, 0, 0, "alice", 1, "chunk zero")
	add(t, mine, 1, 0, "alice", 2, "chunk one")

	theirs := newArchive(t)
	add(t, theirs, 1, 0, "bob", 3, "chunk one")

	theirFingerprint, err := theirs.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "bundle")
	sent, err := Send(mine, theirFingerprint, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	// Only the object they lack travels; the one they already hold does not.
	if sent.Objects != 1 {
		t.Fatalf("expected one object sent, got %d", sent.Objects)
	}

	if _, err := Receive(theirs, out); err != nil {
		t.Fatal(err)
	}

	// Convergence needs both directions. Theirs holds bob's record, which mine
	// has never seen, and no amount of sending one way fixes that.
	mineFingerprintBefore, err := mine.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	back := filepath.Join(t.TempDir(), "back")
	if _, err := Send(theirs, mineFingerprintBefore, nil, back); err != nil {
		t.Fatal(err)
	}
	if _, err := Receive(mine, back); err != nil {
		t.Fatal(err)
	}

	if report := theirs.Check(); len(report.Errors) != 0 {
		t.Fatalf("the receiving archive failed its own check: %v", report.Errors)
	}

	// Both now hold the object that was only on one side.
	mineFingerprint, err := mine.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := theirs.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	negotiation := archive.Negotiate(mineFingerprint, updated)
	if len(negotiation.Offer) != 0 {
		t.Fatalf("after the exchange there should be nothing left to send, got %d", len(negotiation.Offer))
	}

	// Objects agreeing is not enough. Two mirrors that hold the same bytes and
	// disagree about who observed what are not the same archive, and the
	// manifest is what notices.
	mineManifest, err := mine.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	theirManifest, err := theirs.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if differences := archive.Compare(mineManifest, theirManifest); len(differences) != 0 {
		t.Fatalf("the two archives still disagree after the exchange: %v", differences)
	}
}

func TestReceivingTheSameBundleTwiceChangesNothing(t *testing.T) {
	mine := newArchive(t)
	add(t, mine, 0, 0, "alice", 1, "chunk zero")
	theirs := newArchive(t)

	theirFingerprint, err := theirs.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Send(mine, theirFingerprint, nil, out); err != nil {
		t.Fatal(err)
	}
	if _, err := Receive(theirs, out); err != nil {
		t.Fatal(err)
	}
	second, err := Receive(theirs, out)
	if err != nil {
		t.Fatal(err)
	}
	if second.Observations != 0 || second.AlreadyHeld == 0 {
		t.Fatalf("a repeated import should be a no-op, got %+v", second)
	}
	if report := theirs.Check(); len(report.Errors) != 0 {
		t.Fatalf("archive failed its check after a repeated import: %v", report.Errors)
	}
}

// A bundle arrives from someone the receiver has no reason to trust, so its
// bytes are checked against the digests it declares rather than accepted.
func TestABundleWithTamperedObjectBytesIsRefused(t *testing.T) {
	mine := newArchive(t)
	add(t, mine, 0, 0, "alice", 1, "the original bytes")
	theirs := newArchive(t)

	theirFingerprint, err := theirs.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Send(mine, theirFingerprint, nil, out); err != nil {
		t.Fatal(err)
	}

	var objectPath string
	err = filepath.Walk(filepath.Join(out, "objects"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			objectPath = path
		}
		return err
	})
	if err != nil || objectPath == "" {
		t.Fatalf("could not find the object in the bundle: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte("substituted bytes!"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Receive(theirs, out); err == nil {
		t.Fatal("an archive accepted object bytes that do not match their declared digest")
	}
	if report := theirs.Check(); len(report.Errors) != 0 {
		t.Fatalf("a refused import left the archive damaged: %v", report.Errors)
	}
}

// Renaming an observation, or moving it to another chunk or moment, changes the
// identity it must hash to.
func TestABundleWithATamperedObservationIsRefused(t *testing.T) {
	mine := newArchive(t)
	add(t, mine, 0, 0, "alice", 1, "chunk zero")
	theirs := newArchive(t)

	theirFingerprint, err := theirs.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Send(mine, theirFingerprint, nil, out); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(out, "observations"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "observations", entries[0].Name())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Reattribute it to someone else while leaving the id alone.
	tampered := strings.Replace(string(raw), `"contributor": "alice"`, `"contributor": "mallory"`, 1)
	if tampered == string(raw) {
		t.Fatal("the test did not manage to alter the observation")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Receive(theirs, out); err == nil {
		t.Fatal("an archive accepted an observation whose contents no longer match its id")
	}
}

func TestSendingToAMirrorThatHoldsEverythingProducesNothing(t *testing.T) {
	mine := newArchive(t)
	add(t, mine, 0, 0, "alice", 1, "chunk zero")

	own, err := mine.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bundle")
	sent, err := Send(mine, own, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Objects != 0 {
		t.Fatalf("a mirror holding every object needs none of them, got %d", sent.Objects)
	}

	// Without the peer's manifest the records are included, because a
	// fingerprint cannot say which records they hold. With it, nothing is sent.
	ownManifest, err := mine.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	informed := filepath.Join(t.TempDir(), "informed")
	withManifest, err := Send(mine, own, &ownManifest, informed)
	if err != nil {
		t.Fatal(err)
	}
	if withManifest.Objects != 0 || withManifest.Observations != 0 {
		t.Fatalf("given the peer's manifest, an identical mirror needs nothing, got %+v", withManifest)
	}
}
