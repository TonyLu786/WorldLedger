package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/anvil"
	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/model"
	"github.com/worldledger/worldledger-mc/internal/translate"
)

// Under the report policy a conversion is decided by one pass and written by
// another. The first approves a world and the second writes one, and the
// approval is worth something only if they are the same world.
//
// They translate the same chunks with the same rules through two translators,
// so what has to hold is that translation depends on nothing but its input.
// It does today because everything in it that ranges over a map sorts what it
// collected afterwards: the rebuilt palette, the order of sections, and the
// order of the report. A sort going missing is the likeliest way this breaks,
// and it would break silently -- the palette would still be valid, only
// ordered differently from one pass to the next. Go randomises the order of a
// map on every range, so translating the same chunk many times is how a
// missing sort shows itself.

// newOnlyStates are blocks Minecraft 26.2 has and 1.21.11 does not, each
// substituted below. Four of them become one block, so the palette has to be
// rebuilt rather than patched, and that rebuilding is where a map is ranged.
var newOnlyStates = map[string]string{
	"minecraft:cinnabar":          "minecraft:red_terracotta",
	"minecraft:polished_cinnabar": "minecraft:red_terracotta",
	"minecraft:chiseled_cinnabar": "minecraft:red_terracotta",
	"minecraft:cinnabar_bricks":   "minecraft:red_terracotta",
	"minecraft:chiseled_sulfur":   "minecraft:yellow_terracotta",
	"minecraft:golden_dandelion":  "minecraft:dandelion",
}

func rulesForTheNewStates(t *testing.T) string {
	t.Helper()
	substitutions := map[string]translate.Substitution{}
	for source, target := range newOnlyStates {
		substitutions[source] = translate.Substitution{Block: target}
	}
	rules := translate.Rules{
		Schema:             translate.RulesSchema,
		Substitutions:      substitutions,
		BiomeSubstitutions: map[string]string{"minecraft:sulfur_caves": "minecraft:dripstone_caves"},
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// mixedChunk holds the new-only states beside states both releases know, in
// equal numbers, so every change in the report ties on its count and its
// place in the report is decided by the tie-break alone.
func mixedChunk(t *testing.T) anvil.PreparedChunk {
	t.Helper()
	names := []string{
		"minecraft:stone", "minecraft:dirt", "minecraft:oak_planks", "minecraft:glass",
		"minecraft:cobblestone", "minecraft:sand", "minecraft:gravel", "minecraft:clay",
	}
	for source := range newOnlyStates {
		names = append(names, source)
	}
	states := make([]mcjava.BlockState, mcjava.BlockCount)
	for position := range states {
		states[position] = mcjava.BlockState{Name: names[position%len(names)]}
	}
	encodedBlocks, err := mcjava.EncodeBlockSection(-4, states)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := mcjava.DecodeBlockSection(encodedBlocks)
	if err != nil {
		t.Fatal(err)
	}

	biomeNames := []string{"minecraft:plains", "minecraft:sulfur_caves", "minecraft:desert", "minecraft:forest"}
	biomeValues := make([]string, mcjava.BiomeCount)
	for position := range biomeValues {
		biomeValues[position] = biomeNames[position%len(biomeNames)]
	}
	encodedBiomes, err := mcjava.EncodeBiomeSection(-4, biomeValues)
	if err != nil {
		t.Fatal(err)
	}
	biomes, err := mcjava.DecodeBiomeSection(encodedBiomes)
	if err != nil {
		t.Fatal(err)
	}

	return anvil.PreparedChunk{
		Chunk: model.ChunkRef{ServerID: "example", Dimension: "minecraft:overworld", X: 3, Z: -2},
		Components: anvil.ChunkComponents{
			Shape:  mcjava.Shape{MinSectionY: -4, SectionCount: 24},
			Blocks: map[int32]mcjava.BlockSection{-4: blocks},
			Biomes: map[int32]mcjava.BiomeSection{-4: biomes},
		},
	}
}

// encodeChunk is the chunk as the bytes a region file would carry, which is the
// only comparison that means "the same world".
func encodeChunk(t *testing.T, conversion *translation, entry anvil.PreparedChunk) []byte {
	t.Helper()
	chunk, err := anvil.BuildChunk(entry.Chunk.X, entry.Chunk.Z, conversion.profile.DataVersion, entry.Components)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := anvil.EncodeNamed("", chunk)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestTheDecidingPassApprovesTheWorldTheWritingPassWrites(t *testing.T) {
	conversion, err := newTranslation("minecraft:overworld", translationOptions{
		profilePath: olderProfile(),
		rulesPath:   rulesForTheNewStates(t),
		policyName:  "report",
		filler:      translate.DefaultFiller,
		fillerBiome: translate.DefaultBiomeFiller,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !conversion.canRefuse() {
		t.Fatal("the report policy is the one that decides before it writes; this proves nothing about the other two")
	}
	input := []anvil.PreparedChunk{mixedChunk(t)}

	decided, err := conversion.chunks(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := conversion.verdict(); err != nil {
		t.Fatalf("a conversion with a rule for every state was refused, so nothing would be written: %v", err)
	}
	approved := encodeChunk(t, conversion, decided[0])
	report := conversion.translator.Report()
	if len(report.Blocks) != len(newOnlyStates) || len(report.Biomes) != 1 {
		t.Fatalf("the report holds %d block and %d biome changes, want %d and 1; the rules did not apply and this proves nothing",
			len(report.Blocks), len(report.Biomes), len(newOnlyStates))
	}

	for pass := 1; pass <= 32; pass++ {
		if err := conversion.restart(); err != nil {
			t.Fatal(err)
		}
		written, err := conversion.chunks(input)
		if err != nil {
			t.Fatal(err)
		}
		if got := encodeChunk(t, conversion, written[0]); !bytes.Equal(got, approved) {
			t.Fatalf("writing pass %d produced a different chunk from the one the deciding pass approved", pass)
		}
		if got := conversion.translator.Report(); !reflect.DeepEqual(got, report) {
			t.Fatalf("writing pass %d reported the losses differently:\n  approved %+v\n  written  %+v", pass, report, got)
		}
	}
}
