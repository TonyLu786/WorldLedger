package mcjava

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The adapter canonicalizes on a background thread while the client keeps
// running, so what matters is how long one chunk's worth of work takes and how
// much it allocates. These benchmarks measure the reference implementation of
// that work against the committed golden bytes and against a full-height chunk
// of the size the adapter actually handles.
//
// The guardrails in docs/test-strategy.md ask for canonicalization and spool
// throughput to be measured separately. This file is the canonicalization half.

func readFixture(b *testing.B, name string) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mcjava-v1", name))
	if err != nil {
		b.Fatal(err)
	}
	return data
}

func BenchmarkDecodeBlockSection(b *testing.B) {
	for _, name := range []string{"blocks-all-air-negative.bin", "blocks-high-palette.bin", "blocks-property-order.bin"} {
		data := readFixture(b, name)
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := DecodeBlockSection(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeBiomeSection(b *testing.B) {
	data := readFixture(b, "biomes-mixed-negative.bin")
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeBiomeSection(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeBlockEntities(b *testing.B) {
	data := readFixture(b, "block-entities-nbt-special.bin")
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeBlockEntities(data); err != nil {
			b.Fatal(err)
		}
	}
}

// blockSectionStates builds one section's worth of states across a chosen
// palette size. A section is 16x16x16, and palette size is what decides the bit
// width of the packed indices, so it is the parameter that moves the cost.
func blockSectionStates(paletteSize int) []BlockState {
	states := make([]BlockState, 4096)
	for i := range states {
		states[i] = BlockState{Name: fmt.Sprintf("minecraft:test_block_%d", i%paletteSize)}
	}
	return states
}

func BenchmarkEncodeBlockSection(b *testing.B) {
	for _, paletteSize := range []int{1, 16, 256} {
		states := blockSectionStates(paletteSize)
		b.Run(fmt.Sprintf("palette-%d", paletteSize), func(b *testing.B) {
			b.ReportAllocs()
			encoded, err := EncodeBlockSection(0, states)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(encoded)))
			for i := 0; i < b.N; i++ {
				if _, err := EncodeBlockSection(0, states); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A full-height overworld chunk is 24 sections from Y=-4. The adapter's queue
// sizing assumes roughly 214 KiB for one, so this is the unit that the
// in-flight memory bound is expressed in.
func BenchmarkEncodeFullHeightChunk(b *testing.B) {
	const minSectionY = -4
	const sectionCount = 24
	states := blockSectionStates(64)

	var encodedBytes int64
	shape, err := EncodeShape(minSectionY, sectionCount)
	if err != nil {
		b.Fatal(err)
	}
	encodedBytes += int64(len(shape))
	for section := 0; section < sectionCount; section++ {
		encoded, err := EncodeBlockSection(int32(minSectionY+section), states)
		if err != nil {
			b.Fatal(err)
		}
		encodedBytes += int64(len(encoded))
	}
	b.Logf("one full-height chunk encodes to %d bytes across %d sections", encodedBytes, sectionCount)

	b.ResetTimer()
	b.SetBytes(encodedBytes)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := EncodeShape(minSectionY, sectionCount); err != nil {
			b.Fatal(err)
		}
		for section := 0; section < sectionCount; section++ {
			if _, err := EncodeBlockSection(int32(minSectionY+section), states); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Decoding validates canonical form rather than accepting anything parseable,
// so it is measured separately from encoding rather than assumed symmetric.
func BenchmarkRoundTripBlockSection(b *testing.B) {
	states := blockSectionStates(64)
	encoded, err := EncodeBlockSection(0, states)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		decoded, err := DecodeBlockSection(encoded)
		if err != nil {
			b.Fatal(err)
		}
		parsed, err := decoded.ParsedStates()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := EncodeBlockSection(0, parsed); err != nil {
			b.Fatal(err)
		}
	}
}
