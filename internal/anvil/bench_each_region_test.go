package anvil

import (
	"os"
	"path/filepath"
	"testing"
)

// What the deciding pass costs.
//
// convert under the report policy runs the translation twice: once over every
// region with the result thrown away, to find out whether anything may be
// written at all, and then again as the world is written. Saying that costs
// twice the work is wrong, because the second pass also builds and writes the
// region files and the first does not.
//
// Measured with nothing to translate, so that what is priced is the reading and
// decoding the deciding pass repeats: 3.5ms against 17ms, and 247 KiB allocated
// against 4.5 MiB. The deciding pass is about a fifth of the writing pass.
//
// A real conversion translates in both passes, so the share rises with how
// expensive the translation is, towards a half in the limit where it dominates
// everything else. A fifth is the floor, not the answer.

func benchWorld(b *testing.B) (sourceForTest, []ChunkSource, string) {
	b.Helper()
	source, sources, _ := newSourceForTest(b)
	world := b.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		b.Fatal(err)
	}
	return source, sources, world
}

// Into a world that has nothing in it, which is what convert writes into. An
// overwrite would also read every region file back, and that cost belongs to
// ExportByRegion rather than to the deciding pass this is here to price.
func BenchmarkWritingPass(b *testing.B) {
	source, sources, _ := benchWorld(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		world := b.TempDir()
		if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := ExportByRegion(source, sources, ExportRequest{
			WorldDir: world, Dimension: "minecraft:overworld", DataVersion: 4903,
		}, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecidingPass(b *testing.B) {
	source, sources, _ := benchWorld(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := EachRegion(source, sources, func([]PreparedChunk) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}
