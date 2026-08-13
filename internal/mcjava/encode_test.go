package mcjava

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestCanonicalBlockStateSortsProperties(t *testing.T) {
	got, err := CanonicalBlockState(BlockState{
		Name: "minecraft:oak_stairs",
		Properties: []Property{
			{Name: "waterlogged", Value: "false"},
			{Name: "shape", Value: "straight"},
			{Name: "half", Value: "bottom"},
			{Name: "facing", Value: "north"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=false]"
	if got != want {
		t.Fatalf("state = %q; want %q", got, want)
	}
}

func TestBlockSectionWritesCallerPositionOrder(t *testing.T) {
	states := make([]BlockState, BlockCount)
	for index := range states {
		states[index] = BlockState{Name: "minecraft:air"}
	}
	states[1] = BlockState{Name: "minecraft:stone"}
	states[16] = BlockState{Name: "minecraft:dirt"}
	states[256] = BlockState{Name: "minecraft:grass_block"}
	encoded, err := EncodeBlockSection(-4, states)
	if err != nil {
		t.Fatal(err)
	}
	indices := encoded[len(encoded)-BlockCount*2:]
	wants := map[int]uint16{0: 0, 1: 3, 16: 1, 256: 2}
	for position, want := range wants {
		got := binary.BigEndian.Uint16(indices[position*2:])
		if got != want {
			t.Errorf("palette index at linear position %d = %d; want %d", position, got, want)
		}
	}
}

func TestCanonicalNBTCompoundOrderIsDeterministic(t *testing.T) {
	left := NBTValue{Type: TagCompound, Compound: []NamedNBT{
		{Name: "z", Value: NBTValue{Type: TagInt, Int: 1}},
		{Name: "a", Value: NBTValue{Type: TagInt, Int: 2}},
	}}
	right := NBTValue{Type: TagCompound, Compound: []NamedNBT{
		{Name: "a", Value: NBTValue{Type: TagInt, Int: 2}},
		{Name: "z", Value: NBTValue{Type: TagInt, Int: 1}},
	}}
	leftBytes, err := EncodeNBT(left)
	if err != nil {
		t.Fatal(err)
	}
	rightBytes, err := EncodeNBT(right)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftBytes, rightBytes) {
		t.Fatal("compound input order changed canonical bytes")
	}
}

func TestCanonicalEncodersRejectMalformedInputs(t *testing.T) {
	validCompound := NBTValue{Type: TagCompound}
	tests := []struct {
		name string
		run  func() error
	}{
		{"zero shape", func() error { _, err := EncodeShape(0, 0); return err }},
		{"overflowing shape", func() error { _, err := EncodeShape(1<<31-1, 2); return err }},
		{"short block section", func() error { _, err := EncodeBlockSection(0, make([]BlockState, BlockCount-1)); return err }},
		{"short biome section", func() error { _, err := EncodeBiomeSection(0, make([]string, BiomeCount-1)); return err }},
		{"invalid biome resource", func() error {
			biomes := make([]string, BiomeCount)
			for index := range biomes {
				biomes[index] = "minecraft:plains"
			}
			biomes[5] = "missing_namespace"
			_, err := EncodeBiomeSection(0, biomes)
			return err
		}},
		{"duplicate property", func() error {
			_, err := CanonicalBlockState(BlockState{Name: "minecraft:stone", Properties: []Property{{Name: "a", Value: "1"}, {Name: "a", Value: "2"}}})
			return err
		}},
		{"unicode whitespace property", func() error {
			_, err := CanonicalBlockState(BlockState{Name: "minecraft:stone", Properties: []Property{{Name: "a\u00a0b", Value: "1"}}})
			return err
		}},
		{"invalid local coordinate", func() error {
			_, err := EncodeBlockEntities([]BlockEntity{{LocalX: 16, Type: "minecraft:sign", NBT: validCompound}})
			return err
		}},
		{"non-compound block entity", func() error {
			_, err := EncodeBlockEntities([]BlockEntity{{Type: "minecraft:sign", NBT: NBTValue{Type: TagString, String: "x"}}})
			return err
		}},
		{"duplicate block entity key", func() error {
			entry := BlockEntity{LocalX: 1, BlockY: 2, LocalZ: 3, Type: "minecraft:sign", NBT: validCompound}
			_, err := EncodeBlockEntities([]BlockEntity{entry, entry})
			return err
		}},
		{"list element mismatch", func() error {
			_, err := EncodeNBT(NBTValue{Type: TagList, List: &NBTList{ElementType: TagInt, Values: []NBTValue{{Type: TagLong}}}})
			return err
		}},
		{"duplicate compound key", func() error {
			_, err := EncodeNBT(NBTValue{Type: TagCompound, Compound: []NamedNBT{{Name: "x", Value: NBTValue{Type: TagInt}}, {Name: "x", Value: NBTValue{Type: TagInt}}}})
			return err
		}},
		{"NBT depth", func() error {
			_, err := EncodeNBTWithLimits(
				NBTValue{Type: TagCompound, Compound: []NamedNBT{{Name: "nested", Value: NBTValue{Type: TagCompound, Compound: []NamedNBT{{Name: "too_deep", Value: NBTValue{Type: TagCompound}}}}}}},
				Limits{MaxNBTDepth: 1, MaxNBTBytes: 128, MaxComponentBytes: 128},
			)
			return err
		}},
		{"component byte limit", func() error {
			_, err := EncodeNBTWithLimits(NBTValue{Type: TagString, String: strings.Repeat("x", 20)}, Limits{MaxNBTBytes: 8, MaxComponentBytes: 8})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
