package mcjava

import (
	"strings"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newTestWriter() *canonicalWriter {
	limits := DefaultLimits()
	return newCanonicalWriter(limits.MaxComponentBytes, limits.MaxStringBytes)
}

func blockSectionBytes(t *testing.T, sectionY int32, palette []string, indices []uint16) []byte {
	t.Helper()
	w := newTestWriter()
	must(t, w.writeString(blockSectionDomain))
	must(t, w.writeI32(sectionY))
	must(t, w.writeU16(uint16(len(palette))))
	for _, entry := range palette {
		must(t, w.writeString(entry))
	}
	for _, index := range indices {
		must(t, w.writeU16(index))
	}
	return w.bytes()
}

func repeatIndices(value uint16, count int) []uint16 {
	out := make([]uint16, count)
	for position := range out {
		out[position] = value
	}
	return out
}

func alternatingIndices(count int) []uint16 {
	out := make([]uint16, count)
	for position := range out {
		out[position] = uint16(position % 2)
	}
	return out
}

func TestDecodeBlockSectionAcceptsCanonicalBytes(t *testing.T) {
	data := blockSectionBytes(t, -4, []string{"minecraft:air"}, repeatIndices(0, BlockCount))
	section, err := DecodeBlockSection(data)
	if err != nil {
		t.Fatal(err)
	}
	if section.SectionY != -4 {
		t.Fatalf("section_y = %d; want -4", section.SectionY)
	}
	state, err := section.StateAt(15, 15, 15)
	if err != nil {
		t.Fatal(err)
	}
	if state != "minecraft:air" {
		t.Fatalf("state = %q; want minecraft:air", state)
	}
	if _, err := section.StateAt(16, 0, 0); err == nil {
		t.Fatal("StateAt accepted a position outside the section")
	}
}

func TestDecodeBlockSectionRejectsNonCanonicalBytes(t *testing.T) {
	valid := blockSectionBytes(t, 0, []string{"minecraft:air"}, repeatIndices(0, BlockCount))

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "palette out of order",
			data: blockSectionBytes(t, 0, []string{"minecraft:stone", "minecraft:air"}, alternatingIndices(BlockCount)),
			want: "canonical order",
		},
		{
			name: "duplicate palette entry",
			data: blockSectionBytes(t, 0, []string{"minecraft:air", "minecraft:air"}, repeatIndices(0, BlockCount)),
			want: "duplicate",
		},
		{
			name: "unused palette entry",
			data: blockSectionBytes(t, 0, []string{"minecraft:air", "minecraft:stone"}, repeatIndices(0, BlockCount)),
			want: "unused",
		},
		{
			name: "index outside palette",
			data: blockSectionBytes(t, 0, []string{"minecraft:air"}, repeatIndices(1, BlockCount)),
			want: "references palette entry",
		},
		{
			name: "empty palette",
			data: blockSectionBytes(t, 0, nil, repeatIndices(0, BlockCount)),
			want: "invalid block palette size",
		},
		{
			name: "property order is not canonical",
			data: blockSectionBytes(t, 0, []string{"minecraft:oak_stairs[half=bottom,facing=north]"}, repeatIndices(0, BlockCount)),
			want: "not canonical",
		},
		{
			name: "unexpected domain",
			data: func() []byte {
				w := newTestWriter()
				must(t, w.writeString(biomeSectionDomain))
				must(t, w.writeI32(0))
				return w.bytes()
			}(),
			want: "unexpected canonical domain",
		},
		{
			name: "trailing bytes",
			data: append(append([]byte(nil), valid...), 0),
			want: "trailing bytes",
		},
		{
			name: "truncated",
			data: valid[:len(valid)-1],
			want: "unexpected end",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeBlockSection(test.data)
			if err == nil {
				t.Fatal("non-canonical block section decoded without error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want it to mention %q", err, test.want)
			}
		})
	}
}

