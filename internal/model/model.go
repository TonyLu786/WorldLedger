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

func NormalizeToken(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func (c ChunkRef) Validate() error {
	if NormalizeToken(c.ServerID) == "" {
		return errors.New("server_id is required")
	}
	if NormalizeToken(c.Dimension) == "" {
		return errors.New("dimension is required")
	}
	return nil
}

func (o *Observation) Finalize() error {
	if err := validateFields(*o); err != nil {
		return err
	}

	o.Schema = ObservationSchema
	o.Chunk.ServerID = NormalizeToken(o.Chunk.ServerID)
	o.Chunk.Dimension = NormalizeToken(o.Chunk.Dimension)
	o.Source.Contributor = strings.TrimSpace(o.Source.Contributor)
	o.Source.Agent = strings.TrimSpace(o.Source.Agent)
	o.Protocol = strings.TrimSpace(o.Protocol)
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
	if strings.TrimSpace(o.Source.Contributor) == "" {
		return errors.New("source.contributor is required")
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
	writeString(&b, strings.TrimSpace(o.Protocol))
	writeString(&b, strings.TrimSpace(o.Source.Contributor))
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
