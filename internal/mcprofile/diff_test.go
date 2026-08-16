package mcprofile

import (
	"path/filepath"
	"strings"
	"testing"
)

// Two real releases ship with the project, so the comparison is exercised
// against registries Mojang produced rather than ones written to make it pass.

func realProfile(t *testing.T, name string) Profile {
	t.Helper()
	profile, err := Load(filepath.Join("..", "..", "profiles", name))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestUpgradingFrom1_21_11To26_2AddsAndRemovesNothing(t *testing.T) {
	older := realProfile(t, "minecraft-java-1.21.11.json")
	newer := realProfile(t, "minecraft-java-26.2.json")

	delta := Compare(older, newer)
	if !delta.Forward() {
		t.Fatalf("26.2 (%d) should be forward of 1.21.11 (%d)", delta.ToDataVersion, delta.FromDataVersion)
	}
	if len(delta.BlocksRemoved) != 0 {
		t.Errorf("upgrading removes blocks: %v", delta.BlocksRemoved)
	}
	if len(delta.BiomesRemoved) != 0 {
		t.Errorf("upgrading removes biomes: %v", delta.BiomesRemoved)
	}
	if len(delta.BlocksAdded) != 30 {
		t.Errorf("upgrading adds %d blocks, want 30", len(delta.BlocksAdded))
	}
	if len(delta.BiomesAdded) != 1 {
		t.Errorf("upgrading adds %d biomes, want 1", len(delta.BiomesAdded))
	}

	// The whole reason to separate the two kinds of change: an upgrade that only
	// adds cannot invalidate an observation already captured.
	if delta.TouchesExistingArchives() {
		t.Error("an upgrade that only adds was reported as touching existing archives")
	}
}

// The same two releases the other way round. This is the direction convert has
// to survive, and it must not look like the harmless one.
func TestGoingBackwardsIsReportedAsTouchingExistingArchives(t *testing.T) {
	newer := realProfile(t, "minecraft-java-26.2.json")
	older := realProfile(t, "minecraft-java-1.21.11.json")

	delta := Compare(newer, older)
	if delta.Forward() {
		t.Fatal("1.21.11 was reported as forward of 26.2")
	}
	if len(delta.BlocksRemoved) != 30 {
		t.Errorf("going backwards removes %d blocks, want 30", len(delta.BlocksRemoved))
	}
	if !delta.TouchesExistingArchives() {
		t.Error("a downgrade that removes thirty blocks was reported as not touching existing archives")
	}
	found := false
	for _, name := range delta.BlocksRemoved {
		if name == "minecraft:cinnabar" {
			found = true
		}
	}
	if !found {
		t.Errorf("the removed set does not name a block that really went: %v", delta.BlocksRemoved)
	}
}

func TestAProfileComparedWithItselfIsEmpty(t *testing.T) {
	profile := realProfile(t, "minecraft-java-26.2.json")
	delta := Compare(profile, profile)
	if !delta.Empty() {
		t.Errorf("a profile differs from itself: %+v", delta)
	}
	if delta.TouchesExistingArchives() {
		t.Error("a profile compared with itself was reported as touching existing archives")
	}
	if delta.Forward() {
		t.Error("a profile was reported as forward of itself")
	}
}

// The build range is the change that put sections outside the world in 1.18, so
// narrowing has to be distinguished from widening rather than both counting as
// "changed".
func TestANarrowedBuildRangeIsDistinguishedFromAWidenedOne(t *testing.T) {
	base := realProfile(t, "minecraft-java-26.2.json")
	overworld, exists := base.Dimension("minecraft:overworld")
	if !exists {
		t.Fatal("26.2 has no overworld")
	}

	raised := clone(base)
	raised.Dimensions["minecraft:overworld"] = Dimension{
		MinSectionY:  overworld.MinSectionY + 1,
		SectionCount: overworld.SectionCount - 1,
	}
	narrowing := Compare(base, raised)
	if len(narrowing.DimensionsChanged) != 1 || !narrowing.DimensionsChanged[0].Narrowed() {
		t.Fatalf("raising the floor was not reported as narrowing: %+v", narrowing.DimensionsChanged)
	}
	if !narrowing.TouchesExistingArchives() {
		t.Error("a narrowed build range was reported as not touching existing archives")
	}

	deepened := clone(base)
	deepened.Dimensions["minecraft:overworld"] = Dimension{
		MinSectionY:  overworld.MinSectionY - 1,
		SectionCount: overworld.SectionCount + 1,
	}
	widening := Compare(base, deepened)
	if len(widening.DimensionsChanged) != 1 {
		t.Fatalf("lowering the floor was not reported as a change: %+v", widening.DimensionsChanged)
	}
	if widening.DimensionsChanged[0].Narrowed() {
		t.Error("lowering the floor was reported as narrowing")
	}
	if widening.TouchesExistingArchives() {
		t.Error("a widened build range was reported as touching existing archives")
	}
}

// Structure placement is the entire input to the seed search, so a different
// salt is a different answer rather than a smaller one.
func TestAChangedStructureSaltCountsEvenThoughNothingWasRemoved(t *testing.T) {
	base := realProfile(t, "minecraft-java-26.2.json")
	var id string
	for name := range base.StructureSets {
		if id == "" || name < id {
			id = name
		}
	}
	if id == "" {
		t.Fatal("26.2 profile carries no structure sets")
	}

	resalted := clone(base)
	set := resalted.StructureSets[id]
	set.Salt++
	resalted.StructureSets[id] = set

	delta := Compare(base, resalted)
	if len(delta.StructureSetsChanged) != 1 || delta.StructureSetsChanged[0].ID != id {
		t.Fatalf("changing a salt was not reported: %+v", delta.StructureSetsChanged)
	}
	if delta.Empty() {
		t.Error("a delta with a changed structure set reported itself empty")
	}
	if !delta.TouchesExistingArchives() {
		t.Error("a changed structure salt was reported as not touching existing archives")
	}
}

func TestTheBuildRangeIsDescribedInBlocksNotSections(t *testing.T) {
	described := Dimension{MinSectionY: -4, SectionCount: 24}.Describe()
	if !strings.Contains(described, "-64") || !strings.Contains(described, "319") {
		t.Errorf("the overworld range rendered as %q, want it to name -64 and 319", described)
	}
}

// Maps are shared by reference, so a test that mutates one has to copy the maps
// it touches or it corrupts the profile the next test loads.
func clone(p Profile) Profile {
	out := p
	out.Dimensions = make(map[string]Dimension, len(p.Dimensions))
	for id, dimension := range p.Dimensions {
		out.Dimensions[id] = dimension
	}
	out.StructureSets = make(map[string]StructureSet, len(p.StructureSets))
	for id, set := range p.StructureSets {
		out.StructureSets[id] = set
	}
	return out
}
