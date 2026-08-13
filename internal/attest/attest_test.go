package attest

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

const observationID = "34fb4e4699f4e2c8c8f4f0b2f7ad5bbd9b3b4b2f1d5a7e9c0f2b4d6a8c0e2f46"

func mustKey(t *testing.T) PrivateKey {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSignedAttestationVerifies(t *testing.T) {
	key := mustKey(t)
	attestation, err := Sign(key, observationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := attestation.Verify(); err != nil {
		t.Fatalf("a freshly signed attestation failed to verify: %v", err)
	}
}

// The signature covers the observation id, which is itself a digest over the
// server, dimension, chunk, instant, protocol, contributor label, and state
// digest. Moving a signature to any other observation must fail.
func TestASignatureCannotBeMovedToAnotherObservation(t *testing.T) {
	key := mustKey(t)
	attestation, err := Sign(key, observationID)
	if err != nil {
		t.Fatal(err)
	}
	attestation.ObservationID = strings.Repeat("a", 64)
	if err := attestation.Verify(); err == nil {
		t.Fatal("a signature verified against an observation it was not made for")
	}
}

func TestATamperedSignatureFails(t *testing.T) {
	key := mustKey(t)
	attestation, err := Sign(key, observationID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(attestation.Signature)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xff
	attestation.Signature = hex.EncodeToString(raw)
	if err := attestation.Verify(); err == nil {
		t.Fatal("a modified signature verified")
	}
}

func TestASignatureFromAnotherKeyFails(t *testing.T) {
	signer := mustKey(t)
	other := mustKey(t)
	attestation, err := Sign(signer, observationID)
	if err != nil {
		t.Fatal(err)
	}
	attestation.PublicKey = other.Public().Encode()
	if err := attestation.Verify(); err == nil {
		t.Fatal("an attestation verified after its key was swapped")
	}
}

// Without a domain separator, a signature made over the bare observation id for
// some other purpose would verify here, and one of these would be usable as the
// other. The separator is what keeps the two apart.
func TestASignatureOverTheBareIdIsNotAnAttestation(t *testing.T) {
	key := mustKey(t)
	bare := ed25519.Sign(key.key, []byte(observationID))
	attestation := Attestation{
		Schema:        Schema,
		ObservationID: observationID,
		PublicKey:     key.Public().Encode(),
		Signature:     hex.EncodeToString(bare),
	}
	if err := attestation.Verify(); err == nil {
		t.Fatal("a signature over the undomained id was accepted as an attestation")
	}
}

func TestKeyEncodingRoundTrips(t *testing.T) {
	key := mustKey(t)
	parsed, err := ParsePrivateKey(key.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Public().Encode() != key.Public().Encode() {
		t.Fatal("a round-tripped private key produced a different public key")
	}
	if _, err := ParsePublicKey("not hex"); err == nil {
		t.Fatal("a non-hex public key was accepted")
	}
	if _, err := ParsePublicKey(hex.EncodeToString([]byte("too short"))); err == nil {
		t.Fatal("an undersized public key was accepted")
	}
}

func identity(t *testing.T, label string, key PrivateKey) Identity {
	t.Helper()
	return Identity{
		Schema:     IdentitySchema,
		Label:      label,
		PublicKey:  key.Public().Encode(),
		DeclaredBy: "reviewer",
		DeclaredAt: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
	}
}

// A valid signature from a key nobody registered is exactly that, and reporting
// it as anything more would turn key generation into an endorsement.
func TestAnUnregisteredKeyIsValidButNotRecognised(t *testing.T) {
	key := mustKey(t)
	attestation, err := Sign(key, observationID)
	if err != nil {
		t.Fatal(err)
	}

	outcome := Evaluate(attestation, nil)
	if !outcome.Valid {
		t.Fatalf("the signature is genuine and should verify: %s", outcome.Reason)
	}
	if outcome.Label != "" {
		t.Fatalf("an unregistered key was given the label %q", outcome.Label)
	}

	recognised := Evaluate(attestation, []Identity{identity(t, "alice", key)})
	if !recognised.Valid || recognised.Label != "alice" {
		t.Fatalf("a registered key was not resolved to its label: %+v", recognised)
	}
}

// Anyone can generate a key and pick a name. The registry is where that is
// judged, so a second key claiming a taken label must be refused rather than
// quietly inheriting the standing of the first.
func TestASecondKeyCannotClaimARegisteredLabel(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	first := mustKey(t)
	if err := store.Register(identity(t, "alice", first)); err != nil {
		t.Fatal(err)
	}

	impostor := mustKey(t)
	err := store.Register(identity(t, "alice", impostor))
	if err == nil {
		t.Fatal("a second key was allowed to register the same label")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("the refusal should say why: %v", err)
	}

	// Re-registering the same key under the same label is not a conflict.
	if err := store.Register(identity(t, "alice", first)); err != nil {
		t.Fatalf("re-registering an identical identity should be accepted: %v", err)
	}
	listed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one identity, got %d", len(listed))
	}
}

func TestReplacingAKeyRequiresRemovingTheOldOneFirst(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	first := mustKey(t)
	if err := store.Register(identity(t, "alice", first)); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Remove(first.Public().Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("removing a registered identity reported nothing removed")
	}
	replacement := mustKey(t)
	if err := store.Register(identity(t, "alice", replacement)); err != nil {
		t.Fatalf("after removal the label should be free: %v", err)
	}
}

func TestStoreKeepsAttestationsPerObservationAndKey(t *testing.T) {
	store := NewStore(t.TempDir())
	first := mustKey(t)
	second := mustKey(t)

	for _, key := range []PrivateKey{first, second} {
		attestation, err := Sign(key, observationID)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(attestation); err != nil {
			t.Fatal(err)
		}
	}

	stored, err := store.For(observationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("two parties vouched for this observation; got %d attestations", len(stored))
	}
	for _, attestation := range stored {
		if err := attestation.Verify(); err != nil {
			t.Fatalf("a stored attestation no longer verifies: %v", err)
		}
	}
}

func TestStoreRefusesAnAttestationThatDoesNotVerify(t *testing.T) {
	store := NewStore(t.TempDir())
	key := mustKey(t)
	attestation, err := Sign(key, observationID)
	if err != nil {
		t.Fatal(err)
	}
	attestation.ObservationID = strings.Repeat("b", 64)
	if err := store.Put(attestation); err == nil {
		t.Fatal("an archive accepted an attestation that does not verify")
	}
}
