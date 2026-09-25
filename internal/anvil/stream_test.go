package anvil

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/model"
)

// blockStatesOf turns a decoded section back into the states it was built
// from, so the same components can be written as objects and read as objects.
func blockStatesOf(t testing.TB, section mcjava.BlockSection) []mcjava.BlockState {
	t.Helper()
	states, err := section.ParsedStates()
	if err != nil {
		t.Fatal(err)
	}
	return states
}

// Writing region by region has to produce the same world as writing all of it
// at once, or it is a different export wearing the same name. It also has to
// keep the two things the original guarantees about an interrupted run: every
// target checked, and every existing file proved readable, before anything is
// written.

// sourceForTest hands out the same component bytes for every reference, which
// is all Prepare needs in order to load something.
type sourceForTest struct{ dir string }

func newSourceForTest(t testing.TB) (sourceForTest, []ChunkSource, []PreparedChunk) {
	t.Helper()
	dir := t.TempDir()
	components := testComponents(t)

	// The same components testComponents builds, encoded back to the bytes an
	// archive would hold, so Prepare has something real to decode.
	shape, err := mcjava.EncodeShape(components.Shape.MinSectionY, components.Shape.SectionCount)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := mcjava.EncodeBlockSection(-4, blockStatesOf(t, components.Blocks[-4]))
	if err != nil {
		t.Fatal(err)
	}
	biomes, err := mcjava.EncodeBiomeSection(-4, components.Biomes[-4].Biomes())
	if err != nil {
		t.Fatal(err)
	}

	refs := map[string]model.BlobRef{}
	for name, payload := range map[string][]byte{
		"mcjava.shape":     shape,
		"mcjava.blocks.-4": blocks,
		"mcjava.biomes.-4": biomes,
	} {
		sum := sha256.Sum256(payload)
		ref := model.BlobRef{Algorithm: "sha256", Digest: hex.EncodeToString(sum[:]), Size: int64(len(payload))}
		if err := os.WriteFile(filepath.Join(dir, ref.Digest), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		refs[name] = ref
	}

	// Chunks spread over more than one region file, which is the whole point.
	positions := [][2]int32{{1, 1}, {2, 2}, {40, 3}, {41, 4}, {-1, -1}}
	var sources []ChunkSource
	var prepared []PreparedChunk
	for _, position := range positions {
		chunk := model.ChunkRef{ServerID: "example", Dimension: "minecraft:overworld", X: position[0], Z: position[1]}
		sources = append(sources, ChunkSource{
			Chunk:       chunk,
			Observation: model.Observation{Chunk: chunk, Components: refs},
		})
		prepared = append(prepared, PreparedChunk{Chunk: chunk, Components: components})
	}
	return sourceForTest{dir: dir}, sources, prepared
}

func (s sourceForTest) Open(ref model.BlobRef) (*os.File, error) {
	return os.Open(filepath.Join(s.dir, ref.Digest))
}

func regionFilesUnder(t *testing.T, worldDir, dimension string) map[string][]byte {
	t.Helper()
	dir, err := DimensionDirectory(worldDir, dimension)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = data
	}
	return out
}

func TestRegionByRegionWritesTheSameWorld(t *testing.T) {
	source, sources, prepared := newSourceForTest(t)
	const dimension = "minecraft:overworld"

	atOnce := world(t)
	wholeReport, err := Export(prepared, ExportRequest{
		WorldDir: atOnce, Dimension: dimension, DataVersion: DataVersion26_2,
	})
	if err != nil {
		t.Fatal(err)
	}

	streamed := world(t)
	streamReport, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: streamed, Dimension: dimension, DataVersion: DataVersion26_2,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if wholeReport.Chunks != streamReport.Chunks || wholeReport.Kept != streamReport.Kept {
		t.Errorf("reports differ: whole %+v, streamed %+v", wholeReport, streamReport)
	}
	if len(wholeReport.RegionFiles) != len(streamReport.RegionFiles) {
		t.Errorf("region file counts differ: %d and %d",
			len(wholeReport.RegionFiles), len(streamReport.RegionFiles))
	}

	whole := regionFilesUnder(t, atOnce, dimension)
	stream := regionFilesUnder(t, streamed, dimension)
	if len(whole) != len(stream) {
		t.Fatalf("%d region file(s) written at once, %d region by region", len(whole), len(stream))
	}
	names := make([]string, 0, len(whole))
	for name := range whole {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !bytes.Equal(whole[name], stream[name]) {
			t.Errorf("%s differs between the two paths: %d bytes and %d",
				name, len(whole[name]), len(stream[name]))
		}
	}
}

