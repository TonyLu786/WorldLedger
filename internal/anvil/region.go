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
}

func NewRegion(regionX, regionZ int32) *Region {
	return &Region{X: regionX, Z: regionZ, payload: map[int][]byte{}}
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

	locations := make([]byte, sectorBytes)
	timestamps := make([]byte, sectorBytes)
	body := make([]byte, 0, len(r.payload)*sectorBytes)

	nextSector := headerSectors
	for _, slot := range slots {
		frame := r.payload[slot]
		sectors := (len(frame) + sectorBytes - 1) / sectorBytes
		entry := slot * 4
		locations[entry] = byte(nextSector >> 16)
		locations[entry+1] = byte(nextSector >> 8)
		locations[entry+2] = byte(nextSector)
		locations[entry+3] = byte(sectors)

		padded := make([]byte, sectors*sectorBytes)
		copy(padded, frame)
		body = append(body, padded...)
		nextSector += sectors
	}

	out := make([]byte, 0, len(locations)+len(timestamps)+len(body))
	out = append(out, locations...)
	out = append(out, timestamps...)
	return append(out, body...)
}

func regionSlot(chunkX, chunkZ int32) int {
	return int(chunkX&31) + int(chunkZ&31)*32
}

func appendU32(out []byte, value uint32) []byte {
	return append(out, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}
