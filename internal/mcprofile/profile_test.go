package mcprofile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
)

func committedProfile(t *testing.T) Profile {
	t.Helper()
	profile, err := Load(filepath.Join("..", "..", "profiles", "minecraft-java-26.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestCommittedProfileMatchesThePinnedRelease(t *testing.T) {
	profile := committedProfile(t)

	if profile.Version != "26.2" {
		t.Fatalf("version = %q", profile.Version)
	}
	// The client jar's version.json declares world_version 4903.
	if profile.DataVersion != 4903 {
		t.Fatalf("data version = %d; want 4903", profile.DataVersion)
	}
	if len(profile.Blocks) == 0 || len(profile.Biomes) == 0 {
		t.Fatal("profile carries no registries")
	}
	for _, block := range []string{"minecraft:air", "minecraft:stone", "minecraft:oak_stairs", "minecraft:cave_air"} {
		if !profile.HasBlock(block) {
			t.Fatalf("profile is missing %s", block)
		}
	}
	if !profile.HasBiome("minecraft:the_void") || !profile.HasBiome("minecraft:plains") {
		t.Fatal("profile is missing a required biome")
	}
	if profile.HasBlock("minecraft:not_a_block") || profile.HasBiome("minecraft:not_a_biome") {
		t.Fatal("profile claims an identifier it does not have")
	}
}

// A client jar's blockstate definitions enumerate only the properties that
// change a rendered model. minecraft:oak_stairs declares facing, half, and shape
// but not waterlogged, so deriving state definitions from that source rejects
// real states. This is the exact state the controlled 26.2 fixture places.
func TestCheckBlockStateDoesNotRejectPropertiesAbsentFromRenderDefinitions(t *testing.T) {
	profile := committedProfile(t)

	state, err := mcjava.ParseBlockState("minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=false]")
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.CheckBlockState(state); err != nil {
		t.Fatalf("a valid 26.2 state was rejected: %v", err)
	}
}

func TestCheckBlockStateRejectsAnAbsentBlock(t *testing.T) {
	profile := committedProfile(t)

	state, err := mcjava.ParseBlockState("examplemod:marble")
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.CheckBlockState(state); err == nil {
		t.Fatal("a block outside the release registry was accepted")
	}
}

func TestDimensionBuildRangesMatchTheVanillaDimensionTypes(t *testing.T) {
	profile := committedProfile(t)

	overworld, exists := profile.Dimension("minecraft:overworld")
	if !exists {
		t.Fatal("profile has no overworld")
	}
	// dimension_type/overworld.json declares min_y -64 and height 384.
	if overworld.MinSectionY != -4 || overworld.SectionCount != 24 || overworld.MaxSectionY() != 19 {
		t.Fatalf("overworld range = %#v", overworld)
	}
	if !overworld.Contains(-4) || !overworld.Contains(19) {
		t.Fatal("overworld does not contain its own bounds")
	}
	if overworld.Contains(-5) || overworld.Contains(20) {
		t.Fatal("overworld contains a section outside its build range")
	}

	nether, exists := profile.Dimension("minecraft:the_nether")
	if !exists {
		t.Fatal("profile has no nether")
	}
	if nether.MinSectionY != 0 || nether.SectionCount != 16 {
		t.Fatalf("nether range = %#v", nether)
	}
	// A dimension lookup normalizes like the rest of the archive.
	if _, exists := profile.Dimension("Minecraft:Overworld"); !exists {
		t.Fatal("dimension lookup is case sensitive")
	}
	if _, exists := profile.Dimension("minecraft:absent"); exists {
		t.Fatal("profile invented a dimension")
	}
}

// Placement parameters are extracted so operators do not hand-enter them. The
// values are checkable against data/minecraft/worldgen/structure_set in the jar.
func TestStructureSetsCarryPlacementParameters(t *testing.T) {
	profile := committedProfile(t)

	if len(profile.StructureSets) == 0 {
		t.Fatal("profile carries no structure sets")
	}

	pyramids, exists := profile.StructureSets["minecraft:desert_pyramids"]
	if !exists {
		t.Fatal("desert_pyramids is absent")
	}
	if !pyramids.RandomSpread || pyramids.Spacing != 32 || pyramids.Separation != 8 || pyramids.Salt != 14357617 {
		t.Fatalf("desert_pyramids = %#v", pyramids)
	}

	monuments, exists := profile.StructureSets["minecraft:ocean_monuments"]
	if !exists {
		t.Fatal("ocean_monuments is absent")
	}
	if monuments.SpreadType != "triangular" {
		t.Fatalf("ocean_monuments spread = %q; want triangular", monuments.SpreadType)
	}

	// Strongholds use concentric rings, which this project does not model. It is
	// recorded without placement parameters rather than made to look usable.
	strongholds, exists := profile.StructureSets["minecraft:strongholds"]
	if !exists {
		t.Fatal("strongholds is absent")
	}
	if strongholds.RandomSpread {
		t.Fatal("strongholds was marked as random spread")
	}
	if strongholds.Spacing != 0 || strongholds.Separation != 0 {
		t.Fatalf("an unmodelled placement carries parameters: %#v", strongholds)
	}
}

func TestValidateRejectsMalformedProfiles(t *testing.T) {
	valid := func() Profile {
		return Profile{
			Schema:      Schema,
			Version:     "26.2",
			DataVersion: 4903,
			Dimensions:  map[string]Dimension{"minecraft:overworld": {MinSectionY: -4, SectionCount: 24}},
			Blocks:      []string{"minecraft:air", "minecraft:stone"},
			Biomes:      []string{"minecraft:plains"},
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}

	tests := map[string]func(*Profile){
		"bad schema":       func(p *Profile) { p.Schema = "other" },
		"no version":       func(p *Profile) { p.Version = "" },
		"no data version":  func(p *Profile) { p.DataVersion = 0 },
		"no dimensions":    func(p *Profile) { p.Dimensions = nil },
		"empty dimension":  func(p *Profile) { p.Dimensions["minecraft:overworld"] = Dimension{} },
		"no blocks":        func(p *Profile) { p.Blocks = nil },
		"unsorted blocks":  func(p *Profile) { p.Blocks = []string{"minecraft:stone", "minecraft:air"} },
		"duplicate blocks": func(p *Profile) { p.Blocks = []string{"minecraft:air", "minecraft:air"} },
		"no biomes":        func(p *Profile) { p.Biomes = nil },
		"unsorted biomes":  func(p *Profile) { p.Biomes = []string{"minecraft:plains", "minecraft:badlands"} },
	}
	for name, mutate := range tests {
		profile := valid()
		mutate(&profile)
		if err := profile.Validate(); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// Windows PowerShell writes UTF-8 with a byte order mark by default, so a
// hand-edited profile very often carries one.
func TestLoadAcceptsAUTF8ByteOrderMark(t *testing.T) {
	original := committedProfile(t)
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, data...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("a profile with a byte order mark was rejected: %v", err)
	}
}

func TestProfileSurvivesASaveLoadRoundTrip(t *testing.T) {
	original := committedProfile(t)
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.DataVersion != original.DataVersion ||
		len(reloaded.Blocks) != len(original.Blocks) ||
		len(reloaded.Biomes) != len(original.Biomes) ||
		len(reloaded.Dimensions) != len(original.Dimensions) {
		t.Fatal("profile changed across a save/load round trip")
	}
}