func TestDecodeShapeRejectsInvalidRanges(t *testing.T) {
	valid, err := EncodeShape(-4, 24)
	if err != nil {
		t.Fatal(err)
	}
	shape, err := DecodeShape(valid)
	if err != nil {
		t.Fatal(err)
	}
	if shape.MinSectionY != -4 || shape.SectionCount != 24 || shape.MaxSectionY() != 19 {
		t.Fatalf("decoded shape = %#v", shape)
	}

	zeroCount := func() []byte {
		w := newTestWriter()
		must(t, w.writeString(shapeDomain))
		must(t, w.writeI32(0))
		must(t, w.writeU32(0))
		return w.bytes()
	}()
	if _, err := DecodeShape(zeroCount); err == nil {
		t.Fatal("shape with zero sections decoded without error")
	}

	if _, err := DecodeShape(append(append([]byte(nil), valid...), 0)); err == nil {
		t.Fatal("shape with trailing bytes decoded without error")
	}
}

func TestDecodeBiomeSectionRejectsInvalidResourceLocations(t *testing.T) {
	w := newTestWriter()
	must(t, w.writeString(biomeSectionDomain))
	must(t, w.writeI32(0))
	must(t, w.writeU16(1))
	must(t, w.writeString("plains"))
	for position := 0; position < BiomeCount; position++ {
		must(t, w.writeU16(0))
	}
	if _, err := DecodeBiomeSection(w.bytes()); err == nil {
		t.Fatal("biome palette without a namespace decoded without error")
	}
}

type rawBlockEntity struct {
	localX uint8
	blockY int32
	localZ uint8
	kind   string
	nbt    []byte
}

func blockEntitiesBytes(t *testing.T, entries []rawBlockEntity) []byte {
	t.Helper()
	w := newTestWriter()
	must(t, w.writeString(blockEntitiesDomain))
	must(t, w.writeU32(uint32(len(entries))))
	for _, entry := range entries {
		must(t, w.writeU8(entry.localX))
		must(t, w.writeI32(entry.blockY))
		must(t, w.writeU8(entry.localZ))
		must(t, w.writeString(entry.kind))
		must(t, w.writeBytes(entry.nbt))
	}
	return w.bytes()
}

func emptyCompound(t *testing.T) []byte {
	t.Helper()
	data, err := EncodeNBT(NBTValue{Type: TagCompound})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeBlockEntitiesEnforcesCanonicalOrder(t *testing.T) {
	compound := emptyCompound(t)
	sign := "minecraft:sign"

	tests := []struct {
		name    string
		entries []rawBlockEntity
		want    string
	}{
		{
			name: "entries out of order",
			entries: []rawBlockEntity{
				{localX: 0, blockY: 70, localZ: 0, kind: sign, nbt: compound},
				{localX: 0, blockY: 64, localZ: 0, kind: sign, nbt: compound},
			},
			want: "canonical order",
		},
		{
			name: "duplicate key",
			entries: []rawBlockEntity{
				{localX: 1, blockY: 64, localZ: 2, kind: sign, nbt: compound},
				{localX: 1, blockY: 64, localZ: 2, kind: sign, nbt: compound},
			},
			want: "duplicate block entity key",
		},
		{
			name:    "local coordinate outside the chunk",
			entries: []rawBlockEntity{{localX: 16, blockY: 64, localZ: 0, kind: sign, nbt: compound}},
			want:    "invalid local coordinates",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeBlockEntities(blockEntitiesBytes(t, test.entries))
			if err == nil {
				t.Fatal("non-canonical block entities decoded without error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want it to mention %q", err, test.want)
			}
		})
	}
}

func TestDecodeBlockEntitiesRejectsNonCompoundRoot(t *testing.T) {
	payload, err := EncodeNBT(NBTValue{Type: TagString, String: "not a compound"})
	if err != nil {
		t.Fatal(err)
	}
	entries := []rawBlockEntity{{localX: 0, blockY: 64, localZ: 0, kind: "minecraft:sign", nbt: payload}}
	if _, err := DecodeBlockEntities(blockEntitiesBytes(t, entries)); err == nil {
		t.Fatal("block entity with a non-compound NBT root decoded without error")
	}
}

func TestDecodeBlockEntitiesRejectsImpossibleEntryCount(t *testing.T) {
	w := newTestWriter()
	must(t, w.writeString(blockEntitiesDomain))
	must(t, w.writeU32(0xFFFFFFFF))
	if _, err := DecodeBlockEntities(w.bytes()); err == nil {
		t.Fatal("an entry count larger than the component decoded without error")
	}
}

