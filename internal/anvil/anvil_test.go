package anvil

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
)

func blockSection(t *testing.T, sectionY int32, names ...string) mcjava.BlockSection {
	t.Helper()
	states := make([]mcjava.BlockState, mcjava.BlockCount)
	for position := range states {
		states[position] = mcjava.BlockState{Name: names[position%len(names)]}
	}
	encoded, err := mcjava.EncodeBlockSection(sectionY, states)
	if err != nil {
		t.Fatal(err)
	}
	section, err := mcjava.DecodeBlockSection(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return section
}

func biomeSection(t *testing.T, sectionY int32, names ...string) mcjava.BiomeSection {
	t.Helper()
	biomes := make([]string, mcjava.BiomeCount)
	for position := range biomes {
		biomes[position] = names[position%len(names)]
	}
	encoded, err := mcjava.EncodeBiomeSection(sectionY, biomes)
	if err != nil {
		t.Fatal(err)
	}
	section, err := mcjava.DecodeBiomeSection(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return section
}

func TestPackIndicesMatchesTheAnvilLongLayout(t *testing.T) {
	// Two block palette entries pack at the four-bit floor, sixteen per long,
	// so alternating indices set bit 4 of every nibble pair.
	blocks := make([]uint16, mcjava.BlockCount)
	for position := range blocks {
		blocks[position] = uint16(position % 2)
	}
	packedBlocks := packIndices(blocks, 2, 4)
	if len(packedBlocks) != mcjava.BlockCount/16 {
		t.Fatalf("block long count = %d; want %d", len(packedBlocks), mcjava.BlockCount/16)
	}
	if uint64(packedBlocks[0]) != 0x1010101010101010 {
		t.Fatalf("packed blocks[0] = %#016x; want 0x1010101010101010", uint64(packedBlocks[0]))
	}

	// Two biome palette entries pack at one bit, sixty-four per long.
	biomes := make([]uint16, mcjava.BiomeCount)
	for position := range biomes {
		biomes[position] = uint16(position % 2)
	}
	packedBiomes := packIndices(biomes, 2, 1)
	if len(packedBiomes) != 1 {
		t.Fatalf("biome long count = %d; want 1", len(packedBiomes))
	}
	if uint64(packedBiomes[0]) != 0xAAAAAAAAAAAAAAAA {
		t.Fatalf("packed biomes[0] = %#016x; want 0xAAAAAAAAAAAAAAAA", uint64(packedBiomes[0]))
	}
}

func TestPackIndicesOmitsDataForASingleEntryPalette(t *testing.T) {
	if packed := packIndices(make([]uint16, mcjava.BlockCount), 1, 4); packed != nil {
		t.Fatalf("single-entry palette produced %d longs; want no data array", len(packed))
	}
}

func TestPackIndicesNeverSplitsAnEntryAcrossLongs(t *testing.T) {
	// Five bits per entry leaves four unused high bits in every long; an entry
	// must not straddle that boundary.
	indices := make([]uint16, mcjava.BlockCount)
	for position := range indices {
		indices[position] = uint16(position % 17)
	}
	packed := packIndices(indices, 17, 4)
	const bits, perLong = 5, 12
	if len(packed) != (mcjava.BlockCount+perLong-1)/perLong {
		t.Fatalf("long count = %d", len(packed))
	}
	for position, want := range indices {
		slot := position / perLong
		offset := uint((position % perLong) * bits)
		got := uint16((uint64(packed[slot]) >> offset) & 0x1F)
		if got != want {
			t.Fatalf("entry %d unpacked to %d; want %d", position, got, want)
		}
	}
}

func TestModifiedUTF8EncodesSupplementaryCharactersAsSurrogatePairs(t *testing.T) {
	// U+1F600 is four bytes in standard UTF-8 but six in Java modified UTF-8.
	encoded := modifiedUTF8("\U0001F600")
	want := []byte{0xED, 0xA0, 0xBD, 0xED, 0xB8, 0x80}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("emoji encoded to % X; want % X", encoded, want)
	}

	if got := modifiedUTF8("\x00"); !bytes.Equal(got, []byte{0xC0, 0x80}) {
		t.Fatalf("NUL encoded to % X; want C0 80", got)
	}
	if got := modifiedUTF8("ab"); !bytes.Equal(got, []byte("ab")) {
		t.Fatalf("ASCII encoded to % X", got)
	}
}

func TestEncodeNamedWritesVanillaFraming(t *testing.T) {
	value := tagCompound(named("a", tagByte(7)))
	encoded, err := EncodeNamed("", value)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		byte(mcjava.TagCompound), 0x00, 0x00, // root type and empty name
		byte(mcjava.TagByte), 0x00, 0x01, 'a', 7, // named byte
		byte(mcjava.TagEnd), // compound terminator
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded % X; want % X", encoded, want)
	}
}

