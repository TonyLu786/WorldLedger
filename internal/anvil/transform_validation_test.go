package anvil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The validation pass skips the slots the export is about to overwrite, because
// refusing a file over a chunk that is about to be replaced would refuse
// exports that are perfectly safe.
//
// A transform breaks that reasoning. It is allowed to drop chunks, and a slot
// the export intended to overwrite but did not is a slot nobody validated and
// the writing pass then tries to adopt.

// damageChunk rewrites one chunk's frame length to zero, which is what a
// region file looks like after the sort of damage vanilla repairs by zeroing
// the slot. Adopt refuses it; that is the behaviour being relied on here.
func damageChunk(t *testing.T, path string, chunkX, chunkZ int32) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := regionSlot(chunkX, chunkZ) * 4
	offset := int(data[entry])<<16 | int(data[entry+1])<<8 | int(data[entry+2])
	if offset < headerSectors {
		t.Fatalf("chunk (%d,%d) is not in %s", chunkX, chunkZ, filepath.Base(path))
	}
	start := offset * sectorBytes
	data[start], data[start+1], data[start+2], data[start+3] = 0, 0, 0, 0
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestATransformThatDropsAChunkCannotBlameTheFile(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903,
	}, nil); err != nil {
		t.Fatal(err)
	}
	regionDir, err := DimensionDirectory(world, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	// (1,1) and (2,2) share region (0,0); the damage goes on the one the
	// transform will drop.
	damageChunk(t, filepath.Join(regionDir, "r.0.0.mca"), 1, 1)

	_, err = ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903, Overwrite: true,
	}, func(prepared []PreparedChunk) ([]PreparedChunk, error) {
		kept := make([]PreparedChunk, 0, len(prepared))
		for _, entry := range prepared {
			if entry.Chunk.X == 1 && entry.Chunk.Z == 1 {
				continue
			}
			kept = append(kept, entry)
		}
		return kept, nil
	})
	if err == nil {
		t.Fatal("a damaged region file was adopted without complaint")
	}
	if strings.Contains(err.Error(), "changed while the export was running") {
		t.Errorf("it blamed the file for changing, which it did not: %v", err)
	}
	if !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("the refusal does not say the file could not be read: %v", err)
	}
}

// And the refusal has to come before anything is written, which is the property
// the validation pass exists for.
func TestADamagedFileUnderATransformStopsTheExportBeforeAnythingIsWritten(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903,
	}, nil); err != nil {
		t.Fatal(err)
	}
	regionDir, err := DimensionDirectory(world, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture covers regions (-1,-1), (0,0) and (1,0), written in that
	// order, and the damage goes in the last of them. Comparing the earlier
	// files before and after would prove nothing: the same inputs produce the
	// same bytes, so a late refusal would rewrite them identically and look
	// like no write at all. They are deleted instead, and a file that comes
	// back is a file that was written before the refusal.
	damageChunk(t, filepath.Join(regionDir, "r.1.0.mca"), 40, 3)
	for _, name := range []string{"r.-1.-1.mca", "r.0.0.mca"} {
		if err := os.Remove(filepath.Join(regionDir, name)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903, Overwrite: true,
	}, func(prepared []PreparedChunk) ([]PreparedChunk, error) {
		kept := make([]PreparedChunk, 0, len(prepared))
		for _, entry := range prepared {
			if entry.Chunk.X == 40 && entry.Chunk.Z == 3 {
				continue
			}
			kept = append(kept, entry)
		}
		return kept, nil
	}); err == nil {
		t.Fatal("the damaged file was accepted")
	}

	for name := range regionFilesUnder(t, world, "minecraft:overworld") {
		if name != "r.1.0.mca" {
			t.Errorf("%s was written before the export refused", name)
		}
	}
}

// Without a transform nothing can drop a chunk, so the precise behaviour stays:
// a damaged frame in a slot this export is going to replace does not refuse the
// export.
func TestWithoutATransformADamagedSlotAboutToBeReplacedIsStillFine(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903,
	}, nil); err != nil {
		t.Fatal(err)
	}
	regionDir, err := DimensionDirectory(world, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	damageChunk(t, filepath.Join(regionDir, "r.0.0.mca"), 1, 1)

	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903, Overwrite: true,
	}, nil); err != nil {
		t.Fatalf("an export that replaces the damaged chunk was refused: %v", err)
	}
}
