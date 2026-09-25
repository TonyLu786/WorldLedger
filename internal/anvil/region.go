package anvil

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"sort"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
)

const (
	sectorBytes = 4096
	// The header holds 1024 location entries and 1024 timestamps, each four
	// bytes, so chunk payloads begin at sector 2.
	headerSectors = 2
	regionChunks  = 32 * 32
	// A location entry stores its sector count in a single byte.
	maxChunkSectors = 255

	// RegionFileVersion registers 1 as gzip and 2 as deflate. compress/zlib
	// writes zlib-wrapped deflate, which is the format behind VERSION_DEFLATE.
	compressionDeflate = 2
)

// RegionOf returns the region that owns a chunk. Region coordinates are an
// arithmetic shift, so negative chunk coordinates land in negative regions.
func RegionOf(chunkX, chunkZ int32) (int32, int32) {
	return chunkX >> 5, chunkZ >> 5
}

func RegionFileName(regionX, regionZ int32) string {
	return fmt.Sprintf("r.%d.%d.mca", regionX, regionZ)
}

type Region struct {
	X, Z    int32
	payload map[int][]byte
	// stamped carries the per-chunk timestamps of chunks adopted from a file
	// that already existed. Chunks this export wrote have none, which is what
	// the writer has always produced and what makes an export reproducible.
	stamped map[int][]byte
}

func NewRegion(regionX, regionZ int32) *Region {
	return &Region{X: regionX, Z: regionZ, payload: map[int][]byte{}, stamped: map[int][]byte{}}
}

// Adopt keeps the chunks an existing region file holds that this export does
// not supply.
//
// Without it, writing one chunk into a region destroys the other 1,023 that
// file could hold, because a region is laid out from what it was given and
// written over whatever was there. That is how an export into a world somebody
// had played in deleted terrain nobody asked it to touch: the region file is
// the unit on disk, and the chunk is the unit anybody thinks in.
//
// Adopted chunks are copied as raw frames. They are not decompressed, parsed,
// re-encoded or validated, because this archive did not observe them and has no
// business having an opinion about their contents -- it only has to not lose
// them.
func (r *Region) Adopt(existing []byte) (int, error) {
	if len(existing) == 0 {
		return 0, nil
	}
	if len(existing) < headerSectors*sectorBytes {
		return 0, fmt.Errorf("region (%d,%d): the file is %d bytes, too short to hold a header", r.X, r.Z, len(existing))
	}

	adopted := 0
	for slot := 0; slot < regionChunks; slot++ {
		entry := slot * 4
		offset := int(existing[entry])<<16 | int(existing[entry+1])<<8 | int(existing[entry+2])
		sectors := int(existing[entry+3])
		if offset == 0 && sectors == 0 {
			continue
		}
		// A chunk this export wrote wins, which is the whole point of writing
		// it. Everything else is somebody else's and is kept as it is.
		if _, ours := r.payload[slot]; ours {
			continue
		}
		if offset < headerSectors || sectors == 0 {
			return adopted, fmt.Errorf("region (%d,%d): chunk slot %d points at sector %d, which is inside the header",
				r.X, r.Z, slot, offset)
		}
		start := offset * sectorBytes
		end := start + sectors*sectorBytes
		if end > len(existing) {
			return adopted, fmt.Errorf("region (%d,%d): chunk slot %d runs to byte %d of a %d byte file",
				r.X, r.Z, slot, end, len(existing))
		}
		length := int(existing[start])<<24 | int(existing[start+1])<<16 | int(existing[start+2])<<8 | int(existing[start+3])
		if length <= 0 || 4+length > sectors*sectorBytes {
			return adopted, fmt.Errorf("region (%d,%d): chunk slot %d declares a %d byte frame in %d sector(s)",
				r.X, r.Z, slot, length, sectors)
		}
		frame := make([]byte, 4+length)
		copy(frame, existing[start:start+4+length])
		r.payload[slot] = frame

		stamp := make([]byte, 4)
		copy(stamp, existing[sectorBytes+entry:sectorBytes+entry+4])
		r.stamped[slot] = stamp
		adopted++
	}
	return adopted, nil
}