func testComponents(t *testing.T) ChunkComponents {
	t.Helper()
	return ChunkComponents{
		Shape:  mcjava.Shape{MinSectionY: -4, SectionCount: 24},
		Blocks: map[int32]mcjava.BlockSection{-4: blockSection(t, -4, "minecraft:air", "minecraft:stone")},
		Biomes: map[int32]mcjava.BiomeSection{-4: biomeSection(t, -4, "minecraft:plains", "minecraft:desert")},
	}
}

func TestBuildChunkPlacesObservedSectionsOnly(t *testing.T) {
	components := testComponents(t)
	chunk, err := BuildChunk(3, -5, DataVersion26_2, components)
	if err != nil {
		t.Fatal(err)
	}

	fields := map[string]mcjava.NBTValue{}
	for _, entry := range chunk.Compound {
		fields[entry.Name] = entry.Value
	}
	if fields["DataVersion"].Int != DataVersion26_2 {
		t.Fatalf("DataVersion = %d; want %d", fields["DataVersion"].Int, DataVersion26_2)
	}
	if fields["xPos"].Int != 3 || fields["zPos"].Int != -5 {
		t.Fatalf("chunk position = (%d,%d)", fields["xPos"].Int, fields["zPos"].Int)
	}
	if fields["yPos"].Int != -4 {
		t.Fatalf("yPos = %d; want the shape minimum -4", fields["yPos"].Int)
	}
	if fields["Status"].String != chunkStatusFull {
		t.Fatalf("Status = %q", fields["Status"].String)
	}
	if fields["isLightOn"].Byte != 0 {
		t.Fatal("isLightOn must be false so the game relights rather than trusting fabricated light")
	}
	if got := len(fields["sections"].List.Values); got != 1 {
		t.Fatalf("wrote %d sections; want only the one observed section", got)
	}
}

func TestBuildChunkSubstitutesTheVoidForUnobservedBiomes(t *testing.T) {
	components := testComponents(t)
	components.Biomes = nil

	chunk, err := BuildChunk(0, 0, DataVersion26_2, components)
	if err != nil {
		t.Fatal(err)
	}
	section := findField(t, chunk, "sections").List.Values[0]
	biomes := findField(t, section, "biomes")
	palette := findField(t, biomes, "palette")
	if len(palette.List.Values) != 1 || palette.List.Values[0].String != UnknownBiome {
		t.Fatalf("unobserved biomes became %#v; want a single %s entry", palette.List.Values, UnknownBiome)
	}
	for _, entry := range biomes.Compound {
		if entry.Name == "data" {
			t.Fatal("a single-entry biome palette must not carry a data array")
		}
	}
}

func TestBuildChunkRejectsASectionOutsideTheObservedShape(t *testing.T) {
	components := testComponents(t)
	components.Blocks[99] = blockSection(t, 99, "minecraft:stone")

	if _, err := BuildChunk(0, 0, DataVersion26_2, components); err == nil {
		t.Fatal("a section outside the declared shape was accepted")
	}
}

func TestBuildChunkRequiresAnObservedShape(t *testing.T) {
	if _, err := BuildChunk(0, 0, DataVersion26_2, ChunkComponents{}); err == nil {
		t.Fatal("a chunk with no observed shape was accepted")
	}
}

func TestBuildChunkOmitsBlockEntitiesWithoutABaseline(t *testing.T) {
	components := testComponents(t)
	components.HasBlockEntities = false

	chunk, err := BuildChunk(0, 0, DataVersion26_2, components)
	if err != nil {
		t.Fatal(err)
	}
	if values := findField(t, chunk, "block_entities").List.Values; len(values) != 0 {
		t.Fatalf("wrote %d block entities without a baseline", len(values))
	}
}