func TestDecodeNBTEnforcesCanonicalCompounds(t *testing.T) {
	byteTag := []byte{uint8(TagByte), 7}

	compound := func(keys ...string) []byte {
		w := newTestWriter()
		must(t, w.writeU8(uint8(TagCompound)))
		must(t, w.writeU32(uint32(len(keys))))
		for _, key := range keys {
			must(t, w.writeString(key))
			must(t, w.write(byteTag))
		}
		return w.bytes()
	}

	if _, err := DecodeNBT(compound("beta", "alpha")); err == nil {
		t.Fatal("compound with unsorted keys decoded without error")
	}
	if _, err := DecodeNBT(compound("alpha", "alpha")); err == nil {
		t.Fatal("compound with a duplicate key decoded without error")
	}
	if _, err := DecodeNBT(compound("alpha", "beta")); err != nil {
		t.Fatalf("canonical compound failed to decode: %v", err)
	}
}

func TestDecodeNBTRejectsInvalidStructures(t *testing.T) {
	standaloneEnd := []byte{uint8(TagEnd)}
	if _, err := DecodeNBT(standaloneEnd); err == nil {
		t.Fatal("standalone End decoded without error")
	}

	nonEmptyEndList := func() []byte {
		w := newTestWriter()
		must(t, w.writeU8(uint8(TagList)))
		must(t, w.writeU8(uint8(TagEnd)))
		must(t, w.writeU32(1))
		return w.bytes()
	}()
	if _, err := DecodeNBT(nonEmptyEndList); err == nil {
		t.Fatal("non-empty End list decoded without error")
	}

	oversizedIntArray := func() []byte {
		w := newTestWriter()
		must(t, w.writeU8(uint8(TagIntArray)))
		must(t, w.writeU32(0xFFFF))
		return w.bytes()
	}()
	if _, err := DecodeNBT(oversizedIntArray); err == nil {
		t.Fatal("int array longer than the payload decoded without error")
	}
}

func TestDecodeNBTRoundTripsSpecialValues(t *testing.T) {
	original := NBTValue{Type: TagCompound, Compound: []NamedNBT{
		{Name: "bytes", Value: NBTValue{Type: TagByteArray, ByteArray: []byte{0, 255, 7}}},
		{Name: "double", Value: NBTValue{Type: TagDouble, DoubleBits: 0xFFF8000000000000}},
		{Name: "float", Value: NBTValue{Type: TagFloat, FloatBits: 0x7FC00000}},
		{Name: "longs", Value: NBTValue{Type: TagLongArray, LongArray: []int64{-1, 0, 1}}},
		{Name: "nested", Value: NBTValue{Type: TagList, List: &NBTList{
			ElementType: TagCompound,
			Values: []NBTValue{
				{Type: TagCompound, Compound: []NamedNBT{{Name: "a", Value: NBTValue{Type: TagInt, Int: -5}}}},
			},
		}}},
	}}

	encoded, err := EncodeNBT(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeNBT(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := EncodeNBT(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(encoded) {
		t.Fatal("canonical NBT did not survive a decode/encode round trip")
	}
}

func TestParseBlockStateIsInverseOfCanonicalBlockState(t *testing.T) {
	values := []string{
		"minecraft:stone",
		"minecraft:oak_log[axis=y]",
		"minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=false]",
		"examplemod:marble",
	}
	for _, value := range values {
		state, err := ParseBlockState(value)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		canonical, err := CanonicalBlockState(state)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if canonical != value {
			t.Fatalf("round trip produced %q; want %q", canonical, value)
		}
	}
}

func TestParseBlockStateRejectsMalformedValues(t *testing.T) {
	values := []string{
		"",
		"stone",
		"minecraft:stone[",
		"minecraft:stone[]",
		"minecraft:stone[axis]",
		"minecraft:stone[axis=]",
		"minecraft:stone[=y]",
		"minecraft:Stone",
	}
	for _, value := range values {
		if _, err := ParseBlockState(value); err == nil {
			t.Fatalf("malformed block state %q parsed without error", value)
		}
	}
}
