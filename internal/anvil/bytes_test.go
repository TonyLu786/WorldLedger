package anvil

import (
	"testing"
)

// A region file's every length is known before any of it is written: two
// header sectors and, for each chunk, its frame rounded up to a whole sector.
// It used to guess one sector per chunk and append, so the slice regrew several
// times, and each chunk got a padded copy of its own on the way in.
func TestARegionAllocatesOnlyWhatItProduces(t *testing.T) {
	region := NewRegion(0, 0)
	for x := int32(0); x < 32; x++ {
		for z := int32(0); z < 8; z++ {
			chunk, err := BuildChunk(x, z, DataVersion26_2, testComponents(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := region.AddChunk(x, z, chunk); err != nil {
				t.Fatal(err)
			}
		}
	}

	var allocated int64
	var produced int
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out := region.Bytes()
			produced = len(out)
		}
	})
	allocated = result.AllocedBytesPerOp()

	ratio := float64(allocated) / float64(produced)
	t.Logf("a %d byte region costs %d bytes of allocation (%.2fx)", produced, allocated, ratio)
	if ratio > 1.5 {
		t.Errorf("Bytes allocates %.1fx what it produces; it knows every length in advance", ratio)
	}
}
