package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

// The committed bundle carries four components. One from a real session carries
// about fifty: twenty-four block sections, twenty-four biome sections, a shape
// and a block-entity component. Importing 158 of those measured 2 minutes 25
// seconds, or roughly 918 ms each, against a benchmark figure of 37 ms taken
// from the four-component fixture.
//
// These pin that difference to its cause. If import cost tracks components
// rather than bundles, the two sub-benchmarks differ by roughly the ratio of
// their component counts, and any change that claims to make import faster has
// to move the fifty-component number.
const realisticComponentCount = 50

// realisticComponentBytes is the size of a block section from a real capture.
const realisticComponentBytes = 8262

func benchmarkBundle(tb testing.TB, components int) string {
	return benchmarkBundleWith(tb, components, true)
}

// benchmarkBundleWith builds a bundle whose components are either all different
// or all the same. Identical components collapse to one object in the store, so
// the difference between the two isolates what the object store costs from what
// the rest of import costs.
func benchmarkBundleWith(tb testing.TB, components int, distinct bool) string {
	tb.Helper()
	directory := filepath.Join(tb.TempDir(), "ready-benchmark-00000000000000000001")
	componentsDirectory := filepath.Join(directory, "components")
	if err := os.MkdirAll(componentsDirectory, 0o755); err != nil {
		tb.Fatal(err)
	}

	descriptors := make(map[string]any, components)
	for index := 0; index < components; index++ {
		// Distinct bytes per component, so the object store cannot collapse
		// them and measure something easier than the real case.
		payload := make([]byte, realisticComponentBytes)
		seed := index
		if !distinct {
			seed = 0
		}
		for offset := range payload {
			payload[offset] = byte(offset + seed)
		}
		name := fmt.Sprintf("mcjava.blocks.%d", index)
		file := fmt.Sprintf("component-%03d.bin", index)
		if err := os.WriteFile(filepath.Join(componentsDirectory, file), payload, 0o644); err != nil {
			tb.Fatal(err)
		}
		sum := sha256.Sum256(payload)
		descriptors[name] = map[string]any{
			"path":      "components/" + file,
			"algorithm": "sha256",
			"digest":    hex.EncodeToString(sum[:]),
			"size":      int64(len(payload)),
		}
	}

	manifest := map[string]any{
		"schema":         Schema,
		"server_id":      "example.org:25565",
		"server_address": "example.org:25565",
		"dimension":      "minecraft:overworld",
		"chunk":          map[string]any{"x": int32(0), "z": int32(0)},
		"observed_at":    "2026-08-09T12:00:03.123456Z",
		"protocol":       "minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1",
		"source": map[string]any{
			"contributor": "benchmark",
			"agent":       "worldledger-fabric/0.1.0-dev",
		},
		"capture": map[string]any{
			"session_id": "5dfe3db2-208e-4cd8-8d11-1d83fa4f951b",
			"sequence":   uint64(1),
			"trigger":    "dirty-flush",
		},
		"components": descriptors,
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bundle.json"), encoded, 0o644); err != nil {
		tb.Fatal(err)
	}
	return directory
}

func BenchmarkImportByComponentCount(b *testing.B) {
	for _, components := range []int{4, realisticComponentCount} {
		b.Run(fmt.Sprintf("components-%d", components), func(b *testing.B) {
			directory := benchmarkBundle(b, components)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				a, err := archive.Init(b.TempDir())
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()

				if _, err := Import(a, directory, Options{Limits: DefaultLimits()}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Fifty components that are all the same collapse to one object, so this is
// the same import work with the object store's writes taken out. What remains
// is the manifest, the safety-checked open of every component file, the
// observation record, the chunk index, and the transaction around them.
func BenchmarkImportWithoutObjectWrites(b *testing.B) {
	directory := benchmarkBundleWith(b, realisticComponentCount, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		a, err := archive.Init(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := Import(a, directory, Options{Limits: DefaultLimits()}); err != nil {
			b.Fatal(err)
		}
	}
}

// How much of the cost is the object store rather than the record and index.
// Import writes one observation and one index entry however many components it
// carries, so a difference here localises where any improvement has to come
// from.
func BenchmarkObjectStoreWrites(b *testing.B) {
	payloads := make([][]byte, realisticComponentCount)
	for index := range payloads {
		payload := make([]byte, realisticComponentBytes)
		for offset := range payload {
			payload[offset] = byte(offset + index)
		}
		payloads[index] = payload
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		a, err := archive.Init(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		for _, payload := range payloads {
			if _, err := a.CAS.Put(bytes.NewReader(payload)); err != nil {
				b.Fatal(err)
			}
		}
	}
}
