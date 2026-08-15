package archive

import (
	"fmt"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// Import spends its time in three places: reading and hashing the bundle,
// writing objects, and committing the observation. The first two have been
// measured in internal/bundle. This is the third.
//
// A commit writes a transaction record, the observation, and the chunk index,
// forcing each to disk, and that count does not change with how many components
// an observation references. If the remaining cost of an import is here, it is
// a fixed cost per bundle rather than a per-component one, and the way to
// reduce it is to commit fewer times rather than to commit faster.
func benchmarkObservation(components int, minute int) model.Observation {
	refs := make(map[string]model.BlobRef, components)
	for index := 0; index < components; index++ {
		digest := fmt.Sprintf("%064x", index+1)
		refs[fmt.Sprintf("mcjava.blocks.%d", index)] = model.BlobRef{
			Algorithm: "sha256", Digest: digest, Size: 8262,
		}
	}
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: "s", Dimension: "minecraft:overworld", X: 0, Z: int32(minute)},
		ObservedAt: time.Date(2026, 8, 9, 12, minute%60, 0, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: "benchmark"},
		Components: refs,
	}
	if err := o.Finalize(); err != nil {
		panic(err)
	}
	return o
}

func BenchmarkAddObservation(b *testing.B) {
	a, err := Init(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	observations := make([]model.Observation, b.N)
	for i := range observations {
		observations[i] = benchmarkObservation(50, i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.AddObservation(observations[i]); err != nil {
			b.Fatal(err)
		}
	}
}

// The same commit into a chunk that already holds observations, which is what
// a second contributor's import does. The index is rewritten rather than
// created, and it grows.
func BenchmarkAddObservationToABusyChunk(b *testing.B) {
	a, err := Init(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		o := benchmarkObservation(50, i)
		o.Chunk.Z = 0
		if err := o.Finalize(); err != nil {
			b.Fatal(err)
		}
		if err := a.AddObservation(o); err != nil {
			b.Fatal(err)
		}
	}

	observations := make([]model.Observation, b.N)
	for i := range observations {
		o := benchmarkObservation(50, 1000+i)
		o.Chunk.Z = 0
		if err := o.Finalize(); err != nil {
			b.Fatal(err)
		}
		observations[i] = o
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.AddObservation(observations[i]); err != nil {
			b.Fatal(err)
		}
	}
}