func TestBuildChunkRewritesBlockEntityCoordinatesAndType(t *testing.T) {
	components := testComponents(t)
	components.HasBlockEntities = true
	components.BlockEntities = []mcjava.BlockEntity{{
		LocalX: 5,
		BlockY: 64,
		LocalZ: 9,
		Type:   "minecraft:sign",
		NBT: mcjava.NBTValue{Type: mcjava.TagCompound, Compound: []mcjava.NamedNBT{
			{Name: "id", Value: tagString("minecraft:stale")},
			{Name: "x", Value: tagInt(-999)},
			{Name: "front_text", Value: tagString("WL26.2-A")},
		}},
	}}

	chunk, err := BuildChunk(2, -3, DataVersion26_2, components)
	if err != nil {
		t.Fatal(err)
	}
	entity := findField(t, chunk, "block_entities").List.Values[0]
	if got := findField(t, entity, "id").String; got != "minecraft:sign" {
		t.Fatalf("id = %q; want the component type", got)
	}
	if got := findField(t, entity, "x").Int; got != 2*16+5 {
		t.Fatalf("x = %d; want the absolute coordinate %d", got, 2*16+5)
	}
	if got := findField(t, entity, "z").Int; got != -3*16+9 {
		t.Fatalf("z = %d; want the absolute coordinate %d", got, -3*16+9)
	}
	if got := findField(t, entity, "front_text").String; got != "WL26.2-A" {
		t.Fatalf("payload field was lost: %q", got)
	}
	seen := 0
	for _, field := range entity.Compound {
		if field.Name == "x" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("coordinate key appears %d times; the stale one was not replaced", seen)
	}
}

func findField(t *testing.T, value mcjava.NBTValue, name string) mcjava.NBTValue {
	t.Helper()
	for _, entry := range value.Compound {
		if entry.Name == name {
			return entry.Value
		}
	}
	t.Fatalf("field %q is absent", name)
	return mcjava.NBTValue{}
}

func TestRegionOfUsesArithmeticShiftForNegativeChunks(t *testing.T) {
	cases := []struct {
		chunkX, chunkZ   int32
		regionX, regionZ int32
		slot             int
	}{
		{0, 0, 0, 0, 0},
		{31, 31, 0, 0, 31 + 31*32},
		{32, 0, 1, 0, 0},
		{-1, -1, -1, -1, 31 + 31*32},
		{-32, -32, -1, -1, 0},
		{-33, 0, -2, 0, 31},
	}
	for _, test := range cases {
		regionX, regionZ := RegionOf(test.chunkX, test.chunkZ)
		if regionX != test.regionX || regionZ != test.regionZ {
			t.Fatalf("chunk (%d,%d) mapped to region (%d,%d); want (%d,%d)", test.chunkX, test.chunkZ, regionX, regionZ, test.regionX, test.regionZ)
		}
		if got := regionSlot(test.chunkX, test.chunkZ); got != test.slot {
			t.Fatalf("chunk (%d,%d) slot = %d; want %d", test.chunkX, test.chunkZ, got, test.slot)
		}
	}
	if got := RegionFileName(-2, 3); got != "r.-2.3.mca" {
		t.Fatalf("region file name = %q", got)
	}
}

