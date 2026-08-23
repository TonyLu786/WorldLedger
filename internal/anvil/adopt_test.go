package anvil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// A region file holds up to 1,024 chunks and an export usually has a handful of
// them. Laying the file out from what it was given and writing it over the old
// one therefore deleted up to 1,023 chunks nobody had asked about, and it did so
// silently, because a shorter file is not an error.
//
// This was found by somebody exporting into a world they had made two weeks
// earlier: the spawn area Minecraft had generated was gone afterwards, and the
// world's own point-of-interest data still named chunks that no longer existed.
// Nothing in the archive had observed those chunks, which is exactly why it had
// no standing to remove them.

// world makes a directory an export will accept. level.dat is never generated
// by this package, so a test that wants one has to put it there.
func world(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "level.dat"), []byte{0x0a, 0x00, 0x00, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func preparedAt(t *testing.T, x, z int32) PreparedChunk {
	t.Helper()
	return PreparedChunk{Chunk: model.ChunkRef{X: x, Z: z}, Components: testComponents(t)}
}

// somebodyElsesRegion stands in for what the game wrote: a region file holding
// chunks this archive has never seen.
func somebodyElsesRegion(t *testing.T, regionX, regionZ int32, at ...[2]int32) []byte {
	t.Helper()
	region := NewRegion(regionX, regionZ)
	for _, position := range at {
		chunk, err := BuildChunk(position[0], position[1], DataVersion26_2, testComponents(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := region.AddChunk(position[0], position[1], chunk); err != nil {
			t.Fatal(err)
		}
	}
	return region.Bytes()
}

func chunksIn(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < headerSectors*sectorBytes {
		t.Fatalf("%s is %d bytes, too short to be a region file", path, len(raw))
	}
	count := 0
	for slot := 0; slot < regionChunks; slot++ {
		entry := slot * 4
		if raw[entry] != 0 || raw[entry+1] != 0 || raw[entry+2] != 0 {
			count++
		}
	}
	return count
}

// The defect, stated as the thing that must not happen again.
func TestWritingOneChunkDoesNotDeleteTheRestOfTheRegion(t *testing.T) {
	dir := world(t)
	regionDir, err := DimensionDirectory(dir, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(regionDir, RegionFileName(0, 0))

	// Twelve chunks the game generated, none of them ours.
	theirs := [][2]int32{}
	for i := int32(10); i < 22; i++ {
		theirs = append(theirs, [2]int32{i, i})
	}
	if err := os.WriteFile(path, somebodyElsesRegion(t, 0, 0, theirs...), 0o644); err != nil {
		t.Fatal(err)
	}
	if before := chunksIn(t, path); before != 12 {
		t.Fatalf("the fixture holds %d chunk(s), want 12", before)
	}

	report, err := Export([]PreparedChunk{preparedAt(t, 1, 1), preparedAt(t, 2, 2)}, ExportRequest{
		WorldDir:    dir,
		Dimension:   "minecraft:overworld",
		DataVersion: DataVersion26_2,
		Overwrite:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if after := chunksIn(t, path); after != 14 {
		t.Errorf("the region holds %d chunk(s) after writing 2 into a file that had 12, want 14", after)
	}
	if report.Kept != 12 {
		t.Errorf("report says %d chunk(s) were kept, want 12", report.Kept)
	}
	if report.Chunks != 2 {
		t.Errorf("report says %d chunk(s) were written, want 2", report.Chunks)
	}
}

// Ours wins where they meet. Anything else would make an export unable to
// correct what it had written before.
func TestAChunkThisExportHasReplacesTheOneThatWasThere(t *testing.T) {
	dir := world(t)
	regionDir, err := DimensionDirectory(dir, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(regionDir, RegionFileName(0, 0))
	if err := os.WriteFile(path, somebodyElsesRegion(t, 0, 0, [2]int32{3, 3}, [2]int32{9, 9}), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Export([]PreparedChunk{preparedAt(t, 3, 3)}, ExportRequest{
		WorldDir:    dir,
		Dimension:   "minecraft:overworld",
		DataVersion: DataVersion26_2,
		Overwrite:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if after := chunksIn(t, path); after != 2 {
		t.Errorf("the region holds %d chunk(s), want the one replaced and the one kept", after)
	}
	if report.Kept != 1 {
		t.Errorf("report says %d chunk(s) were kept, want 1: (3,3) is ours and (9,9) is not", report.Kept)
	}
}

// A file that cannot be understood is the one case where carrying on would
// destroy what it holds, so it stops instead. The message has to name the file,
// because "export failed" leaves somebody with no idea which world to look at.
func TestARegionFileThatCannotBeReadStopsTheExport(t *testing.T) {
	dir := world(t)
	regionDir, err := DimensionDirectory(dir, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(regionDir, RegionFileName(0, 0))
	damaged := somebodyElsesRegion(t, 0, 0, [2]int32{7, 7})
	// A slot pointing past the end of the file.
	slot := regionSlot(7, 7) * 4
	damaged[slot] = 0xff
	if err := os.WriteFile(path, damaged, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Export([]PreparedChunk{preparedAt(t, 1, 1)}, ExportRequest{
		WorldDir:    dir,
		Dimension:   "minecraft:overworld",
		DataVersion: DataVersion26_2,
		Overwrite:   true,
	}); err == nil {
		t.Fatal("a region file that could not be read was replaced rather than refused")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("the file changed from %d to %d bytes; a refused export must leave it alone", len(before), len(after))
	}
}

func TestAnAdoptedChunkKeepsItsOwnBytes(t *testing.T) {
	original := somebodyElsesRegion(t, 0, 0, [2]int32{4, 4})

	region := NewRegion(0, 0)
	chunk, err := BuildChunk(5, 5, DataVersion26_2, testComponents(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := region.AddChunk(5, 5, chunk); err != nil {
		t.Fatal(err)
	}
	kept, err := region.Adopt(original)
	if err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatalf("kept %d, want 1", kept)
	}

	// The adopted frame is compared against what it was, uncompressed and
	// unparsed. An export that re-encodes somebody else's chunk has formed an
	// opinion about data it never observed.
	theirSlot := regionSlot(4, 4)
	from := int(original[theirSlot*4])<<16 | int(original[theirSlot*4+1])<<8 | int(original[theirSlot*4+2])
	length := int(original[from*sectorBytes])<<24 | int(original[from*sectorBytes+1])<<16 |
		int(original[from*sectorBytes+2])<<8 | int(original[from*sectorBytes+3])
	want := original[from*sectorBytes : from*sectorBytes+4+length]
	got := region.payload[theirSlot]
	if string(got) != string(want) {
		t.Errorf("the adopted frame is %d bytes and was %d; it was not copied through", len(got), len(want))
	}
}

func TestAdoptingNothingIsNotAnError(t *testing.T) {
	region := NewRegion(0, 0)
	if kept, err := region.Adopt(nil); err != nil || kept != 0 {
		t.Errorf("adopting an absent file returned %d, %v", kept, err)
	}
	if _, err := region.Adopt([]byte{1, 2, 3}); err == nil {
		t.Error("a file too short to hold a header was accepted")
	}
}