func (r *Region) Len() int {
	return len(r.payload)
}

// AddChunk compresses one chunk into the region. It rejects a chunk that does
// not belong to this region rather than silently writing it to the wrong slot.
func (r *Region) AddChunk(chunkX, chunkZ int32, chunk mcjava.NBTValue) error {
	regionX, regionZ := RegionOf(chunkX, chunkZ)
	if regionX != r.X || regionZ != r.Z {
		return fmt.Errorf("chunk (%d,%d) belongs to region (%d,%d), not (%d,%d)", chunkX, chunkZ, regionX, regionZ, r.X, r.Z)
	}
	index := regionSlot(chunkX, chunkZ)
	if _, exists := r.payload[index]; exists {
		return fmt.Errorf("chunk (%d,%d) is already present in region (%d,%d)", chunkX, chunkZ, r.X, r.Z)
	}

	encoded, err := EncodeNamed("", chunk)
	if err != nil {
		return fmt.Errorf("chunk (%d,%d): %w", chunkX, chunkZ, err)
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(encoded); err != nil {
		return fmt.Errorf("chunk (%d,%d): %w", chunkX, chunkZ, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("chunk (%d,%d): %w", chunkX, chunkZ, err)
	}

	// The stored frame is a big-endian length followed by the compression byte
	// and the compressed payload; the length covers the compression byte.
	frame := make([]byte, 0, 5+compressed.Len())
	frame = appendU32(frame, uint32(compressed.Len()+1))
	frame = append(frame, compressionDeflate)
	frame = append(frame, compressed.Bytes()...)

	sectors := (len(frame) + sectorBytes - 1) / sectorBytes
	if sectors > maxChunkSectors {
		return fmt.Errorf("chunk (%d,%d) needs %d sectors; a region entry allows %d", chunkX, chunkZ, sectors, maxChunkSectors)
	}
	r.payload[index] = frame
	return nil
}

// Bytes lays out the region file. Chunks are placed in slot order so the same
// input always produces the same file.
func (r *Region) Bytes() []byte {
	slots := make([]int, 0, len(r.payload))
	for slot := range r.payload {
		slots = append(slots, slot)
	}
	sort.Ints(slots)

	// The whole file is sized before any of it is written.
	//
	// It used to guess: one sector per chunk for the body, when a chunk is
	// three or four, so appending regrew the slice several times over. Each
	// chunk also got a padded copy of its own before being appended into that.
	// A sixteen mebibyte region cost a hundred and eleven mebibytes of
	// allocation, seven times what it produced, and an export is many regions.
	//
	// Every length here is known in advance, so none of it needs guessing.
	total := headerSectors * sectorBytes
	for _, slot := range slots {
		total += sectorsFor(len(r.payload[slot])) * sectorBytes
	}
	out := make([]byte, total)
	locations := out[:sectorBytes]
	timestamps := out[sectorBytes : 2*sectorBytes]

	nextSector := headerSectors
	for _, slot := range slots {
		frame := r.payload[slot]
		sectors := sectorsFor(len(frame))
		entry := slot * 4
		locations[entry] = byte(nextSector >> 16)
		locations[entry+1] = byte(nextSector >> 8)
		locations[entry+2] = byte(nextSector)
		locations[entry+3] = byte(sectors)
		// An adopted chunk keeps the time the game last wrote it. Chunks this
		// export produced keep none, which is what has always been written and
		// is what lets two exports of the same archive compare byte for byte.
		if stamp, kept := r.stamped[slot]; kept {
			copy(timestamps[entry:entry+4], stamp)
		}

		// Straight into the sector it belongs in. The rest of that sector is
		// already zero, which is the padding.
		copy(out[nextSector*sectorBytes:], frame)
		nextSector += sectors
	}
	return out
}

// sectorsFor is how many whole sectors a frame occupies.
func sectorsFor(length int) int {
	return (length + sectorBytes - 1) / sectorBytes
}

func regionSlot(chunkX, chunkZ int32) int {
	return int(chunkX&31) + int(chunkZ&31)*32
}

func appendU32(out []byte, value uint32) []byte {
	return append(out, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}
