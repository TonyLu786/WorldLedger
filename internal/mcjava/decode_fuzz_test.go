package mcjava

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The canonical decoders accept bytes that originate from an untrusted
// multiplayer server by way of an untrusted contributor. Accepting a byte
// sequence is a claim that the sequence is canonical, so anything that decodes
// must re-encode to exactly the same bytes.

func seedFromGolden(f *testing.F, names ...string) {
	f.Helper()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mcjava-v1", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
}

func FuzzDecodeShape(f *testing.F) {
	seedFromGolden(f, "shape-negative.bin")
	f.Fuzz(func(t *testing.T, data []byte) {
		shape, err := DecodeShape(data)
		if err != nil {
			return
		}
		reencoded, err := EncodeShape(shape.MinSectionY, shape.SectionCount)
		if err != nil {
			t.Fatalf("decoded shape failed to re-encode: %v", err)
		}
		if !bytes.Equal(reencoded, data) {
			t.Fatal("decoder accepted non-canonical shape bytes")
		}
	})
}

func FuzzDecodeBlockSection(f *testing.F) {
	seedFromGolden(f, "blocks-all-air-negative.bin", "blocks-property-order.bin")
	air := make([]BlockState, BlockCount)
	for position := range air {
		air[position] = BlockState{Name: "minecraft:air"}
	}
	minimal, err := EncodeBlockSection(0, air)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(minimal)
	f.Fuzz(func(t *testing.T, data []byte) {
		section, err := DecodeBlockSection(data)
		if err != nil {
			return
		}
		states, err := section.ParsedStates()
		if err != nil {
			t.Fatalf("decoded block section failed to parse: %v", err)
		}
		reencoded, err := EncodeBlockSection(section.SectionY, states)
		if err != nil {
			t.Fatalf("decoded block section failed to re-encode: %v", err)
		}
		if !bytes.Equal(reencoded, data) {
			t.Fatal("decoder accepted non-canonical block section bytes")
		}
	})
}

func FuzzDecodeBiomeSection(f *testing.F) {
	seedFromGolden(f, "biomes-mixed-negative.bin")
	f.Fuzz(func(t *testing.T, data []byte) {
		section, err := DecodeBiomeSection(data)
		if err != nil {
			return
		}
		reencoded, err := EncodeBiomeSection(section.SectionY, section.Biomes())
		if err != nil {
			t.Fatalf("decoded biome section failed to re-encode: %v", err)
		}
		if !bytes.Equal(reencoded, data) {
			t.Fatal("decoder accepted non-canonical biome section bytes")
		}
	})
}

func FuzzDecodeBlockEntities(f *testing.F) {
	seedFromGolden(f, "block-entities-empty.bin", "block-entities-nbt-special.bin")
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, err := DecodeBlockEntities(data)
		if err != nil {
			return
		}
		reencoded, err := EncodeBlockEntities(entries)
		if err != nil {
			t.Fatalf("decoded block entities failed to re-encode: %v", err)
		}
		if !bytes.Equal(reencoded, data) {
			t.Fatal("decoder accepted non-canonical block entity bytes")
		}
	})
}

func FuzzDecodeNBT(f *testing.F) {
	f.Add([]byte{uint8(TagCompound), 0, 0, 0, 0})
	f.Add([]byte{uint8(TagByte), 7})
	f.Add([]byte{uint8(TagList), uint8(TagEnd), 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := DecodeNBT(data)
		if err != nil {
			return
		}
		reencoded, err := EncodeNBT(value)
		if err != nil {
			t.Fatalf("decoded NBT failed to re-encode: %v", err)
		}
		if !bytes.Equal(reencoded, data) {
			t.Fatal("decoder accepted non-canonical NBT bytes")
		}
	})
}
