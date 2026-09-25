package anvil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Writing a dimension one region file at a time.
//
// Export builds every chunk and every region before it writes anything, which
// means the whole dimension is decoded and compressed in memory at once, on a
// machine somebody plays Minecraft on.
//
// A region file holds 1,024 chunks and nothing outside it needs them, so the
// work divides along the same line the output does. This loads one region's
// chunks, writes that file, and drops them.
//
// What that buys is a bound rather than a factor, and the factor on its own
// would be misleading. Measured at 205 KiB per chunk held, which matches what
// the audit found: an export covering three region files went from 320 MiB to
// 155, and one covering a single region saves nothing at all. The peak is one
// region file's worth of chunks, at most 1,024 of them and so around 210 MiB,
// whether the export is a thousand chunks or a million. The old shape had no
// bound: a hundred thousand chunks is twenty gibibytes and there is nothing in
// it that stops.
//
// What it does not trade away is the two properties the original had, because
// both are about what an interrupted export leaves behind:
//
//   - every target path is checked before anything is written, so a refusal
//     over an existing file cannot leave half a dimension;
//   - every existing region file is read and proved adoptable before anything
//     is written, so a file this cannot understand stops the export rather than
//     being replaced by a smaller one.
//
// The second is why there is a validation pass that reads the region files and
// throws the result away, and then a writing pass that reads them again. It
// costs reading the destination twice. The alternative is holding every
// adopted frame across the whole export, which is the memory this exists to
// avoid, or discovering the unreadable file after four of its neighbours have
// already been replaced.

// ExportByRegion writes an existing world one region file at a time.
//
// transform, when it is not nil, is given each region's chunks after they are
// loaded and before they are built. It is where a conversion between Minecraft
// releases happens, so that a conversion is bounded the same way an export is.
func ExportByRegion(source ObjectSource, chunks []ChunkSource, request ExportRequest,
	transform func(prepared []PreparedChunk) ([]PreparedChunk, error)) (ExportReport, error) {

	regionDir, err := DimensionDirectory(request.WorldDir, request.Dimension)
	if err != nil {
		return ExportReport{}, err
	}
	if err := requireExistingWorld(request.WorldDir); err != nil {
		return ExportReport{}, err
	}

	grouped := map[[2]int32][]ChunkSource{}
	for _, entry := range chunks {
		regionX, regionZ := RegionOf(entry.Chunk.X, entry.Chunk.Z)
		key := [2]int32{regionX, regionZ}
		grouped[key] = append(grouped[key], entry)
	}
	keys := make([][2]int32, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] == keys[j][0] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})

	paths := make([]string, 0, len(keys))
	for _, key := range keys {
		path := filepath.Join(regionDir, RegionFileName(key[0], key[1]))
		if !request.Overwrite {
			if _, err := os.Stat(path); err == nil {
				return ExportReport{}, fmt.Errorf(
					"%s already exists; pass --overwrite to write into it, which replaces only the chunks this export has", path)
			} else if !os.IsNotExist(err) {
				return ExportReport{}, err
			}
		}
		paths = append(paths, path)
	}

	// Proved before anything is written, and the result thrown away. Holding it
	// would be holding the whole dimension again.
	for index, key := range keys {
		existing, err := os.ReadFile(paths[index])
		if err != nil && !os.IsNotExist(err) {
			return ExportReport{}, fmt.Errorf("%s: %w", paths[index], err)
		}
		if len(existing) == 0 {
			continue
		}
		if _, err := checkAdoptable(key[0], key[1], existing, grouped[key]); err != nil {
			return ExportReport{}, fmt.Errorf(
				"%s could not be read, so writing it would have discarded what it holds: %w", paths[index], err)
		}
	}

	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return ExportReport{}, err
	}

	report := ExportReport{RegionFiles: paths}
	for index, key := range keys {
		prepared, err := Prepare(source, grouped[key])
		if err != nil {
			return ExportReport{}, err
		}
		if transform != nil {
			prepared, err = transform(prepared)
			if err != nil {
				return ExportReport{}, err
			}
		}

		region := NewRegion(key[0], key[1])
		for _, entry := range prepared {
			chunk, err := BuildChunk(entry.Chunk.X, entry.Chunk.Z, request.DataVersion, entry.Components)
			if err != nil {
				return ExportReport{}, err
			}
			if err := region.AddChunk(entry.Chunk.X, entry.Chunk.Z, chunk); err != nil {
				return ExportReport{}, err
			}
		}
		report.Chunks += len(prepared)

		existing, err := os.ReadFile(paths[index])
		if err != nil && !os.IsNotExist(err) {
			return ExportReport{}, fmt.Errorf("%s: %w", paths[index], err)
		}
		if len(existing) > 0 {
			kept, err := region.Adopt(existing)
			if err != nil {
				// The validation pass above read the same file, so reaching
				// this means it changed underneath the export.
				return ExportReport{}, fmt.Errorf(
					"%s changed while the export was running: %w", paths[index], err)
			}
			report.Kept += kept
		}
		if err := writeFileAtomic(paths[index], region.Bytes()); err != nil {
			return ExportReport{}, err
		}
	}
	return report, nil
}

// checkAdoptable reads an existing region file the way Adopt would, and reports
// how many chunks would be kept without keeping any of them.
//
// The slots this export will occupy are marked first, because Adopt skips them
// and a validation that did not would refuse a file over a chunk about to be
// replaced. What is validated is exactly what would be adopted.
func checkAdoptable(regionX, regionZ int32, existing []byte, ours []ChunkSource) (int, error) {
	probe := NewRegion(regionX, regionZ)
	for _, entry := range ours {
		probe.payload[regionSlot(entry.Chunk.X, entry.Chunk.Z)] = nil
	}
	return probe.Adopt(existing)
}
