package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ObservationSchema = "worldledger.observation/v1"
	stateHashDomain   = "worldledger.state/v1"
)

type ChunkRef struct {
	ServerID  string `json:"server_id"`
	Dimension string `json:"dimension"`
	X         int32  `json:"x"`
	Z         int32  `json:"z"`
}

type BlobRef struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type Source struct {
	Contributor string `json:"contributor"`
	Agent       string `json:"agent,omitempty"`
}

type Observation struct {
	Schema      string             `json:"schema"`
	ID          string             `json:"id"`
	Chunk       ChunkRef           `json:"chunk"`
	ObservedAt  time.Time          `json:"observed_at"`
	ReceivedAt  time.Time          `json:"received_at"`
	Protocol    string             `json:"protocol,omitempty"`
	Source      Source             `json:"source"`
	Components  map[string]BlobRef `json:"components"`
	StateDigest string             `json:"state_digest"`
}

// The identity encoding, and what a second implementation has to reproduce.
//
// An observation id is this project's one irreplaceable value. Two programs
// that disagree about it disagree about whether they are looking at the same
// observation, and nothing downstream recovers from that: a fingerprint
// comparison reports a divergence that is not there, a negotiation asks for
// bytes the other side already sent, a redaction fails to find the record it
// names. writeInstant already refuses to leave the timestamp to a text format
// for exactly this reason.
//
// The strings were left to Go's own idea of space and case, which is not
// anybody else's:
//
//   - strings.ToLower is Unicode simple lowercase and locale-independent.
//     Java's String.toLowerCase() is locale-sensitive: on a Turkish JVM an
//     upper-case I lowercases to the dotless letter rather than to i, and the
//     same server name derives a different identity.
//   - strings.TrimSpace trims unicode.IsSpace, which includes U+0085 and
//     U+00A0. Java's String.trim() stops at U+0020 and String.strip() uses
//     Character.isWhitespace, which excludes both. A label with a non-breaking
//     space at one end normalizes three different ways.
//   - Both tables belong to a Unicode version. Two implementations built
//     against different revisions can drift without either changing a line.
//
// So the encoding takes neither table. Space is the six bytes in identitySpace
// and folding is A-Z alone; everything else is carried through as the UTF-8
// bytes it arrived as, which every language reproduces identically.
//
// Restricting these fields to ASCII was the first attempt and was worse. It
// would have refused a server somebody had named in their own language, for no
// gain: a name this does not fold is a name no implementation folds, so it is
// already reproducible. What it costs instead is that two spellings of a
// non-ASCII name are two servers. They come from a configuration field rather
// than from being retyped, and being wrong about that is a great deal cheaper
// than refusing to record what somebody saw.
//
// Valid UTF-8 is required, because that is the one thing a Go string can hold
// and another language cannot. It is also already true in practice: the JSON
// these are stored in would substitute a replacement character for a bad byte,
// so an identity derived from one would not match the record it was written on.
//
// None of this changes an identity any real archive derived. Over ASCII, which
// is every server address and every Minecraft resource location, Go's own
// functions were computing exactly this; and a non-ASCII name reaching here has
// already been lowered once by the code that stored it.
const identitySpace = " \t\n\v\f\r"

func trimIdentitySpace(s string) string { return strings.Trim(s, identitySpace) }

func foldIdentityCase(s string) string {
	var folded []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 'A' || c > 'Z' {
			if folded != nil {
				folded = append(folded, c)
			}
			continue
		}
		if folded == nil {
			folded = append(folded, s[:i]...)
		}
		folded = append(folded, c+('a'-'A'))
	}
	if folded == nil {
		return s
	}
	return string(folded)
}

// requireIdentityUTF8 refuses a value another language could not hold, let alone
// derive the same identity from.
func requireIdentityUTF8(field, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf(
			"%s is not valid UTF-8, and an observation identity is derived from these bytes:"+
				" the record it would be written on stores them as JSON, which substitutes a"+
				" replacement character, so the identity would not match its own record", field)
	}
	return nil
}

func NormalizeToken(s string) string {
	return foldIdentityCase(trimIdentitySpace(s))
}

// ContributorKey is the identity a contributor label stands for.
//
// The label is what somebody typed. The key is who they are, wherever this
// program has to decide whether two observations came from the same person:
// counting independent witnesses, and honouring a request to withhold
// somebody's data.
//
// Those two were decided differently. redact compared labels case-insensitively
// on the reasoning that failing to match one differing only in capitalisation
// would leave behind exactly what somebody asked to have removed. Corroboration
// compared them exactly, so one person writing their own name two ways was two
// independent witnesses, and a chunk only they had ever seen came out
// "corroborated". Those cannot both be right, and the withholding side is the
// one that was.
//
// It folds case and surrounding space and nothing else. Unicode has several
// other ways to spell one name twice, and none of them can be closed here: a
// contributor label is not verified in the first place, so anybody who wants a
// second identity can pick a second name. What this closes is the version that
// costs nothing and happens by accident.
//
// It is deliberately not the same function as NormalizeToken, which pins the
// identity encoding and cannot be generous. This one never reaches a digest, so
// it is free to be.
func ContributorKey(label string) string {
	return strings.ToLower(strings.TrimSpace(label))
}

func (c ChunkRef) Validate() error {
	server := NormalizeToken(c.ServerID)
	if server == "" {
		return errors.New("server_id is required")
	}
	if err := requireIdentityUTF8("server_id", server); err != nil {
		return err
	}
	dimension := NormalizeToken(c.Dimension)
	if dimension == "" {
		return errors.New("dimension is required")
	}
	return requireIdentityUTF8("dimension", dimension)
}

