package mcjava_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/mcjava/fixture"
)

// Every committed golden component must decode and re-encode to the identical
// bytes. This is the executable form of the canonical round-trip guarantee: a
// component that decodes is proof that the stored bytes are canonical.
func TestDecodeCommittedGoldenFixturesRoundTrip(t *testing.T) {
	directory := filepath.Join("..", "..", "testdata", "mcjava-v1")
	set, err := fixture.Load(filepath.Join(directory, "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Fixtures) == 0 {
		t.Fatal("no committed fixtures to decode")
	}

	for _, item := range set.Fixtures {
		item := item
		t.Run(item.Name, func(t *testing.T) {
			committed, err := os.ReadFile(filepath.Join(directory, item.Output))
			if err != nil {
				t.Fatal(err)
			}
			reencoded, err := roundTrip(item.Component, committed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(reencoded, committed) {
				t.Fatalf("re-encoded %s is %d bytes; committed golden is %d bytes", item.Component, len(reencoded), len(committed))
			}
		})
	}
}

func roundTrip(component string, committed []byte) ([]byte, error) {
	switch component {
	case "shape":
		shape, err := mcjava.DecodeShape(committed)
		if err != nil {
			return nil, err
		}
		return mcjava.EncodeShape(shape.MinSectionY, shape.SectionCount)
	case "block_section":
		section, err := mcjava.DecodeBlockSection(committed)
		if err != nil {
			return nil, err
		}
		states, err := section.ParsedStates()
		if err != nil {
			return nil, err
		}
		return mcjava.EncodeBlockSection(section.SectionY, states)
	case "biome_section":
		section, err := mcjava.DecodeBiomeSection(committed)
		if err != nil {
			return nil, err
		}
		return mcjava.EncodeBiomeSection(section.SectionY, section.Biomes())
	case "block_entities":
		entries, err := mcjava.DecodeBlockEntities(committed)
		if err != nil {
			return nil, err
		}
		return mcjava.EncodeBlockEntities(entries)
	default:
		return nil, errUnknownComponent(component)
	}
}

type errUnknownComponent string

func (e errUnknownComponent) Error() string {
	return "unknown fixture component " + string(e)
}

func TestDecodedGoldenSectionsExposeSemanticState(t *testing.T) {
	directory := filepath.Join("..", "..", "testdata", "mcjava-v1")

	blocks, err := os.ReadFile(filepath.Join(directory, "blocks-property-order.bin"))
	if err != nil {
		t.Fatal(err)
	}
	section, err := mcjava.DecodeBlockSection(blocks)
	if err != nil {
		t.Fatal(err)
	}
	state, err := section.StateAt(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mcjava.ParseBlockState(state)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name == "" || len(parsed.Properties) == 0 {
		t.Fatalf("block state %q did not parse into a name and properties", state)
	}
	for index := 1; index < len(parsed.Properties); index++ {
		if parsed.Properties[index-1].Name >= parsed.Properties[index].Name {
			t.Fatalf("decoded properties are not in canonical order: %#v", parsed.Properties)
		}
	}

	shapeBytes, err := os.ReadFile(filepath.Join(directory, "shape-negative.bin"))
	if err != nil {
		t.Fatal(err)
	}
	shape, err := mcjava.DecodeShape(shapeBytes)
	if err != nil {
		t.Fatal(err)
	}
	if shape.MinSectionY >= 0 {
		t.Fatalf("negative shape fixture decoded min_section_y = %d", shape.MinSectionY)
	}
	sections := shape.SectionYs()
	if len(sections) != int(shape.SectionCount) {
		t.Fatalf("SectionYs returned %d entries; want %d", len(sections), shape.SectionCount)
	}
	if !shape.Contains(shape.MinSectionY) || !shape.Contains(shape.MaxSectionY()) {
		t.Fatal("shape does not contain its own bounds")
	}
	if shape.Contains(shape.MinSectionY-1) || shape.Contains(shape.MaxSectionY()+1) {
		t.Fatal("shape contains a section outside its declared range")
	}
}