// Minecraft 26.2 keeps every dimension under dimensions/<namespace>/<path>,
// including the vanilla three. Older releases used region/, DIM-1/ and DIM1/.
// Writing to the layout the world does not use produces a successful export that
// the game silently ignores, so the world is asked which one it has.
func TestDimensionDirectoryFollowsTheLayoutTheWorldAlreadyUses(t *testing.T) {
	current := t.TempDir()
	if err := os.MkdirAll(filepath.Join(current, "dimensions", "minecraft", "overworld"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DimensionDirectory(current, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(current, "dimensions", "minecraft", "overworld", "region"); got != want {
		t.Fatalf("current layout resolved to %s; want %s", got, want)
	}

	legacy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(legacy, "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(legacy, "DIM-1", "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = DimensionDirectory(legacy, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(legacy, "region"); got != want {
		t.Fatalf("legacy overworld resolved to %s; want %s", got, want)
	}
	got, err = DimensionDirectory(legacy, "minecraft:the_nether")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(legacy, "DIM-1", "region"); got != want {
		t.Fatalf("legacy nether resolved to %s; want %s", got, want)
	}

	// A world with neither layout is new, so it gets the current one.
	empty := t.TempDir()
	got, err = DimensionDirectory(empty, "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(empty, "dimensions", "minecraft", "overworld", "region"); got != want {
		t.Fatalf("empty world resolved to %s; want %s", got, want)
	}
}

func TestDimensionDirectoryAlwaysUsesTheCurrentLayoutForModdedDimensions(t *testing.T) {
	world := t.TempDir()
	if err := os.MkdirAll(filepath.Join(world, "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DimensionDirectory(world, "examplemod:crystal_realm")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(world, "dimensions", "examplemod", "crystal_realm", "region"); got != want {
		t.Fatalf("modded dimension resolved to %s; want %s", got, want)
	}
}

func TestDimensionDirectoryRejectsUnsafeNames(t *testing.T) {
	world := t.TempDir()
	for _, dimension := range []string{"overworld", "", ":overworld", "minecraft:", "minecraft:../escape", "a:b:c"} {
		if _, err := DimensionDirectory(world, dimension); err == nil {
			t.Fatalf("dimension %q was accepted", dimension)
		}
	}
}

func TestRegionRejectsAForeignChunk(t *testing.T) {
	region := NewRegion(0, 0)
	if err := region.AddChunk(32, 0, tagCompound()); err == nil {
		t.Fatal("a chunk from another region was accepted")
	}
}

func TestRegionRejectsADuplicateChunk(t *testing.T) {
	components := testComponents(t)
	chunk, err := BuildChunk(1, 1, DataVersion26_2, components)
	if err != nil {
		t.Fatal(err)
	}
	region := NewRegion(0, 0)
	if err := region.AddChunk(1, 1, chunk); err != nil {
		t.Fatal(err)
	}
	if err := region.AddChunk(1, 1, chunk); err == nil {
		t.Fatal("the same chunk was written twice into one region")
	}
}

func TestRegionFileLayoutIsReadableAndSectorAligned(t *testing.T) {
	components := testComponents(t)
	chunk, err := BuildChunk(1, 2, DataVersion26_2, components)
	if err != nil {
		t.Fatal(err)
	}
	region := NewRegion(0, 0)
	if err := region.AddChunk(1, 2, chunk); err != nil {
		t.Fatal(err)
	}
	data := region.Bytes()

	if len(data) < headerSectors*sectorBytes {
		t.Fatalf("region is %d bytes; the header alone is %d", len(data), headerSectors*sectorBytes)
	}
	if len(data)%sectorBytes != 0 {
		t.Fatalf("region length %d is not sector aligned", len(data))
	}

	slot := regionSlot(1, 2)
	entry := data[slot*4 : slot*4+4]
	offset := int(entry[0])<<16 | int(entry[1])<<8 | int(entry[2])
	sectors := int(entry[3])
	if offset != headerSectors {
		t.Fatalf("first chunk starts at sector %d; want %d", offset, headerSectors)
	}
	if sectors < 1 {
		t.Fatal("location entry claims zero sectors")
	}

	frame := data[offset*sectorBytes:]
	length := int(binary.BigEndian.Uint32(frame[:4]))
	if length < 1 || 4+length > len(frame) {
		t.Fatalf("frame length %d does not fit in the region", length)
	}
	if frame[4] != compressionDeflate {
		t.Fatalf("compression byte = %d; want %d (VERSION_DEFLATE)", frame[4], compressionDeflate)
	}

	reader, err := zlib.NewReader(bytes.NewReader(frame[5 : 4+length]))
	if err != nil {
		t.Fatal(err)
	}
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	expected, err := EncodeNamed("", chunk)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decompressed, expected) {
		t.Fatal("the stored payload does not decompress to the encoded chunk")
	}
}

func TestRegionBytesAreDeterministic(t *testing.T) {
	build := func() []byte {
		components := testComponents(t)
		region := NewRegion(0, 0)
		for _, coordinate := range [][2]int32{{4, 4}, {1, 2}, {0, 0}} {
			chunk, err := BuildChunk(coordinate[0], coordinate[1], DataVersion26_2, components)
			if err != nil {
				t.Fatal(err)
			}
			if err := region.AddChunk(coordinate[0], coordinate[1], chunk); err != nil {
				t.Fatal(err)
			}
		}
		return region.Bytes()
	}
	if !bytes.Equal(build(), build()) {
		t.Fatal("region layout is not reproducible")
	}
}
