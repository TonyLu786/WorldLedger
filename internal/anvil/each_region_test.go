package anvil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// EachRegion is the half of a conversion that decides whether the result may be
// written at all, so what matters about it is that it sees everything and holds
// one region while it does.

func TestEachRegionSeesEveryChunkOneRegionAtATime(t *testing.T) {
	source, sources, _ := newSourceForTest(t)

	var visits [][2]int32
	seen := 0
	err := EachRegion(source, sources, func(prepared []PreparedChunk) error {
		if len(prepared) == 0 {
			t.Fatal("a region was visited with no chunks")
		}
		regionX, regionZ := RegionOf(prepared[0].Chunk.X, prepared[0].Chunk.Z)
		for _, entry := range prepared[1:] {
			x, z := RegionOf(entry.Chunk.X, entry.Chunk.Z)
			if x != regionX || z != regionZ {
				t.Fatalf("one visit mixed regions (%d,%d) and (%d,%d)", regionX, regionZ, x, z)
			}
		}
		visits = append(visits, [2]int32{regionX, regionZ})
		seen += len(prepared)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != len(sources) {
		t.Errorf("visited %d chunks of %d", seen, len(sources))
	}

	// One visit per region, not one per chunk: holding a region at a time is
	// the entire reason this exists, and a visitor called per chunk would pass
	// every other assertion here.
	distinct := map[[2]int32]int{}
	for _, region := range visits {
		distinct[region]++
	}
	if len(distinct) != len(visits) {
		t.Errorf("%d visits over %d regions; a region was visited more than once", len(visits), len(distinct))
	}
	if len(distinct) < 2 {
		t.Fatalf("the fixture covers %d region(s); this proves nothing about grouping", len(distinct))
	}
}

func TestEachRegionStopsAtTheFirstRefusal(t *testing.T) {
	source, sources, _ := newSourceForTest(t)

	refusal := errors.New("no")
	visits := 0
	err := EachRegion(source, sources, func([]PreparedChunk) error {
		visits++
		return refusal
	})
	if !errors.Is(err, refusal) {
		t.Fatalf("err = %v, want the visitor's own error", err)
	}
	if visits != 1 {
		t.Errorf("it carried on after a refusal: %d visits", visits)
	}
}

// A conversion under skip-chunk can drop every chunk a region held. Writing the
// file anyway would put an empty region where there had been none and then name
// it as a file the export wrote.
func TestARegionLeftEmptyByATransformIsNotWritten(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	// Everything in region (0,0) goes; the other regions are untouched.
	report, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903,
	}, func(prepared []PreparedChunk) ([]PreparedChunk, error) {
		kept := make([]PreparedChunk, 0, len(prepared))
		for _, entry := range prepared {
			if x, z := RegionOf(entry.Chunk.X, entry.Chunk.Z); x == 0 && z == 0 {
				continue
			}
			kept = append(kept, entry)
		}
		return kept, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	written := regionFilesUnder(t, world, "minecraft:overworld")
	if _, exists := written["r.0.0.mca"]; exists {
		t.Error("an empty region file was written for a region the transform emptied")
	}
	for _, path := range report.RegionFiles {
		if filepath.Base(path) == "r.0.0.mca" {
			t.Error("the report names a region file that was not written")
		}
	}
	if len(report.RegionFiles) != len(written) {
		t.Errorf("the report names %d file(s) and %d were written", len(report.RegionFiles), len(written))
	}
	if len(written) == 0 {
		t.Fatal("nothing at all was written; this proves nothing")
	}
}

// And a region file that was already there keeps what it held, rather than
// being rewritten to say the same thing or replaced by an empty one.
func TestATransformThatEmptiesARegionLeavesAnExistingFileAlone(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write it once with everything, to have a real region file on disk.
	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903,
	}, nil); err != nil {
		t.Fatal(err)
	}
	before := regionFilesUnder(t, world, "minecraft:overworld")
	if _, exists := before["r.0.0.mca"]; !exists {
		t.Fatal("the fixture does not cover region (0,0)")
	}

	// Now write again with everything dropped.
	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903, Overwrite: true,
	}, func([]PreparedChunk) ([]PreparedChunk, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}

	after := regionFilesUnder(t, world, "minecraft:overworld")
	if len(after) != len(before) {
		t.Fatalf("%d region file(s) became %d", len(before), len(after))
	}
	for name, content := range before {
		if string(after[name]) != string(content) {
			t.Errorf("%s changed although the transform wrote nothing into it", name)
		}
	}
}
