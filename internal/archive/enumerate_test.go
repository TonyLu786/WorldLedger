package archive

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func chunkObservation(t *testing.T, serverID, dimension string, x, z int32, contributor string) model.Observation {
	t.Helper()
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: serverID, Dimension: dimension, X: x, Z: z},
		ObservedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{
			"chunk": {Algorithm: "sha256", Digest: strings.Repeat("a", 64), Size: 1},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	return o
}

func populatedArchive(t *testing.T) Archive {
	t.Helper()
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	observations := []model.Observation{
		chunkObservation(t, "Example.ORG", "Overworld", 1, 2, "alice"),
		chunkObservation(t, "example.org", "overworld", 1, 3, "alice"),
		chunkObservation(t, "example.org", "overworld", -5, 7, "bob"),
		chunkObservation(t, "example.org", "minecraft:the_nether", 0, 0, "alice"),
		chunkObservation(t, "other.net", "overworld", 0, 0, "carol"),
	}
	for _, o := range observations {
		if err := a.AddObservation(o); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestEnumerationReportsNormalizedIdentities(t *testing.T) {
	a := populatedArchive(t)

	servers, err := a.Servers()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(servers, []string{"example.org", "other.net"}) {
		t.Fatalf("servers = %#v", servers)
	}

	dimensions, err := a.Dimensions("Example.ORG")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dimensions, []string{"minecraft:the_nether", "overworld"}) {
		t.Fatalf("dimensions = %#v", dimensions)
	}
}

func TestChunksAreSortedAndPreserveNegativeCoordinates(t *testing.T) {
	a := populatedArchive(t)

	chunks, err := a.Chunks("example.org", "overworld")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ChunkRef{
		{ServerID: "example.org", Dimension: "overworld", X: -5, Z: 7},
		{ServerID: "example.org", Dimension: "overworld", X: 1, Z: 2},
		{ServerID: "example.org", Dimension: "overworld", X: 1, Z: 3},
	}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %#v; want %#v", chunks, want)
	}
}

// DimensionObservations reads the index and every chunk under one lock. The
// archive lock is not reentrant, so a nested acquisition would hang here rather
// than fail.
func TestDimensionObservationsGathersEveryChunkUnderOneLock(t *testing.T) {
	a := populatedArchive(t)

	done := make(chan []ChunkObservations, 1)
	go func() {
		gathered, err := a.DimensionObservations("example.org", "overworld")
		if err != nil {
			t.Error(err)
			done <- nil
			return
		}
		done <- gathered
	}()

	select {
	case gathered := <-done:
		if len(gathered) != 3 {
			t.Fatalf("gathered %d chunks; want 3", len(gathered))
		}
		for _, entry := range gathered {
			if len(entry.Observations) != 1 {
				t.Fatalf("chunk %#v has %d observations; want 1", entry.Chunk, len(entry.Observations))
			}
			if entry.Observations[0].Chunk != entry.Chunk {
				t.Fatalf("observation is filed under the wrong chunk: %#v", entry)
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("DimensionObservations deadlocked against its own archive lock")
	}
}

func TestEnumerationOfAbsentIdentitiesIsEmpty(t *testing.T) {
	a := populatedArchive(t)

	dimensions, err := a.Dimensions("absent.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(dimensions) != 0 {
		t.Fatalf("dimensions = %#v; want none", dimensions)
	}

	chunks, err := a.Chunks("example.org", "absent")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("chunks = %#v; want none", chunks)
	}

	gathered, err := a.DimensionObservations("absent.example", "absent")
	if err != nil {
		t.Fatal(err)
	}
	if len(gathered) != 0 {
		t.Fatalf("gathered = %#v; want none", gathered)
	}
}

func TestEmptyArchiveEnumeratesWithoutError(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := a.Servers()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("servers = %#v; want none", servers)
	}
}
