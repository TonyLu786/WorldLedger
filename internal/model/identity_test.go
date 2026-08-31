package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// An observation id is derived from strings, and the encoding used to derive it
// through Go's own idea of space and case. Those are not portable ideas, and
// this project intends a second implementation to exist.

// The compatibility claim, and the reason this could be changed at all. Over
// everything a real archive holds, the pinned encoding computes exactly what
// the previous one computed, so no identity moved.
func TestThePinnedEncodingMatchesWhatItReplaced(t *testing.T) {
	for _, token := range []string{
		"example.org:25565",
		"EXAMPLE.org:25565",
		"  play.example.net  ",
		"minecraft:overworld",
		"minecraft:the_nether",
		"MINECRAFT:THE_END",
		"127.0.0.1:25565",
		"a-server_with.every/allowed:char-9",
		"",
		"\tspaced\t",
	} {
		previous := strings.TrimSpace(strings.ToLower(token))
		if got := NormalizeToken(token); got != previous {
			t.Errorf("NormalizeToken(%q) = %q; the encoding it replaced gave %q", token, got, previous)
		}
	}
}

// What was actually wrong. Go trims these as space and Java does not, so two
// implementations disagreed about where a token ended, and therefore about
// every identity derived from one.
func TestSpaceIsSixBytesAndNotWhateverUnicodeSaysThisYear(t *testing.T) {
	for _, token := range []string{
		"name\u00a0", // no-break space: unicode.IsSpace, not Character.isWhitespace
		"name\u0085", // next line: same disagreement
		"\u2003name", // em space
		"name\u3000", // ideographic space
	} {
		if got := NormalizeToken(token); got == "name" {
			t.Errorf("NormalizeToken(%q) trimmed a character only some languages call space", token)
		}
	}
	if got := NormalizeToken(" \t\n\v\f\rname \t\n\v\f\r"); got != "name" {
		t.Errorf("NormalizeToken did not trim the six bytes it pins; got %q", got)
	}
}

// Folding is A-Z. Anything else is carried through as the bytes it arrived as,
// which is what makes it reproducible without agreeing on a Unicode version.
func TestFoldingTouchesOnlyASCIILetters(t *testing.T) {
	if got := NormalizeToken("SERVER"); got != "server" {
		t.Errorf("NormalizeToken(SERVER) = %q", got)
	}
	// Lower-casing this depends on a locale in Java and on a table version
	// everywhere, so the encoding does not attempt it.
	const cyrillic = "\u041c\u041e\u0421\u041a\u0412\u0410"
	if got := NormalizeToken(cyrillic); got != cyrillic {
		t.Errorf("NormalizeToken folded a letter outside ASCII: %q became %q", cyrillic, got)
	}
}

// A name in somebody's own language is recorded, not refused. Restricting these
// fields to ASCII would have closed nothing that folding A-Z had not already
// closed, and would have cost a person their capture.
func TestANameOutsideASCIIIsStillAnObservation(t *testing.T) {
	o := Observation{
		Chunk:      ChunkRef{ServerID: "\u670d\u52a1\u5668.example:25565", Dimension: "minecraft:overworld"},
		ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Protocol:   "770",
		Source:     Source{Contributor: "\u674e\u660e"},
		Components: map[string]BlobRef{
			"blocks": {Algorithm: "sha256", Digest: strings.Repeat("a", 64), Size: 1},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatalf("an observation named outside ASCII was refused: %v", err)
	}
	if err := o.ValidateStored(); err != nil {
		t.Fatalf("it does not validate as stored: %v", err)
	}
}

// The one thing a Go string can hold that another language cannot, and that the
// record it would be written on cannot hold either: the JSON encoder would
// substitute a replacement character, so the identity would not match its own
// file.
func TestBytesThatAreNotUTF8AreRefused(t *testing.T) {
	o := Observation{
		Chunk:      ChunkRef{ServerID: "example\xff.org", Dimension: "minecraft:overworld"},
		ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Source:     Source{Contributor: "alice"},
		Components: map[string]BlobRef{
			"blocks": {Algorithm: "sha256", Digest: strings.Repeat("a", 64), Size: 1},
		},
	}
	err := o.Finalize()
	if err == nil {
		t.Fatal("an identity was derived from bytes the record could not store")
	}
	if !strings.Contains(err.Error(), "server_id") {
		t.Errorf("the error does not name the field: %v", err)
	}
}

// A golden identity. Anything that changes the encoding changes this, which is
// the point: it should not be possible to change it without saying so.
func TestAKnownObservationStillHasTheSameIdentity(t *testing.T) {
	o := Observation{
		Chunk: ChunkRef{
			ServerID:  "Example.org:25565",
			Dimension: "minecraft:overworld",
			X:         12,
			Z:         -7,
		},
		ObservedAt: time.Date(2026, 8, 31, 12, 34, 56, 789000000, time.UTC),
		Protocol:   "minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1",
		Source:     Source{Contributor: "Alice"},
		Components: map[string]BlobRef{
			"mcjava.shape":  {Algorithm: "sha256", Digest: strings.Repeat("a", 64), Size: 53},
			"mcjava.blocks": {Algorithm: "sha256", Digest: strings.Repeat("b", 64), Size: 4096},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	const want = "3e00983ce680c5e972c153001d0c4c640b49c9cf4cb2af253d5d10032fd0c044"
	if o.ID != want {
		t.Errorf("observation id = %q, want %q", o.ID, want)
	}

	// And that value is not new. This is the encoding as it stood before the
	// strings were pinned, run over the same observation: if the two ever
	// disagree for an input like this one, an archive somebody already holds
	// has had its identities changed underneath it.
	if previous := identityAsItWasBefore(o); o.ID != previous {
		t.Errorf("the pinned encoding derives %q where the previous one derived %q", o.ID, previous)
	}
}

// identityAsItWasBefore is ObservationID with the two calls that were replaced,
// kept here so the compatibility claim is checked rather than asserted.
func identityAsItWasBefore(o Observation) string {
	var b bytes.Buffer
	writeString(&b, ObservationSchema)
	writeString(&b, strings.TrimSpace(strings.ToLower(o.Chunk.ServerID)))
	writeString(&b, strings.TrimSpace(strings.ToLower(o.Chunk.Dimension)))
	writeI32(&b, o.Chunk.X)
	writeI32(&b, o.Chunk.Z)
	writeInstant(&b, o.ObservedAt)
	writeString(&b, strings.TrimSpace(o.Protocol))
	writeString(&b, strings.TrimSpace(o.Source.Contributor))
	writeString(&b, StateDigest(o.Components))
	sum := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(sum[:])
}
