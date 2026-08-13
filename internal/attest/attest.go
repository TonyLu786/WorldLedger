// Package attest binds an observation to a key that vouches for it.
//
// Until now a contributor is a string an adapter wrote into a bundle. Anyone
// can put any name there, which is adequate while an archive is one person's
// own files and useless the moment two parties exchange observations.
//
// An attestation is a signature over the observation id. That id is already a
// digest over the schema, server, dimension, chunk, instant, protocol,
// contributor label, and state digest, so signing it binds all of them at once:
// a valid signature cannot be moved to a different observation, a different
// chunk, a different moment, or a different claimed contributor.
//
// What this does not do is worth stating as plainly as what it does. A
// signature proves that whoever holds a key asserted something. It does not
// make the assertion true, and it does not stop someone from generating a
// fresh key and calling themselves anything they like. Sybil contributors
// remain a threat the archive cannot solve on its own; see
// docs/trust-model.md. The registry of known identities is where that judgment
// lives, and it is deliberately explicit rather than automatic.
package attest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Schema         = "worldledger.attestation/v1"
	IdentitySchema = "worldledger.identity/v1"

	// signatureDomain keeps these signatures from being valid anywhere else.
	// Without it a signature produced for some other purpose over the same
	// bytes would verify here, and vice versa.
	signatureDomain = "worldledger.observation-attestation/v1"
)

// PrivateKey is a signing key. It is never written into an archive: an archive
// is the thing that gets copied and shared.
type PrivateKey struct {
	key ed25519.PrivateKey
}

type PublicKey struct {
	key ed25519.PublicKey
}

func GenerateKey() (PrivateKey, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PrivateKey{}, err
	}
	return PrivateKey{key: private}, nil
}

func (p PrivateKey) Public() PublicKey {
	return PublicKey{key: p.key.Public().(ed25519.PublicKey)}
}

func (p PrivateKey) Encode() string { return hex.EncodeToString(p.key) }

func ParsePrivateKey(encoded string) (PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return PrivateKey{}, fmt.Errorf("private key is not hex: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return PrivateKey{}, fmt.Errorf("private key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
	}
	return PrivateKey{key: ed25519.PrivateKey(raw)}, nil
}

func (p PublicKey) Encode() string { return hex.EncodeToString(p.key) }

// Fingerprint names a key in output and on disk. The key itself is already
// short enough to print, but a fingerprint stays stable if the encoding ever
// changes and reads better in a list.
func (p PublicKey) Fingerprint() string {
	sum := sha256.Sum256(p.key)
	return hex.EncodeToString(sum[:])
}

