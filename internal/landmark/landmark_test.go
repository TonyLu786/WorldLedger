package landmark

import (
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func spawn() Landmark {
	return Landmark{
		Server:     "example.org",
		Dimension:  "minecraft:overworld",
		Name:       "spawn",
		Bounds:     model.ChunkBounds{MinX: -2, MinZ: -2, MaxX: 2, MaxZ: 2},
		DeclaredBy: "alice",
	}
}

func chunk(server, dimension string, x, z int32) model.ChunkRef {
	return model.ChunkRef{ServerID: server, Dimension: dimension, X: x, Z: z}
}

// A landmark names a place on one server. Coordinates repeat across servers and
// dimensions, so a box that ignored them would claim the nether roof was spawn.
func TestALandmarkDoesNotReachIntoAnotherServerOrDimension(t *testing.T) {
	place := spawn()
	if !place.Contains(chunk("example.org", "minecraft:overworld", 0, 0)) {
		t.Fatal("a chunk inside the bounds was excluded")
	}
	if place.Contains(chunk("other.net", "minecraft:overworld", 0, 0)) {
		t.Error("another server's chunk was claimed")
	}
	if place.Contains(chunk("example.org", "minecraft:the_nether", 0, 0)) {
		t.Error("another dimension's chunk was claimed")
	}
	if place.Contains(chunk("example.org", "minecraft:overworld", 3, 0)) {
		t.Error("a chunk outside the bounds was claimed")
	}
}

// Case and spacing are how the same place gets declared twice by two people.
func TestTheSamePlaceDeclaredTwiceIsOneLandmark(t *testing.T) {
	store := NewStore(t.TempDir())
	first, err := store.Declare(spawn())
	if err != nil {
		t.Fatal(err)
	}
	moved := spawn()
	moved.Name = "  Spawn  "
	moved.Bounds = model.ChunkBounds{MinX: -4, MinZ: -4, MaxX: 4, MaxZ: 4}
	second, err := store.Declare(moved)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("the same name produced two landmarks: %s and %s", first.ID, second.ID)
	}

	set, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 1 {
		t.Fatalf("expected one landmark, got %d", len(set))
	}
	if set[0].Bounds.MaxX != 4 {
		t.Fatalf("the second declaration did not move it: %+v", set[0].Bounds)
	}
}

// Moving a landmark is editing it. If the bounds decided identity, every
// correction would leave the old one behind under the same name.
func TestMovingALandmarkDoesNotLeaveTheOldOne(t *testing.T) {
	first := spawn()
	moved := spawn()
	moved.Bounds = model.ChunkBounds{MinX: 100, MinZ: 100, MaxX: 101, MaxZ: 101}
	if first.deriveID() != moved.deriveID() {
		t.Fatal("bounds reached the identity, so moving a landmark creates a second one")
	}
}

func TestADifferentNameOrPlaceIsADifferentLandmark(t *testing.T) {
	base := spawn()
	for _, change := range []func(*Landmark){
		func(l *Landmark) { l.Name = "nether hub" },
		func(l *Landmark) { l.Server = "other.net" },
		func(l *Landmark) { l.Dimension = "minecraft:the_nether" },
	} {
		other := spawn()
		change(&other)
		if base.deriveID() == other.deriveID() {
			t.Errorf("%+v shares an id with %+v", other, base)
		}
	}
}

func TestADeclarationWithoutAnAuthorIsRefused(t *testing.T) {
	store := NewStore(t.TempDir())
	anonymous := spawn()
	anonymous.DeclaredBy = "  "
	if _, err := store.Declare(anonymous); err == nil {
		t.Fatal("an unattributed assertion about somebody's world was accepted")
	}
}

func TestInvertedBoundsAreRefused(t *testing.T) {
	store := NewStore(t.TempDir())
	backwards := spawn()
	backwards.Bounds = model.ChunkBounds{MinX: 5, MinZ: 0, MaxX: -5, MaxZ: 0}
	if _, err := store.Declare(backwards); err == nil {
		t.Fatal("an inverted box was accepted")
	}
}

func TestANameLongEnoughToBeAPayloadIsRefused(t *testing.T) {
	store := NewStore(t.TempDir())
	verbose := spawn()
	verbose.Name = strings.Repeat("a", MaxNameBytes+1)
	if _, err := store.Declare(verbose); err == nil {
		t.Fatal("an oversized name was accepted")
	}
}

func TestListingAnArchiveWithNoLandmarksIsNotAnError(t *testing.T) {
	set, err := NewStore(t.TempDir()).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 0 {
		t.Fatalf("got %d landmarks from an empty archive", len(set))
	}
}

func TestRemovingSomethingThatWasNeverThereIsNotAnError(t *testing.T) {
	store := NewStore(t.TempDir())
	removed, err := store.Remove("nothing")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("reported removing a landmark that never existed")
	}
}

func TestFindIgnoresCaseAndSpacing(t *testing.T) {
	set := Set{spawn()}
	if _, ok := set.Find("EXAMPLE.ORG", "minecraft:overworld", " SPAWN "); !ok {
		t.Fatal("a name differing in case and spacing was not found")
	}
	if _, ok := set.Find("example.org", "minecraft:the_nether", "spawn"); ok {
		t.Fatal("a landmark was found in the wrong dimension")
	}
}

func TestListingIsOrderedSoTwoRunsReadTheSame(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, name := range []string{"zeta", "alpha", "Mid"} {
		place := spawn()
		place.Name = name
		if _, err := store.Declare(place); err != nil {
			t.Fatal(err)
		}
	}
	set, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, landmark := range set {
		names = append(names, landmark.Name)
	}
	if strings.Join(names, ",") != "alpha,Mid,zeta" {
		t.Fatalf("order = %v", names)
	}
}

// An inclusive box of one chunk is one chunk, not none. Coverage is a fraction
// of this, so an off-by-one here reports the wrong progress everywhere.
func TestBoundsCountChunksInclusively(t *testing.T) {
	for _, test := range []struct {
		bounds model.ChunkBounds
		want   int
	}{
		{model.ChunkBounds{}, 1},
		{model.ChunkBounds{MinX: -2, MinZ: -2, MaxX: 2, MaxZ: 2}, 25},
		{model.ChunkBounds{MinX: 0, MinZ: 0, MaxX: 0, MaxZ: 3}, 4},
		{model.ChunkBounds{MinX: 5, MaxX: -5}, 0},
	} {
		if got := test.bounds.Chunks(); got != test.want {
			t.Errorf("%s covers %d chunks, want %d", test.bounds, got, test.want)
		}
	}
}

func TestCoverageFractionAndCompleteness(t *testing.T) {
	place := spawn()
	partial := Coverage{Landmark: place, Observed: 5, Total: 25}
	if partial.Fraction() != 0.2 || partial.Complete() {
		t.Errorf("%+v", partial)
	}
	whole := Coverage{Landmark: place, Observed: 25, Total: 25}
	if !whole.Complete() {
		t.Error("a fully observed landmark was not reported complete")
	}
	// A landmark covering nothing must not divide by zero or claim completeness.
	empty := Coverage{Landmark: place}
	if empty.Fraction() != 0 || empty.Complete() {
		t.Errorf("%+v", empty)
	}
}
