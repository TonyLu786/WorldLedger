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
// throws the result away, and then a writing pass that reads them again. The
// alternative is holding every adopted frame across the whole export, which is
// the memory this exists to avoid, or discovering the unreadable file after
// four of its neighbours have already been replaced.
//
// Reading the destination twice was worth measuring rather than worrying about,
// and it is cheaper than it sounds: eight hundred chunks over region files that
// all already existed took 9.6 seconds written all at once and 10.0 region by
// region, a difference of under five per cent, and most of that is scanning a
// thousand header slots a second time rather than the bytes. The destination is
// small next to the archive it is built from. A full region file is sixteen
// mebibytes against the hundreds of mebibytes of objects decoded to fill it, so
// the second pass is a rounding error on an export and the property it buys is
// not.

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

	keys, grouped := groupByRegion(chunks)

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
		// Which slots this export will replace, and so does not have to be able
		// to adopt. With a transform there are none it can promise: a transform
		// may drop any chunk it is given, and a slot this meant to replace and
		// then did not is a slot the writing pass tries to adopt after nothing
		// validated it. That failed there instead, reporting that the file had
		// changed while the export was running, about a file that had not
		// changed at all.
		ours := grouped[key]
		if transform != nil {
			ours = nil
		}
		if _, err := checkAdoptable(key[0], key[1], existing, ours); err != nil {
			return ExportReport{}, fmt.Errorf(
				"%s could not be read, so writing it would have discarded what it holds: %w", paths[index], err)
		}
	}

	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return ExportReport{}, err
	}

	var report ExportReport
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
		if len(prepared) == 0 {
			// A transform is allowed to drop every chunk in a region; a
			// conversion that skips what the target release cannot hold does
			// exactly that. Writing the file anyway would create an empty
			// region where there had been none, or rewrite an existing one
			// byte for byte to say nothing, and either way name it afterwards
			// as a file this wrote.
			continue
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
		report.RegionFiles = append(report.RegionFiles, paths[index])
	}
	return report, nil
}

// EachRegion loads a dimension one region at a time and hands each region's
// chunks to visit, writing nothing.
//
// It exists for the half of a conversion that has to be decided before any of
// it is written. Under the report policy a single piece of state the target
// release cannot represent means the whole conversion writes nothing, and that
// promise cannot be kept by a writer that has already put thirty region files
// on disk by the time it reaches the one that refuses. So the translation is
// run over the whole dimension first with its output thrown away, and then run
// again by ExportByRegion for the world it produces. One region is held at a
// time either way. What the first pass adds is measured against the second in
// bench_each_region_test.go: about a fifth of it with nothing to translate, and
// more as the translation itself grows.
//
// The policies that cannot refuse do not pay this. skip-chunk and fill decide
// each chunk on its own and never change their mind about the ones already
// written, so they stream straight through in a single pass.
func EachRegion(source ObjectSource, chunks []ChunkSource, visit func(prepared []PreparedChunk) error) error {
	keys, grouped := groupByRegion(chunks)
	for _, key := range keys {
		prepared, err := Prepare(source, grouped[key])
		if err != nil {
			return err
		}
		if err := visit(prepared); err != nil {
			return err
		}
	}
	return nil
}

// groupByRegion divides chunks along the line the output files divide on, and
// orders the regions so that two runs over the same archive do the same work in
// the same order.
func groupByRegion(chunks []ChunkSource) ([][2]int32, map[[2]int32][]ChunkSource) {
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
	return keys, grouped
}

// checkAdoptable reads an existing region file the way Adopt would, and reports
// how many chunks would be kept without keeping any of them.
//
// The slots this export will occupy are marked first, because Adopt skips them
// and a validation that did not would refuse a file over a chunk about to be
// replaced. What is validated is exactly what would be adopted.
//
// ours may be empty, which validates the whole file. That is what a write with
// a transform passes, because a transform is allowed to drop chunks and so no
// slot can be promised in advance. The cost is refusing a conversion over a
// damaged chunk it might have replaced; the alternative is discovering the
// damage with region files already written.
func checkAdoptable(regionX, regionZ int32, existing []byte, ours []ChunkSource) (int, error) {
	probe := NewRegion(regionX, regionZ)
	for _, entry := range ours {
		probe.payload[regionSlot(entry.Chunk.X, entry.Chunk.Z)] = nil
	}
	return probe.Adopt(existing)
}
