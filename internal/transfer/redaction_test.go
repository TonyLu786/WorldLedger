package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/attest"
	"github.com/worldledger/worldledger-mc/internal/redact"
)

// A transfer bundle is the only thing this project builds that goes to another
// party. Every other path that assembles something to hand over filters
// withdrawn observations; this one did not, so it was the one way a contributor
// who had withdrawn consent still reached a peer -- record and component bytes,
// with nothing printed about it.

func withdraw(t *testing.T, a archive.Archive, contributor string) {
	t.Helper()
	store := redact.NewStore(a.Root)
	if _, err := store.Declare(redact.Redaction{
		Server:      "s",
		Contributor: contributor,
		Reason:      "contributor withdrew consent",
		DeclaredBy:  "operator",
	}); err != nil {
		t.Fatal(err)
	}
}

func bundleContains(t *testing.T, dir, needle string) bool {
	t.Helper()
	found := false
	filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(body), needle) {
			found = true
		}
		return nil
	})
	return found
}

func TestAWithdrawnContributorDoesNotTravelInABundle(t *testing.T) {
	source := newArchive(t)
	add(t, source, 0, 0, "alice", 1, "ALICE-ONLY-BYTES")
	add(t, source, 1, 1, "bob", 2, "bob-bytes")
	withdraw(t, source, "alice")

	empty, err := newArchive(t).Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "outbound")
	sent, err := Send(source, empty, nil, out)
	if err != nil {
		t.Fatal(err)
	}

	if sent.Observations != 1 {
		t.Errorf("the bundle carries %d observation(s), want only the one not withdrawn", sent.Observations)
	}
	if sent.Withheld != 1 {
		t.Errorf("the send reports %d withheld, want 1", sent.Withheld)
	}
	if bundleContains(t, out, "alice") {
		t.Error("the withdrawn contributor's record is in the bundle")
	}
	// The bytes matter as much as the record: dropping one without the other
	// would be the same disclosure with an extra step.
	if bundleContains(t, out, "ALICE-ONLY-BYTES") {
		t.Error("the withdrawn contributor's component bytes are in the bundle")
	}
	if !bundleContains(t, out, "bob") {
		t.Error("the observation that was not withdrawn did not travel")
	}
}

// Content addressing means an object two contributors both observed is one set
// of bytes, not a copy belonging to either. Withholding one of them must not
// take the other's data with it.
func TestAnObjectAlsoNeededByAKeptObservationStillTravels(t *testing.T) {
	source := newArchive(t)
	add(t, source, 0, 0, "alice", 1, "shared-bytes")
	add(t, source, 0, 0, "bob", 2, "shared-bytes")
	withdraw(t, source, "alice")

	empty, err := newArchive(t).Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "outbound")
	sent, err := Send(source, empty, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Objects != 1 {
		t.Errorf("the bundle carries %d object(s), want the one bob's record needs", sent.Objects)
	}
	if !bundleContains(t, out, "shared-bytes") {
		t.Error("bob's record travelled without the bytes it references")
	}
	if bundleContains(t, out, "alice") {
		t.Error("the withdrawn record travelled")
	}
}

// A bundle that would carry nothing but withheld records is not an empty send
// that happened to find nothing; the difference is worth reporting.
func TestASendThatIsEntirelyWithheldSaysSo(t *testing.T) {
	source := newArchive(t)
	add(t, source, 0, 0, "alice", 1, "alice-bytes")
	withdraw(t, source, "alice")

	empty, err := newArchive(t).Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	sent, err := Send(source, empty, nil, filepath.Join(t.TempDir(), "outbound"))
	if err != nil {
		t.Fatal(err)
	}
	if sent.Observations != 0 {
		t.Errorf("%d observation(s) travelled, want none", sent.Observations)
	}
	if sent.Withheld != 1 {
		t.Errorf("the send reports %d withheld, want 1", sent.Withheld)
	}
}

// A signature is what tells a record somebody made from a record somebody else
// merely wrote naming them. Leaving signatures at home made the exchange the one
// place that distinction disappeared: an honestly transferred record and a
// fabricated one both arrived unsigned and read identically.
func TestASignatureTravelsWithTheRecordItSigns(t *testing.T) {
	source := newArchive(t)
	observation := add(t, source, 0, 0, "alice", 1, "alice-bytes")

	key, err := attest.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := attest.Sign(key, observation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := attest.NewStore(source.Root).Put(signature); err != nil {
		t.Fatal(err)
	}

	destination := newArchive(t)
	theirs, err := destination.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "outbound")
	sent, err := Send(source, theirs, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Attestations != 1 {
		t.Fatalf("the bundle carries %d attestation(s), want 1", sent.Attestations)
	}

	received, err := Receive(destination, out)
	if err != nil {
		t.Fatal(err)
	}
	if received.Attestations != 1 {
		t.Errorf("%d attestation(s) were taken in, want 1", received.Attestations)
	}
	held, err := attest.NewStore(destination.Root).For(observation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 {
		t.Fatalf("the receiving archive holds %d signature(s) for that record, want 1", len(held))
	}
	if err := held[0].Verify(); err != nil {
		t.Errorf("the signature that arrived does not verify: %v", err)
	}
}

// The receiver trusts nothing in the bundle. A signature that does not verify,
// or that signs something the bundle does not carry, is refused rather than
// stored.
func TestAnAttestationForARecordTheBundleDoesNotCarryIsRefused(t *testing.T) {
	source := newArchive(t)
	observation := add(t, source, 0, 0, "alice", 1, "alice-bytes")

	key, err := attest.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := attest.Sign(key, observation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := attest.NewStore(source.Root).Put(signature); err != nil {
		t.Fatal(err)
	}

	destination := newArchive(t)
	theirs, err := destination.Fingerprint("")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "outbound")
	if _, err := Send(source, theirs, nil, out); err != nil {
		t.Fatal(err)
	}

	// A bundle that names a signature for a record it does not carry.
	raw, err := os.ReadFile(filepath.Join(out, "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Observations = nil
	edited, err := json.MarshalIndent(manifest, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "bundle.json"), edited, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Receive(destination, out); err == nil {
		t.Fatal("a signature for a record the bundle does not carry was accepted")
	}
}