func (o *Observation) Finalize() error {
	if err := validateFields(*o); err != nil {
		return err
	}

	o.Schema = ObservationSchema
	o.Chunk.ServerID = NormalizeToken(o.Chunk.ServerID)
	o.Chunk.Dimension = NormalizeToken(o.Chunk.Dimension)
	o.Source.Contributor = trimIdentitySpace(o.Source.Contributor)
	o.Source.Agent = strings.TrimSpace(o.Source.Agent)
	o.Protocol = trimIdentitySpace(o.Protocol)
	o.ObservedAt = o.ObservedAt.UTC()
	if o.ReceivedAt.IsZero() {
		o.ReceivedAt = time.Now().UTC()
	} else {
		o.ReceivedAt = o.ReceivedAt.UTC()
	}

	o.StateDigest = StateDigest(o.Components)
	o.ID = ObservationID(*o)
	return nil
}

func (o Observation) ValidateStored() error {
	if err := validateFields(o); err != nil {
		return err
	}
	if o.Schema != ObservationSchema {
		return fmt.Errorf("unsupported observation schema %q", o.Schema)
	}
	if o.Chunk.ServerID != NormalizeToken(o.Chunk.ServerID) {
		return errors.New("server_id is not normalized")
	}
	if o.Chunk.Dimension != NormalizeToken(o.Chunk.Dimension) {
		return errors.New("dimension is not normalized")
	}
	if o.ReceivedAt.IsZero() {
		return errors.New("received_at is required in stored observations")
	}
	expectedState := StateDigest(o.Components)
	if o.StateDigest != expectedState {
		return fmt.Errorf("state_digest mismatch: have %s want %s", o.StateDigest, expectedState)
	}
	expectedID := ObservationID(o)
	if o.ID != expectedID {
		return fmt.Errorf("observation id mismatch: have %s want %s", o.ID, expectedID)
	}
	return nil
}

func validateFields(o Observation) error {
	if err := o.Chunk.Validate(); err != nil {
		return err
	}
	if o.ObservedAt.IsZero() {
		return errors.New("observed_at is required")
	}
	if trimIdentitySpace(o.Source.Contributor) == "" {
		return errors.New("source.contributor is required")
	}
	// The contributor label goes into the identity too, so it is held to the
	// same one rule. It is not folded and not restricted otherwise: it is
	// somebody's name.
	if err := requireIdentityUTF8("source.contributor", o.Source.Contributor); err != nil {
		return err
	}
	if err := requireIdentityUTF8("protocol", o.Protocol); err != nil {
		return err
	}
	if len(o.Components) == 0 {
		return errors.New("at least one component is required")
	}
	for name, ref := range o.Components {
		if strings.TrimSpace(name) == "" {
			return errors.New("component name must not be empty")
		}
		if ref.Algorithm != "sha256" || len(ref.Digest) != 64 || ref.Digest != strings.ToLower(ref.Digest) {
			return fmt.Errorf("component %q has invalid blob reference", name)
		}
		if _, err := hex.DecodeString(ref.Digest); err != nil {
			return fmt.Errorf("component %q has non-hex digest", name)
		}
		if ref.Size < 0 {
			return fmt.Errorf("component %q has negative size", name)
		}
	}
	return nil
}

func StateDigest(components map[string]BlobRef) string {
	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Strings(names)

	var b bytes.Buffer
	writeString(&b, stateHashDomain)
	writeU32(&b, uint32(len(names)))
	for _, name := range names {
		ref := components[name]
		writeString(&b, name)
		writeString(&b, ref.Algorithm)
		writeString(&b, ref.Digest)
		writeI64(&b, ref.Size)
	}
	sum := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(sum[:])
}

func ObservationID(o Observation) string {
	var b bytes.Buffer
	writeString(&b, ObservationSchema)
	writeString(&b, NormalizeToken(o.Chunk.ServerID))
	writeString(&b, NormalizeToken(o.Chunk.Dimension))
	writeI32(&b, o.Chunk.X)
	writeI32(&b, o.Chunk.Z)
	writeInstant(&b, o.ObservedAt)
	writeString(&b, trimIdentitySpace(o.Protocol))
	writeString(&b, trimIdentitySpace(o.Source.Contributor))
	writeString(&b, StateDigest(o.Components))
	sum := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(sum[:])
}

// writeInstant encodes a moment as integer seconds since the Unix epoch
// followed by integer nanoseconds within that second.
//
// It deliberately does not format a timestamp string. Every text form of an
// instant leaves a choice about trailing zeros in the fractional part, and
// languages resolve that choice differently: Go's RFC3339Nano strips them, so
// 100ms is ".1Z", while Java's Instant.toString pads to a group of three and
// writes ".100Z". Two conforming implementations would then derive different
// identities for the same instant, and the disagreement would only appear once
// a second implementation existed. Integers have no such freedom.
func writeInstant(b *bytes.Buffer, t time.Time) {
	utc := t.UTC()
	writeI64(b, utc.Unix())
	writeU32(b, uint32(utc.Nanosecond()))
}

func writeString(b *bytes.Buffer, s string) {
	writeU32(b, uint32(len([]byte(s))))
	_, _ = b.WriteString(s)
}

func writeU32(b *bytes.Buffer, v uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], v)
	_, _ = b.Write(data[:])
}

func writeI32(b *bytes.Buffer, v int32) {
	writeU32(b, uint32(v))
}

func writeI64(b *bytes.Buffer, v int64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], uint64(v))
	_, _ = b.Write(data[:])
}
