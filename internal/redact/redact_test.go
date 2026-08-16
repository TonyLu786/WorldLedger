package redact

import (
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func observation(server, dimension, contributor string, x, z int32) model.Observation {
	return model.Observation{
		Chunk:      model.ChunkRef{ServerID: server, Dimension: dimension, X: x, Z: z},
		ObservedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Source:     model.Source{Contributor: contributor},
	}
}

func declaration(r Redaction) Redaction {
	r.Schema = Schema
	r.Reason = "test"
	r.DeclaredBy = "reviewer"
	r.DeclaredAt = time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return r
}

func TestContributorScopeMatchesOnlyThatContributor(t *testing.T) {
	redaction := declaration(Redaction{Server: "example.org", Contributor: "alice"})

	if !redaction.Matches(observation("example.org", "minecraft:overworld", "alice", 0, 0)) {
		t.Fatal("alice's own observation was not matched")
	}
	if redaction.Matches(observation("example.org", "minecraft:overworld", "bob", 0, 0)) {
		t.Fatal("bob's observation was matched by a redaction scoped to alice")
	}
	if redaction.Matches(observation("other.net", "minecraft:overworld", "alice", 0, 0)) {
		t.Fatal("a redaction on one server reached another")
	}
}

// Withholding data is a consent decision, so a label differing only in
// capitalisation must not leave behind the very thing someone asked to remove.
func TestContributorScopeIgnoresCaseAndSurroundingSpace(t *testing.T) {
	redaction := declaration(Redaction{Server: "example.org", Contributor: "Alice"})
	if !redaction.Matches(observation("example.org", "minecraft:overworld", " alice ", 0, 0)) {
		t.Fatal("a contributor label differing in case and spacing escaped the redaction")
	}
}

func TestRegionScopeCoversItsBoundsInclusively(t *testing.T) {
	redaction := declaration(Redaction{
		Server: "example.org",
		Region: &model.ChunkBounds{MinX: -2, MinZ: -2, MaxX: 2, MaxZ: 2},
	})

	inside := [][2]int32{{-2, -2}, {2, 2}, {0, 0}, {-2, 2}}
	for _, chunk := range inside {
		if !redaction.Matches(observation("example.org", "minecraft:overworld", "anyone", chunk[0], chunk[1])) {
			t.Fatalf("chunk (%d,%d) is inside the region but was not matched", chunk[0], chunk[1])
		}
	}
	outside := [][2]int32{{-3, 0}, {3, 0}, {0, -3}, {0, 3}}
	for _, chunk := range outside {
		if redaction.Matches(observation("example.org", "minecraft:overworld", "anyone", chunk[0], chunk[1])) {
			t.Fatalf("chunk (%d,%d) is outside the region but was matched", chunk[0], chunk[1])
		}
	}
}

func TestRegionScopeCanBeLimitedToOneDimension(t *testing.T) {
	redaction := declaration(Redaction{
		Server:    "example.org",
		Dimension: "minecraft:the_nether",
		Region:    &model.ChunkBounds{MinX: 0, MinZ: 0, MaxX: 0, MaxZ: 0},
	})
	if !redaction.Matches(observation("example.org", "minecraft:the_nether", "anyone", 0, 0)) {
		t.Fatal("the nether observation was not matched")
	}
	if redaction.Matches(observation("example.org", "minecraft:overworld", "anyone", 0, 0)) {
		t.Fatal("the overworld chunk at the same coordinates was matched")
	}
}

func TestFilterSeparatesWithheldFromKept(t *testing.T) {
	set := Set{declaration(Redaction{Server: "example.org", Contributor: "alice"})}
	kept, withheld := set.Filter([]model.Observation{
		observation("example.org", "minecraft:overworld", "alice", 0, 0),
		observation("example.org", "minecraft:overworld", "bob", 0, 0),
	})
	if len(kept) != 1 || kept[0].Source.Contributor != "bob" {
		t.Fatalf("expected only bob kept, got %v", kept)
	}
	if len(withheld) != 1 || withheld[0].Source.Contributor != "alice" {
		t.Fatalf("expected only alice withheld, got %v", withheld)
	}
}

func TestAnEmptySetChangesNothing(t *testing.T) {
	observations := []model.Observation{observation("example.org", "minecraft:overworld", "alice", 0, 0)}
	kept, withheld := Set(nil).Filter(observations)
	if len(kept) != 1 || len(withheld) != 0 {
		t.Fatalf("an archive with no redactions must pass everything through; kept %d withheld %d", len(kept), len(withheld))
	}
}

func TestValidationRequiresAttributionAndAReason(t *testing.T) {
	cases := map[string]Redaction{
		"no server":      {Schema: Schema, Reason: "r", DeclaredBy: "d", DeclaredAt: time.Now()},
		"no declared by": {Schema: Schema, Server: "s", Reason: "r", DeclaredAt: time.Now()},
		"no reason":      {Schema: Schema, Server: "s", DeclaredBy: "d", DeclaredAt: time.Now()},
		"inverted region": {
			Schema: Schema, Server: "s", Reason: "r", DeclaredBy: "d", DeclaredAt: time.Now(),
			Region: &model.ChunkBounds{MinX: 5, MinZ: 0, MaxX: -5, MaxZ: 0},
		},
	}
	for name, redaction := range cases {
		if err := redaction.Validate(); err == nil {
			t.Fatalf("%s: expected validation to fail", name)
		}
	}
}

func TestStoreRoundTripsAndDeclaringTwiceReplaces(t *testing.T) {
	store := NewStore(t.TempDir())
	first, err := store.Declare(declaration(Redaction{Server: "example.org", Contributor: "alice"}))
	if err != nil {
		t.Fatal(err)
	}

	again := declaration(Redaction{Server: "example.org", Contributor: "alice"})
	again.Reason = "restated with more detail"
	second, err := store.Declare(again)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("the same scope produced two ids: %s and %s", first.ID, second.ID)
	}

	listed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one record after redeclaring the same scope, got %d", len(listed))
	}
	if listed[0].Reason != "restated with more detail" {
		t.Fatalf("the second declaration did not replace the first: %q", listed[0].Reason)
	}
}

// A redaction is a decision, and decisions are sometimes wrong or superseded by
// consent given later.
func TestWithdrawRemovesADeclaration(t *testing.T) {
	store := NewStore(t.TempDir())
	declared, err := store.Declare(declaration(Redaction{Server: "example.org", Contributor: "alice"}))
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.Withdraw(declared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("withdrawing an existing redaction reported nothing removed")
	}
	listed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected no redactions after withdrawal, got %d", len(listed))
	}

	missing, err := store.Withdraw(declared.ID)
	if err != nil {
		t.Fatalf("withdrawing something already gone must not error: %v", err)
	}
	if missing {
		t.Fatal("withdrawing a missing redaction reported a removal")
	}
}

func TestScopeWithoutNarrowingCoversTheWholeServer(t *testing.T) {
	redaction := declaration(Redaction{Server: "example.org"})
	if err := redaction.Validate(); err != nil {
		t.Fatalf("a whole-server redaction is a real request and must be expressible: %v", err)
	}
	if !redaction.Matches(observation("example.org", "minecraft:the_end", "anyone", 99, -99)) {
		t.Fatal("a whole-server redaction did not match an observation on that server")
	}
	if got := redaction.Describe(); got != "server example.org (every contributor, every chunk)" {
		t.Fatalf("the description must make the breadth obvious, got %q", got)
	}
}
