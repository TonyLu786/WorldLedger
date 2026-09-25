package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/anvil"
	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/model"
	"github.com/worldledger/worldledger-mc/internal/translate"
)

// convert is the command that produces an approximation of a world rather than
// a copy of one, and it had no test of any kind. It is now written one region
// at a time, which splits it into a pass that decides and a pass that writes,
// and the things that can go wrong in that shape are counting the same chunk
// twice and writing after a refusal.

func olderProfile() string {
	return filepath.Join("..", "..", "profiles", "minecraft-java-1.21.11.json")
}

func newerProfile() string {
	return filepath.Join("..", "..", "profiles", "minecraft-java-26.2.json")
}

func translationFor(t *testing.T, policy string) *translation {
	t.Helper()
	conversion, err := newTranslation("minecraft:overworld", translationOptions{
		profilePath: olderProfile(),
		policyName:  policy,
		filler:      translate.DefaultFiller,
		fillerBiome: translate.DefaultBiomeFiller,
	})
	if err != nil {
		t.Fatal(err)
	}
	return conversion
}

// A chunk of ordinary stone, which every release can represent, plus one block
// entity, which none of them carry across.
func plainChunk(t *testing.T, x, z int32) anvil.PreparedChunk {
	t.Helper()
	states := make([]mcjava.BlockState, mcjava.BlockCount)
	for position := range states {
		states[position] = mcjava.BlockState{Name: "minecraft:stone"}
	}
	encoded, err := mcjava.EncodeBlockSection(-4, states)
	if err != nil {
		t.Fatal(err)
	}
	section, err := mcjava.DecodeBlockSection(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return anvil.PreparedChunk{
		Chunk: model.ChunkRef{ServerID: "example", Dimension: "minecraft:overworld", X: x, Z: z},
		Components: anvil.ChunkComponents{
			Shape:            mcjava.Shape{MinSectionY: -4, SectionCount: 24},
			Blocks:           map[int32]mcjava.BlockSection{-4: section},
			Biomes:           map[int32]mcjava.BiomeSection{},
			HasBlockEntities: true,
			BlockEntities: []mcjava.BlockEntity{
				{LocalX: 0, BlockY: -64, LocalZ: 0, Type: "minecraft:chest"},
			},
		},
	}
}

// Only the report policy can decide, after seeing everything, that nothing
// should be written. That is what buys it the second pass, and what the other
// two must not be charged for.
func TestOnlyTheReportPolicyCanRefuse(t *testing.T) {
	for policy, refuses := range map[string]bool{
		"report":     true,
		"skip-chunk": false,
		"fill":       false,
	} {
		if got := translationFor(t, policy).canRefuse(); got != refuses {
			t.Errorf("policy %s: canRefuse = %v, want %v", policy, got, refuses)
		}
	}
}

// The deciding pass and the writing pass translate the same chunks. Without
// restart between them the report would say the dimension holds twice the
// chunks it holds and would list every loss twice.
func TestRestartForgetsWhatTheDecidingPassCounted(t *testing.T) {
	conversion := translationFor(t, "fill")
	chunks := []anvil.PreparedChunk{plainChunk(t, 0, 0), plainChunk(t, 1, 1)}

	if _, err := conversion.chunks(chunks); err != nil {
		t.Fatal(err)
	}
	first := conversion.translator.Report().Chunks
	firstDropped := conversion.droppedBlockEntities
	if first != 2 || firstDropped != 2 {
		t.Fatalf("the first pass counted %d chunk(s) and %d block entit(ies); want 2 and 2", first, firstDropped)
	}

	// Without restart, translating them again accumulates.
	if _, err := conversion.chunks(chunks); err != nil {
		t.Fatal(err)
	}
	if got := conversion.translator.Report().Chunks; got != 4 {
		t.Fatalf("a shared translator counted %d chunk(s) over two passes; want 4, which is what restart exists to prevent", got)
	}

	if err := conversion.restart(); err != nil {
		t.Fatal(err)
	}
	if got := conversion.translator.Report().Chunks; got != 0 {
		t.Errorf("after restart the report still counts %d chunk(s)", got)
	}
	if conversion.droppedBlockEntities != 0 {
		t.Errorf("after restart it still counts %d dropped block entit(ies)", conversion.droppedBlockEntities)
	}

	if _, err := conversion.chunks(chunks); err != nil {
		t.Fatal(err)
	}
	if got := conversion.translator.Report().Chunks; got != 2 {
		t.Errorf("the writing pass counted %d chunk(s); want the 2 that are there", got)
	}
}

// Dropping every chest, sign and furnace in a world is a loss, and the report
// policy's whole purpose is to write nothing when there is one. The translator
// never sees a block entity, so this check lives outside it and is easy to lose.
func TestDroppedBlockEntitiesStopTheReportPolicy(t *testing.T) {
	conversion := translationFor(t, "report")
	if _, err := conversion.chunks([]anvil.PreparedChunk{plainChunk(t, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	err := conversion.verdict()
	if err == nil {
		t.Fatal("a conversion that dropped a block entity was cleared to write")
	}
	for _, wanted := range []string{"block entit", "nothing was written", "--keep-block-entities"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("the refusal does not mention %q: %v", wanted, err)
		}
	}
}

// And the policies that exist to write anyway are not stopped by it.
func TestDroppedBlockEntitiesDoNotStopTheOtherPolicies(t *testing.T) {
	for _, policy := range []string{"skip-chunk", "fill"} {
		conversion := translationFor(t, policy)
		if _, err := conversion.chunks([]anvil.PreparedChunk{plainChunk(t, 0, 0)}); err != nil {
			t.Fatal(err)
		}
		if conversion.droppedBlockEntities != 1 {
			t.Fatalf("policy %s dropped %d block entit(ies); want 1", policy, conversion.droppedBlockEntities)
		}
		if err := conversion.verdict(); err != nil {
			t.Errorf("policy %s refused over a dropped block entity: %v", policy, err)
		}
	}
}

// Asked to keep them, it keeps them, and then there is nothing to refuse over.
func TestKeepingBlockEntitiesLeavesNothingToRefuseOver(t *testing.T) {
	conversion, err := newTranslation("minecraft:overworld", translationOptions{
		profilePath:       olderProfile(),
		policyName:        "report",
		filler:            translate.DefaultFiller,
		fillerBiome:       translate.DefaultBiomeFiller,
		keepBlockEntities: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := conversion.chunks([]anvil.PreparedChunk{plainChunk(t, 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !out[0].Components.HasBlockEntities {
		t.Fatal("the block entities were dropped although they were asked to be kept")
	}
	if conversion.droppedBlockEntities != 0 {
		t.Errorf("it counted %d dropped block entit(ies) while keeping them", conversion.droppedBlockEntities)
	}
	if err := conversion.verdict(); err != nil {
		t.Errorf("it refused although nothing was dropped: %v", err)
	}
}

// The data version stamped into the converted world comes from the profile that
// was asked for, and is known before a single chunk has been read. The old code
// said otherwise in a comment and took it from the end of the translation.
func TestTheTargetDataVersionIsKnownBeforeAnythingIsTranslated(t *testing.T) {
	older := translationFor(t, "report")
	newer, err := newTranslation("minecraft:overworld", translationOptions{
		profilePath: newerProfile(),
		policyName:  "report",
		filler:      translate.DefaultFiller,
		fillerBiome: translate.DefaultBiomeFiller,
	})
	if err != nil {
		t.Fatal(err)
	}
	if older.profile.DataVersion == 0 || newer.profile.DataVersion == 0 {
		t.Fatal("a profile carries no data version")
	}
	if older.profile.DataVersion >= newer.profile.DataVersion {
		t.Errorf("the older profile is at data version %d and the newer at %d",
			older.profile.DataVersion, newer.profile.DataVersion)
	}
}
