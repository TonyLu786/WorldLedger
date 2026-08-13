package model

import (
	"strings"
	"testing"
	"time"
)

func TestStateDigestIndependentOfMapOrder(t *testing.T) {
	a := map[string]BlobRef{
		"terrain": {Algorithm: "sha256", Digest: string64('a'), Size: 10},
		"biomes":  {Algorithm: "sha256", Digest: string64('b'), Size: 20},
	}
	b := map[string]BlobRef{
		"biomes":  a["biomes"],
		"terrain": a["terrain"],
	}
	if StateDigest(a) != StateDigest(b) {
		t.Fatal("state digest must not depend on map iteration order")
	}
}

func TestObservationIDIgnoresReceivedAt(t *testing.T) {
	base := Observation{
		Chunk:      ChunkRef{ServerID: "Example.org", Dimension: "OverWorld", X: 1, Z: -2},
		ObservedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Protocol:   "java/1.21",
		Source:     Source{Contributor: "alice"},
		Components: map[string]BlobRef{"chunk": {Algorithm: "sha256", Digest: string64('a'), Size: 10}},
	}
	a := base
	a.ReceivedAt = time.Now()
	b := base
	b.ReceivedAt = time.Now().Add(time.Hour)
	if ObservationID(a) != ObservationID(b) {
		t.Fatal("received_at is transport metadata and must not alter observation identity")
	}
}

func string64(r byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = r
	}
	return string(b)
}

func TestIdentityGoldenVector(t *testing.T) {
	o := Observation{
		Chunk:      ChunkRef{ServerID: "Example.ORG", Dimension: "Overworld", X: -17, Z: 42},
		ObservedAt: time.Date(2026, 8, 9, 12, 34, 56, 123000000, time.FixedZone("AEST", 10*60*60)),
		Protocol:   "java/test-v1",
		Source:     Source{Contributor: "alice"},
		Components: map[string]BlobRef{
			"terrain": {Algorithm: "sha256", Digest: string64('a'), Size: 1234},
			"biomes":  {Algorithm: "sha256", Digest: string64('b'), Size: 99},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	const wantState = "d549ff5258645300876b569f556f3dafad2ab221b01b748afabac819136b73e5"
	// Changed when the identity preimage stopped formatting the timestamp as
	// RFC 3339 text and started encoding it as integer seconds and nanoseconds.
	// A text form leaves trailing zeros in the fractional part to each
	// language's convention, so two conforming implementations derived
	// different identities for the same instant. See spec/observation-v1.md.
	// The state digest is unaffected: it never included the timestamp.
	const wantID = "34fb4e461b17ad99f3695031fee325bedf98cb626fec3c50a940114040491273"
	if o.StateDigest != wantState {
		t.Fatalf("state digest changed: have %s want %s", o.StateDigest, wantState)
	}
	if o.ID != wantID {
		t.Fatalf("observation id changed: have %s want %s", o.ID, wantID)
	}
}

// Identity must not depend on how an instant is formatted as text. These are
// the values where textual encodings disagree: a language that strips trailing
// zeros writes 100ms as ".1Z", one that pads to a group of three writes ".100Z".
// The previous single golden vector used 123ms, which both conventions render
// identically, so it could not have detected the difference.
func TestObservationIDDistinguishesSubSecondPrecision(t *testing.T) {
	base := func(nanos int) Observation {
		o := Observation{
			Chunk:      ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: 1, Z: 2},
			ObservedAt: time.Date(2026, 8, 9, 12, 0, 3, nanos, time.UTC),
			Protocol:   "java/test-v1",
			Source:     Source{Contributor: "alice"},
			Components: map[string]BlobRef{
				"chunk": {Algorithm: "sha256", Digest: string64('a'), Size: 1},
			},
		}
		if err := o.Finalize(); err != nil {
			t.Fatal(err)
		}
		return o
	}

	// Every one of these renders differently depending on the convention, so
	// each must produce a distinct identity.
	nanos := []int{0, 100000000, 120000000, 123000000, 123456000, 123456789, 1}
	seen := map[string]int{}
	for _, n := range nanos {
		id := base(n).ID
		if previous, exists := seen[id]; exists {
			t.Fatalf("%d ns and %d ns produced the same observation id", previous, n)
		}
		seen[id] = n
	}
}

// A whole second and the same second expressed with an explicit zero fraction
// are the same instant and must share an identity.
func TestObservationIDIsTheSameForEquivalentInstants(t *testing.T) {
	build := func(t *testing.T, when time.Time) string {
		t.Helper()
		o := Observation{
			Chunk:      ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld"},
			ObservedAt: when,
			Source:     Source{Contributor: "alice"},
			Components: map[string]BlobRef{
				"chunk": {Algorithm: "sha256", Digest: string64('a'), Size: 1},
			},
		}
		if err := o.Finalize(); err != nil {
			t.Fatal(err)
		}
		return o.ID
	}

	utc := time.Date(2026, 8, 9, 12, 0, 3, 0, time.UTC)
	// The same instant in another zone must not change identity.
	elsewhere := utc.In(time.FixedZone("AEST", 10*60*60))
	if build(t, utc) != build(t, elsewhere) {
		t.Fatal("the same instant produced different identities in different zones")
	}
}

func TestObservationRejectsNonCanonicalDigestCase(t *testing.T) {
	o := Observation{
		Chunk:      ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld"},
		ObservedAt: time.Unix(1, 0),
		Source:     Source{Contributor: "alice"},
		Components: map[string]BlobRef{
			"chunk": {Algorithm: "sha256", Digest: strings.Repeat("A", 64), Size: 1},
		},
	}
	if err := o.Finalize(); err == nil {
		t.Fatal("Finalize() accepted an uppercase content digest")
	}
}
