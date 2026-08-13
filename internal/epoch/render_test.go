package epoch

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func selectionAt(x, z int32, status Status) Selection {
	return Selection{
		Chunk:  model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: x, Z: z},
		Status: status,
	}
}

func decodePNG(t *testing.T, path string) image.Image {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestRenderPNGSizesToTheCoveredArea(t *testing.T) {
	snapshot := Snapshot{Selections: []Selection{
		selectionAt(0, 0, StatusCorroborated),
		selectionAt(3, 1, StatusSingleSource),
	}}

	path := filepath.Join(t.TempDir(), "map.png")
	if err := snapshot.RenderPNG(path, 2); err != nil {
		t.Fatal(err)
	}

	image := decodePNG(t, path)
	// x spans 0..3 and z spans 0..1, at two pixels per chunk.
	if got := image.Bounds().Dx(); got != 8 {
		t.Fatalf("width = %d; want 8", got)
	}
	if got := image.Bounds().Dy(); got != 4 {
		t.Fatalf("height = %d; want 4", got)
	}
}

// Each status must be visually distinct, otherwise the map cannot convey what
// the summary already says in words.
func TestEachStatusDrawsADistinctColour(t *testing.T) {
	snapshot := Snapshot{Selections: []Selection{
		selectionAt(0, 0, StatusCorroborated),
		selectionAt(1, 0, StatusSingleSource),
		selectionAt(2, 0, StatusSuperseded),
		selectionAt(3, 0, StatusConflict),
	}}

	path := filepath.Join(t.TempDir(), "map.png")
	if err := snapshot.RenderPNG(path, 1); err != nil {
		t.Fatal(err)
	}

	drawn := decodePNG(t, path)
	seen := map[uint32]int32{}
	for x := int32(0); x < 4; x++ {
		r, g, b, _ := drawn.At(int(x), 0).RGBA()
		key := r<<16 | g<<8 | b
		if previous, exists := seen[key]; exists {
			t.Fatalf("chunks %d and %d drew the same colour", previous, x)
		}
		seen[key] = x
	}
}

// A chunk with nothing observed at the epoch must not be coloured. Painting it
// would assert knowledge the archive does not have, which is the same mistake as
// exporting it as air.
func TestUnknownChunksAreLeftAsBackground(t *testing.T) {
	// The unknown chunk sits between two observed ones so that it is genuinely
	// inside the image; outside the bounds every pixel reads as transparent and
	// the assertion would pass for the wrong reason.
	snapshot := Snapshot{Selections: []Selection{
		selectionAt(0, 0, StatusCorroborated),
		selectionAt(1, 0, StatusUnknown),
		selectionAt(2, 0, StatusSingleSource),
	}}

	path := filepath.Join(t.TempDir(), "map.png")
	if err := snapshot.RenderPNG(path, 1); err != nil {
		t.Fatal(err)
	}

	drawn := decodePNG(t, path)
	if got := drawn.Bounds().Dx(); got != 3 {
		t.Fatalf("width = %d; the gap should lie inside the image", got)
	}
	knownR, knownG, knownB, _ := drawn.At(0, 0).RGBA()
	gapR, gapG, gapB, _ := drawn.At(1, 0).RGBA()
	if knownR == gapR && knownG == gapG && knownB == gapB {
		t.Fatal("an unobserved chunk was drawn the same as an observed one")
	}

	wantR, wantG, wantB, _ := unobserved.RGBA()
	if gapR != wantR || gapG != wantG || gapB != wantB {
		t.Fatal("an unobserved chunk was not left as background")
	}
}

// An unknown chunk must not stretch the image either: the map covers what was
// observed.
func TestUnknownChunksDoNotEnlargeTheImage(t *testing.T) {
	snapshot := Snapshot{Selections: []Selection{
		selectionAt(0, 0, StatusSingleSource),
		selectionAt(1000, 1000, StatusUnknown),
	}}

	path := filepath.Join(t.TempDir(), "map.png")
	if err := snapshot.RenderPNG(path, 1); err != nil {
		t.Fatal(err)
	}
	if got := decodePNG(t, path).Bounds().Dx(); got != 1 {
		t.Fatalf("width = %d; want 1, the unknown chunk should not extend the map", got)
	}
}

func TestRenderRefusesWhenThereIsNothingToDraw(t *testing.T) {
	snapshot := Snapshot{Selections: []Selection{selectionAt(0, 0, StatusUnknown)}}
	if err := snapshot.RenderPNG(filepath.Join(t.TempDir(), "map.png"), 1); err == nil {
		t.Fatal("rendered a map with no observed chunk")
	}
}

func TestRenderRefusesAnUnreasonablyLargeImage(t *testing.T) {
	snapshot := Snapshot{Selections: []Selection{
		selectionAt(0, 0, StatusSingleSource),
		selectionAt(100000, 0, StatusSingleSource),
	}}
	err := snapshot.RenderPNG(filepath.Join(t.TempDir(), "map.png"), 8)
	if err == nil {
		t.Fatal("rendered an image far beyond any usable size")
	}
}

func TestBoundsReportsTheCoveredRectangle(t *testing.T) {
	snapshot := Snapshot{Selections: []Selection{
		selectionAt(-5, 3, StatusSingleSource),
		selectionAt(7, -2, StatusConflict),
	}}
	minX, minZ, maxX, maxZ, ok := snapshot.Bounds()
	if !ok || minX != -5 || maxX != 7 || minZ != -2 || maxZ != 3 {
		t.Fatalf("bounds = (%d,%d)..(%d,%d) ok=%v", minX, minZ, maxX, maxZ, ok)
	}
}
