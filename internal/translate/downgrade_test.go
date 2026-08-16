package translate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/mcprofile"
)

// Until now every translation test targeted a profile of the release the
// archive was captured from, or a synthetic one written for the test. Both can
// only prove the mechanism. Whether the rules describe a real downgrade needs a
// real older release, and 1.21.11 is one: it lacks thirty blocks and one biome
// that 26.2 has, and holds nothing 26.2 does not.
//
// The blocks named here are chosen from that difference rather than invented,
// so a future release that adds them back would make these tests fail loudly
// instead of passing on a name nobody ships.

func olderTarget(t *testing.T) mcprofile.Profile {
	t.Helper()
	profile, err := mcprofile.Load(filepath.Join("..", "..", "profiles", "minecraft-java-1.21.11.json"))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func olderOverworld(t *testing.T) mcprofile.Dimension {
	t.Helper()
	dimension, exists := olderTarget(t).Dimension("minecraft:overworld")
	if !exists {
		t.Fatal("the 1.21.11 profile has no overworld")
	}
	return dimension
}

// The premise the rest of this file rests on. If it ever stops holding, the
// tests below would be checking a downgrade that is not one.
func TestTheOlderProfileIsGenuinelyOlderAndSmaller(t *testing.T) {
	older := olderTarget(t)
	current, err := mcprofile.Load(filepath.Join("..", "..", "profiles", "minecraft-java-26.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if older.DataVersion >= current.DataVersion {
		t.Fatalf("1.21.11 reports data version %d against 26.2's %d", older.DataVersion, current.DataVersion)
	}
	for _, name := range []string{"minecraft:cinnabar", "minecraft:golden_dandelion"} {
		if older.HasBlock(name) {
			t.Errorf("1.21.11 is recorded as having %s, so it is no longer a downgrade for it", name)
		}
		if !current.HasBlock(name) {
			t.Errorf("26.2 does not have %s, so this test names a block nobody ships", name)
		}
	}
}

// A block the older release cannot represent has to be reported, whatever the
// policy does about it. A silent downgrade is the failure this whole path
// exists to prevent: the world would look plausible and be wrong.
func TestABlockTheOlderReleaseLacksIsReported(t *testing.T) {
	translator, err := New(olderTarget(t), Rules{}, PolicyFill, "minecraft:stone", "minecraft:plains")
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{
		Shape:  mcjava.Shape{MinSectionY: 0, SectionCount: 1},
		Blocks: map[int32]mcjava.BlockSection{0: blockSection(t, 0, "minecraft:cinnabar")},
	}
	if _, _, err := translator.Chunk(chunk, olderOverworld(t)); err != nil {
		t.Fatal(err)
	}

	report := translator.Report()
	if !report.Lossy() {
		t.Fatal("filling a block the target cannot represent was reported as lossless")
	}
	named := false
	for _, change := range report.Blocks {
		if strings.Contains(change.Source, "cinnabar") {
			named = true
		}
	}
	if !named {
		t.Fatalf("the report does not name what was lost: %+v", report.Blocks)
	}
}

// The default policy leaves the chunk unwritten rather than inventing a
// substitute, and says how many it left out. A skipped chunk in an exported
// world is honest; a chunk quietly full of stone is not.
func TestTheDefaultPolicyLeavesSuchAChunkUnwritten(t *testing.T) {
	translator, err := New(olderTarget(t), Rules{}, PolicySkipChunk, "minecraft:stone", "minecraft:plains")
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{
		Shape:  mcjava.Shape{MinSectionY: 0, SectionCount: 1},
		Blocks: map[int32]mcjava.BlockSection{0: blockSection(t, 0, "minecraft:cinnabar")},
	}
	_, written, err := translator.Chunk(chunk, olderOverworld(t))
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Fatal("a chunk the target cannot represent was written anyway")
	}
	if report := translator.Report(); report.SkippedChunks != 1 {
		t.Fatalf("skipped chunks = %d, want 1: %+v", report.SkippedChunks, report)
	}
}

// A chunk of blocks the older release does have must survive untouched. The
// gametest world is entirely such blocks, which is why its conversion reported
// no loss, and that has to be the truth rather than a translator that gave up
// quietly.
func TestOrdinaryBlocksSurviveTheDowngrade(t *testing.T) {
	translator, err := New(olderTarget(t), Rules{}, PolicySkipChunk, "minecraft:stone", "minecraft:plains")
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{
		Shape: mcjava.Shape{MinSectionY: 0, SectionCount: 1},
		Blocks: map[int32]mcjava.BlockSection{
			0: blockSection(t, 0, "minecraft:stone", "minecraft:dirt", "minecraft:grass_block"),
		},
	}
	out, written, err := translator.Chunk(chunk, olderOverworld(t))
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("a chunk of blocks 1.21.11 has was not written")
	}
	if report := translator.Report(); report.Lossy() {
		t.Fatalf("an unchanged chunk was reported as lossy: %+v", report)
	}
	for _, state := range out.Blocks[0].States() {
		if !strings.HasPrefix(state, "minecraft:stone") &&
			!strings.HasPrefix(state, "minecraft:dirt") &&
			!strings.HasPrefix(state, "minecraft:grass_block") {
			t.Fatalf("a block changed during a downgrade that needed no change: %s", state)
		}
	}
}

// Refusing is the policy for somebody who would rather have nothing than an
// approximation, and it has to refuse rather than report and continue.
func TestTheReportPolicyRefusesInsteadOfApproximating(t *testing.T) {
	translator, err := New(olderTarget(t), Rules{}, PolicyReport, "minecraft:stone", "minecraft:plains")
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{
		Shape:  mcjava.Shape{MinSectionY: 0, SectionCount: 1},
		Blocks: map[int32]mcjava.BlockSection{0: blockSection(t, 0, "minecraft:cinnabar")},
	}
	if _, _, err := translator.Chunk(chunk, olderOverworld(t)); err != nil {
		t.Fatal(err)
	}
	if !translator.Refused() {
		t.Fatal("the report policy did not refuse a translation it cannot make faithfully")
	}
}

// The biome registry shrank by one as well, and biomes travel by name in a
// separate component, so they need their own check rather than riding on the
// block result.
func TestABiomeTheOlderReleaseLacksIsReported(t *testing.T) {
	translator, err := New(olderTarget(t), Rules{}, PolicyFill, "minecraft:stone", "minecraft:plains")
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{
		Shape:  mcjava.Shape{MinSectionY: 0, SectionCount: 1},
		Blocks: map[int32]mcjava.BlockSection{0: blockSection(t, 0, "minecraft:stone")},
		Biomes: map[int32]mcjava.BiomeSection{0: biomeSection(t, 0, "minecraft:sulfur_caves")},
	}
	if _, _, err := translator.Chunk(chunk, olderOverworld(t)); err != nil {
		t.Fatal(err)
	}
	report := translator.Report()
	named := false
	for _, change := range report.Biomes {
		if strings.Contains(change.Source, "sulfur_caves") {
			named = true
		}
	}
	if !named {
		t.Fatalf("the report does not name the biome that was lost: %+v", report.Biomes)
	}
}