// Chunks somebody else's game wrote are kept, the same way and in the same
// numbers.
func TestRegionByRegionKeepsWhatItDidNotWrite(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	const dimension = "minecraft:overworld"

	streamed := world(t)
	dir, err := DimensionDirectory(streamed, dimension)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A region file the game wrote, holding chunks at positions this export
	// does not have.
	existing := somebodyElsesRegion(t, 0, 0, [2]int32{5, 5}, [2]int32{6, 6})
	if err := os.WriteFile(filepath.Join(dir, RegionFileName(0, 0)), existing, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: streamed, Dimension: dimension, DataVersion: DataVersion26_2, Overwrite: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Kept != 2 {
		t.Errorf("kept %d chunk(s); the two this export never observed should still be there", report.Kept)
	}
}

// The property an interrupted export rests on. A region file that cannot be
// read has to stop the whole thing before any file is replaced, not after the
// ones before it in sort order have already gone.
func TestAnUnreadableRegionStopsTheExportBeforeAnythingIsWritten(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	const dimension = "minecraft:overworld"

	streamed := world(t)
	dir, err := DimensionDirectory(streamed, dimension)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// r.0.0 is written first in sort order and is fine; r.1.0 holds the chunks
	// at x=40 and 41 and is damaged. Without a validation pass that runs first,
	// r.0.0 would already have been replaced by the time this was found.
	good := somebodyElsesRegion(t, 0, 0, [2]int32{5, 5})
	if err := os.WriteFile(filepath.Join(dir, RegionFileName(0, 0)), good, 0o644); err != nil {
		t.Fatal(err)
	}
	damaged := somebodyElsesRegion(t, 1, 0, [2]int32{40, 7})
	// A slot that points into the header, which Adopt refuses.
	damaged[0], damaged[1], damaged[2], damaged[3] = 0, 0, 1, 1
	damagedPath := filepath.Join(dir, RegionFileName(1, 0))
	if err := os.WriteFile(damagedPath, damaged, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, RegionFileName(0, 0)))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ExportByRegion(source, sources, ExportRequest{
		WorldDir: streamed, Dimension: dimension, DataVersion: DataVersion26_2, Overwrite: true,
	}, nil); err == nil {
		t.Fatal("an unreadable region file did not stop the export")
	}

	after, err := os.ReadFile(filepath.Join(dir, RegionFileName(0, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a region file was replaced before the export found the one it could not read")
	}
}

// And a target that already exists still refuses without --overwrite, before
// any file is touched.
func TestRegionByRegionStillRefusesAnExistingFile(t *testing.T) {
	source, sources, _ := newSourceForTest(t)
	const dimension = "minecraft:overworld"

	streamed := world(t)
	dir, err := DimensionDirectory(streamed, dimension)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, RegionFileName(0, 0)), []byte("whatever"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ExportByRegion(source, sources, ExportRequest{
		WorldDir: streamed, Dimension: dimension, DataVersion: DataVersion26_2,
	}, nil)
	if err == nil {
		t.Fatal("an existing region file was written into without being asked")
	}
	if got, err := os.ReadFile(filepath.Join(dir, RegionFileName(0, 0))); err != nil || string(got) != "whatever" {
		t.Errorf("the existing file was touched: %q %v", string(got), err)
	}
}