func ParsePublicKey(encoded string) (PublicKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return PublicKey{}, fmt.Errorf("public key is not hex: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return PublicKey{}, fmt.Errorf("public key is %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return PublicKey{key: ed25519.PublicKey(raw)}, nil
}

// Attestation is one key's claim to have produced an observation.
type Attestation struct {
	Schema        string    `json:"schema"`
	ObservationID string    `json:"observation_id"`
	PublicKey     string    `json:"public_key"`
	Signature     string    `json:"signature"`
	SignedAt      time.Time `json:"signed_at"`
}

func preimage(observationID string) []byte {
	var buffer bytes.Buffer
	buffer.WriteString(signatureDomain)
	buffer.WriteByte(0)
	buffer.WriteString(strings.TrimSpace(observationID))
	return buffer.Bytes()
}

func Sign(key PrivateKey, observationID string) (Attestation, error) {
	if strings.TrimSpace(observationID) == "" {
		return Attestation{}, errors.New("observation id is required")
	}
	signature := ed25519.Sign(key.key, preimage(observationID))
	return Attestation{
		Schema:        Schema,
		ObservationID: strings.TrimSpace(observationID),
		PublicKey:     key.Public().Encode(),
		Signature:     hex.EncodeToString(signature),
		SignedAt:      time.Now().UTC(),
	}, nil
}

// Verify checks the signature against the key the attestation names.
//
// It answers only "did this key sign this observation id". Whether that key
// belongs to anyone the archive should believe is a separate question, and
// keeping the two apart is deliberate: conflating them is how a valid signature
// from an unknown key comes to read as an endorsement.
func (a Attestation) Verify() error {
	if a.Schema != Schema {
		return fmt.Errorf("unsupported attestation schema %q", a.Schema)
	}
	public, err := ParsePublicKey(a.PublicKey)
	if err != nil {
		return err
	}
	signature, err := hex.DecodeString(a.Signature)
	if err != nil {
		return fmt.Errorf("signature is not hex: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("signature is %d bytes, want %d", len(signature), ed25519.SignatureSize)
	}
	if !ed25519.Verify(public.key, preimage(a.ObservationID), signature) {
		return errors.New("signature does not match this observation and key")
	}
	return nil
}

// Identity is a public key the archive's operator has decided to associate with
// a label.
type Identity struct {
	Schema     string    `json:"schema"`
	Label      string    `json:"label"`
	PublicKey  string    `json:"public_key"`
	DeclaredBy string    `json:"declared_by"`
	DeclaredAt time.Time `json:"declared_at"`
	Note       string    `json:"note,omitempty"`
}

func (i Identity) Validate() error {
	if i.Schema != IdentitySchema {
		return fmt.Errorf("unsupported identity schema %q", i.Schema)
	}
	if strings.TrimSpace(i.Label) == "" {
		return errors.New("an identity needs a label")
	}
	if _, err := ParsePublicKey(i.PublicKey); err != nil {
		return err
	}
	if strings.TrimSpace(i.DeclaredBy) == "" {
		return errors.New("an identity must name who registered it")
	}
	if i.DeclaredAt.IsZero() {
		return errors.New("declared_at is required")
	}
	return nil
}

func (i Identity) Fingerprint() string {
	public, err := ParsePublicKey(i.PublicKey)
	if err != nil {
		return ""
	}
	return public.Fingerprint()
}

// IdentityStore is the archive's record of which keys it recognises.
type IdentityStore struct {
	Root string
}

func NewIdentityStore(archiveRoot string) IdentityStore {
	return IdentityStore{Root: filepath.Join(archiveRoot, "policy", "identities")}
}

// Register adds a key under a label.
//
// A label already held by a different key is refused. Silently accepting the
// second one would let anyone who generates a key inherit the standing of the
// name they picked, which is exactly the confusion a registry exists to
// prevent. Replacing a key is then a deliberate act: remove the old identity
// first.
func (s IdentityStore) Register(identity Identity) error {
	identity.Schema = IdentitySchema
	identity.Label = strings.TrimSpace(identity.Label)
	if identity.DeclaredAt.IsZero() {
		identity.DeclaredAt = time.Now().UTC()
	}
	identity.DeclaredAt = identity.DeclaredAt.UTC()
	if err := identity.Validate(); err != nil {
		return err
	}

	existing, err := s.List()
	if err != nil {
		return err
	}
	for _, known := range existing {
		if !strings.EqualFold(known.Label, identity.Label) {
			continue
		}
		if known.PublicKey == identity.PublicKey {
			return nil
		}
		return fmt.Errorf("label %q is already registered to key %s; remove that identity first if you mean to replace it",
			identity.Label, known.Fingerprint()[:12])
	}

	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(identity, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Root, identity.Fingerprint()+".json"), append(encoded, '\n'), 0o644)
}

func (s IdentityStore) Remove(fingerprint string) (bool, error) {
	err := os.Remove(filepath.Join(s.Root, fingerprint+".json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s IdentityStore) List() ([]Identity, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Identity, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Root, entry.Name()))
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var identity Identity
		if err := decoder.Decode(&identity); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := identity.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		out = append(out, identity)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// Outcome is what an archive can say about one attestation.
type Outcome struct {
	ObservationID string
	// Valid means the signature matches the key it names.
	Valid bool
	// Label is the registered name for that key, empty when the key is not
	// registered. A valid signature from an unregistered key is exactly that
	// and nothing more.
	Label  string
	Reason string
}

// Evaluate reports whether an attestation verifies and whether its key is one
// the archive recognises. The two are separate on purpose.
func Evaluate(attestation Attestation, identities []Identity) Outcome {
	outcome := Outcome{ObservationID: attestation.ObservationID}
	if err := attestation.Verify(); err != nil {
		outcome.Reason = err.Error()
		return outcome
	}
	outcome.Valid = true
	for _, identity := range identities {
		if identity.PublicKey == attestation.PublicKey {
			outcome.Label = identity.Label
			outcome.Reason = "signed by a registered key"
			return outcome
		}
	}
	outcome.Reason = "signature is valid, but this key is not registered in this archive"
	return outcome
}

// Store keeps attestations inside an archive.
type Store struct {
	Root string
}

func NewStore(archiveRoot string) Store {
	return Store{Root: filepath.Join(archiveRoot, "attestations")}
}

func (s Store) path(attestation Attestation) (string, error) {
	public, err := ParsePublicKey(attestation.PublicKey)
	if err != nil {
		return "", err
	}
	id := attestation.ObservationID
	if len(id) < 2 {
		return "", fmt.Errorf("observation id %q is too short", id)
	}
	return filepath.Join(s.Root, id[:2], id+"."+public.Fingerprint()[:16]+".json"), nil
}

// Put stores an attestation. One observation can carry several, because two
// parties can independently vouch for the same record.
func (s Store) Put(attestation Attestation) error {
	if err := attestation.Verify(); err != nil {
		return fmt.Errorf("refusing to store an attestation that does not verify: %w", err)
	}
	path, err := s.path(attestation)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(attestation, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

// For returns every attestation stored for one observation.
func (s Store) For(observationID string) ([]Attestation, error) {
	if len(observationID) < 2 {
		return nil, fmt.Errorf("observation id %q is too short", observationID)
	}
	dir := filepath.Join(s.Root, observationID[:2])
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Attestation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), observationID+".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var attestation Attestation
		if err := json.Unmarshal(data, &attestation); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		out = append(out, attestation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublicKey < out[j].PublicKey })
	return out, nil
}
