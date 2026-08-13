package mcjava

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Decoding accepts canonical bytes only. A component that decodes without error
// re-encodes to the identical bytes, so a successful decode is also proof that
// the stored bytes are in canonical form.

type Shape struct {
	MinSectionY  int32  `json:"min_section_y"`
	SectionCount uint32 `json:"section_count"`
}

func (s Shape) MaxSectionY() int32 {
	return s.MinSectionY + int32(s.SectionCount) - 1
}

func (s Shape) Contains(sectionY int32) bool {
	return s.SectionCount > 0 && sectionY >= s.MinSectionY && sectionY <= s.MaxSectionY()
}

func (s Shape) SectionYs() []int32 {
	out := make([]int32, 0, s.SectionCount)
	for offset := uint32(0); offset < s.SectionCount; offset++ {
		out = append(out, s.MinSectionY+int32(offset))
	}
	return out
}

type BlockSection struct {
	SectionY int32    `json:"section_y"`
	Palette  []string `json:"palette"`
	Indices  []uint16 `json:"indices"`
}

func (s BlockSection) StateAt(x, y, z int) (string, error) {
	if x < 0 || x > 15 || y < 0 || y > 15 || z < 0 || z > 15 {
		return "", fmt.Errorf("block position (%d,%d,%d) is outside the section", x, y, z)
	}
	return s.Palette[s.Indices[(y<<8)|(z<<4)|x]], nil
}

func (s BlockSection) States() []string {
	out := make([]string, len(s.Indices))
	for position, index := range s.Indices {
		out[position] = s.Palette[index]
	}
	return out
}

// ParsedPalette parses each canonical palette entry once. Consumers that need
// structured block states should prefer it over parsing every position.
func (s BlockSection) ParsedPalette() ([]BlockState, error) {
	out := make([]BlockState, len(s.Palette))
	for index, entry := range s.Palette {
		state, err := ParseBlockState(entry)
		if err != nil {
			return nil, fmt.Errorf("block palette entry %d: %w", index, err)
		}
		out[index] = state
	}
	return out, nil
}

// ParsedStates expands the section into one parsed block state per position in
// canonical order.
func (s BlockSection) ParsedStates() ([]BlockState, error) {
	palette, err := s.ParsedPalette()
	if err != nil {
		return nil, err
	}
	out := make([]BlockState, len(s.Indices))
	for position, index := range s.Indices {
		out[position] = palette[index]
	}
	return out, nil
}

type BiomeSection struct {
	SectionY int32    `json:"section_y"`
	Palette  []string `json:"palette"`
	Indices  []uint16 `json:"indices"`
}

func (s BiomeSection) BiomeAt(x, y, z int) (string, error) {
	if x < 0 || x > 3 || y < 0 || y > 3 || z < 0 || z > 3 {
		return "", fmt.Errorf("biome quart position (%d,%d,%d) is outside the section", x, y, z)
	}
	return s.Palette[s.Indices[(y<<4)|(z<<2)|x]], nil
}

func (s BiomeSection) Biomes() []string {
	out := make([]string, len(s.Indices))
	for position, index := range s.Indices {
		out[position] = s.Palette[index]
	}
	return out
}

func DecodeShape(data []byte) (Shape, error) {
	return DecodeShapeWithLimits(data, DefaultLimits())
}

func DecodeShapeWithLimits(data []byte, limits Limits) (Shape, error) {
	r, err := newCanonicalReader(data, limits)
	if err != nil {
		return Shape{}, err
	}
	if err := r.expectDomain(shapeDomain); err != nil {
		return Shape{}, err
	}
	minSectionY, err := r.readI32()
	if err != nil {
		return Shape{}, fmt.Errorf("min_section_y: %w", err)
	}
	sectionCount, err := r.readU32()
	if err != nil {
		return Shape{}, fmt.Errorf("section_count: %w", err)
	}
	if sectionCount == 0 {
		return Shape{}, errors.New("section_count must be greater than zero")
	}
	if int64(minSectionY)+int64(sectionCount)-1 > math.MaxInt32 {
		return Shape{}, errors.New("section range exceeds int32")
	}
	if err := r.expectEnd(); err != nil {
		return Shape{}, err
	}
	return Shape{MinSectionY: minSectionY, SectionCount: sectionCount}, nil
}

func DecodeBlockSection(data []byte) (BlockSection, error) {
	return DecodeBlockSectionWithLimits(data, DefaultLimits())
}

func DecodeBlockSectionWithLimits(data []byte, limits Limits) (BlockSection, error) {
	r, err := newCanonicalReader(data, limits)
	if err != nil {
		return BlockSection{}, err
	}
	if err := r.expectDomain(blockSectionDomain); err != nil {
		return BlockSection{}, err
	}
	sectionY, err := r.readI32()
	if err != nil {
		return BlockSection{}, fmt.Errorf("section_y: %w", err)
	}
	palette, err := r.readPalette(BlockCount, "block")
	if err != nil {
		return BlockSection{}, err
	}
	for index, entry := range palette {
		state, err := ParseBlockState(entry)
		if err != nil {
			return BlockSection{}, fmt.Errorf("block palette entry %d: %w", index, err)
		}
		canonical, err := CanonicalBlockState(state)
		if err != nil {
			return BlockSection{}, fmt.Errorf("block palette entry %d: %w", index, err)
		}
		if canonical != entry {
			return BlockSection{}, fmt.Errorf("block palette entry %d is not canonical", index)
		}
	}
	indices, err := r.readIndices(BlockCount, len(palette), "block")
	if err != nil {
		return BlockSection{}, err
	}
	if err := r.expectEnd(); err != nil {
		return BlockSection{}, err
	}
	return BlockSection{SectionY: sectionY, Palette: palette, Indices: indices}, nil
}

func DecodeBiomeSection(data []byte) (BiomeSection, error) {
	return DecodeBiomeSectionWithLimits(data, DefaultLimits())
}

func DecodeBiomeSectionWithLimits(data []byte, limits Limits) (BiomeSection, error) {
	r, err := newCanonicalReader(data, limits)
	if err != nil {
		return BiomeSection{}, err
	}
	if err := r.expectDomain(biomeSectionDomain); err != nil {
		return BiomeSection{}, err
	}
	sectionY, err := r.readI32()
	if err != nil {
		return BiomeSection{}, fmt.Errorf("section_y: %w", err)
	}
	palette, err := r.readPalette(BiomeCount, "biome")
	if err != nil {
		return BiomeSection{}, err
	}
	for index, entry := range palette {
		if err := validateResourceLocation(entry); err != nil {
			return BiomeSection{}, fmt.Errorf("biome palette entry %d: %w", index, err)
		}
	}
	indices, err := r.readIndices(BiomeCount, len(palette), "biome")
	if err != nil {
		return BiomeSection{}, err
	}
	if err := r.expectEnd(); err != nil {
		return BiomeSection{}, err
	}
	return BiomeSection{SectionY: sectionY, Palette: palette, Indices: indices}, nil
}

func DecodeBlockEntities(data []byte) ([]BlockEntity, error) {
	return DecodeBlockEntitiesWithLimits(data, DefaultLimits())
}

func DecodeBlockEntitiesWithLimits(data []byte, limits Limits) ([]BlockEntity, error) {
	r, err := newCanonicalReader(data, limits)
	if err != nil {
		return nil, err
	}
	if err := r.expectDomain(blockEntitiesDomain); err != nil {
		return nil, err
	}
	count, err := r.readU32()
	if err != nil {
		return nil, fmt.Errorf("entry_count: %w", err)
	}
	// A block-entity entry is at least u8+i32+u8+u32+u32 bytes before payloads,
	// so an entry count larger than the remaining bytes allow cannot be honest.
	const minimumEntryBytes = 14
	if uint64(count) > uint64(r.limits.MaxCollectionItems) || uint64(count) > uint64(r.remaining()/minimumEntryBytes) {
		return nil, fmt.Errorf("entry count %d exceeds limit", count)
	}
	entries := make([]BlockEntity, 0, count)
	for index := 0; index < int(count); index++ {
		entry, err := r.readBlockEntity()
		if err != nil {
			return nil, fmt.Errorf("block entity %d: %w", index, err)
		}
		if index > 0 {
			previous := entries[index-1]
			if !blockEntityLess(previous, entry) {
				if blockEntityKeyEqual(previous, entry) {
					return nil, fmt.Errorf("duplicate block entity key (%d,%d,%d,%s)", entry.LocalX, entry.BlockY, entry.LocalZ, entry.Type)
				}
				return nil, fmt.Errorf("block entity %d is not in canonical order", index)
			}
		}
		entries = append(entries, entry)
	}
	if err := r.expectEnd(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *canonicalReader) readBlockEntity() (BlockEntity, error) {
	localX, err := r.readU8()
	if err != nil {
		return BlockEntity{}, fmt.Errorf("local_x: %w", err)
	}
	blockY, err := r.readI32()
	if err != nil {
		return BlockEntity{}, fmt.Errorf("block_y: %w", err)
	}
	localZ, err := r.readU8()
	if err != nil {
		return BlockEntity{}, fmt.Errorf("local_z: %w", err)
	}
	if localX > 15 || localZ > 15 {
		return BlockEntity{}, fmt.Errorf("invalid local coordinates (%d,%d)", localX, localZ)
	}
	blockEntityType, err := r.readString()
	if err != nil {
		return BlockEntity{}, fmt.Errorf("type: %w", err)
	}
	if err := validateResourceLocation(blockEntityType); err != nil {
		return BlockEntity{}, fmt.Errorf("type: %w", err)
	}
	payload, err := r.readBytes()
	if err != nil {
		return BlockEntity{}, fmt.Errorf("nbt: %w", err)
	}
	value, err := DecodeNBTWithLimits(payload, r.limits)
	if err != nil {
		return BlockEntity{}, fmt.Errorf("nbt: %w", err)
	}
	if value.Type != TagCompound {
		return BlockEntity{}, errors.New("nbt root must be a compound")
	}
	return BlockEntity{
		LocalX: int(localX),
		BlockY: blockY,
		LocalZ: int(localZ),
		Type:   blockEntityType,
		NBT:    value,
	}, nil
}

func blockEntityLess(left, right BlockEntity) bool {
	if left.BlockY != right.BlockY {
		return left.BlockY < right.BlockY
	}
	if left.LocalZ != right.LocalZ {
		return left.LocalZ < right.LocalZ
	}
	if left.LocalX != right.LocalX {
		return left.LocalX < right.LocalX
	}
	return left.Type < right.Type
}

func blockEntityKeyEqual(left, right BlockEntity) bool {
	return left.BlockY == right.BlockY &&
		left.LocalZ == right.LocalZ &&
		left.LocalX == right.LocalX &&
		left.Type == right.Type
}

func ParseBlockState(value string) (BlockState, error) {
	if !utf8.ValidString(value) {
		return BlockState{}, errors.New("block state is not valid UTF-8")
	}
	open := strings.IndexByte(value, '[')
	if open < 0 {
		if err := validateResourceLocation(value); err != nil {
			return BlockState{}, err
		}
		return BlockState{Name: value}, nil
	}
	if !strings.HasSuffix(value, "]") {
		return BlockState{}, fmt.Errorf("block state %q has an unterminated property list", value)
	}
	name := value[:open]
	if err := validateResourceLocation(name); err != nil {
		return BlockState{}, err
	}
	body := value[open+1 : len(value)-1]
	if body == "" {
		return BlockState{}, fmt.Errorf("block state %q has an empty property list", value)
	}
	fields := strings.Split(body, ",")
	properties := make([]Property, 0, len(fields))
	for _, field := range fields {
		separator := strings.IndexByte(field, '=')
		if separator < 0 {
			return BlockState{}, fmt.Errorf("block state %q has a property without a value", value)
		}
		property := Property{Name: field[:separator], Value: field[separator+1:]}
		if !validStateToken(property.Name) {
			return BlockState{}, fmt.Errorf("invalid property name %q", property.Name)
		}
		if !validStateToken(property.Value) {
			return BlockState{}, fmt.Errorf("invalid value %q for property %q", property.Value, property.Name)
		}
		properties = append(properties, property)
	}
	return BlockState{Name: name, Properties: properties}, nil
}

type canonicalReader struct {
	data   []byte
	offset int
	limits Limits
}

func newCanonicalReader(data []byte, limits Limits) (*canonicalReader, error) {
	normalized, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	if len(data) > normalized.MaxComponentBytes {
		return nil, fmt.Errorf("canonical component exceeds %d bytes", normalized.MaxComponentBytes)
	}
	return &canonicalReader{data: data, limits: normalized}, nil
}

func (r *canonicalReader) remaining() int {
	return len(r.data) - r.offset
}

func (r *canonicalReader) expectEnd() error {
	if r.remaining() != 0 {
		return fmt.Errorf("canonical component has %d trailing bytes", r.remaining())
	}
	return nil
}

func (r *canonicalReader) expectDomain(domain string) error {
	value, err := r.readString()
	if err != nil {
		return fmt.Errorf("domain: %w", err)
	}
	if value != domain {
		return fmt.Errorf("unexpected canonical domain %q; want %q", value, domain)
	}
	return nil
}

func (r *canonicalReader) take(count int) ([]byte, error) {
	if count < 0 || count > r.remaining() {
		return nil, fmt.Errorf("unexpected end of canonical component")
	}
	out := r.data[r.offset : r.offset+count]
	r.offset += count
	return out, nil
}

func (r *canonicalReader) readU8() (uint8, error) {
	data, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (r *canonicalReader) readU16() (uint16, error) {
	data, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (r *canonicalReader) readU32() (uint32, error) {
	data, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), nil
}

func (r *canonicalReader) readU64() (uint64, error) {
	data, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(data), nil
}

func (r *canonicalReader) readI8() (int8, error) {
	value, err := r.readU8()
	return int8(value), err
}

func (r *canonicalReader) readI16() (int16, error) {
	value, err := r.readU16()
	return int16(value), err
}

func (r *canonicalReader) readI32() (int32, error) {
	value, err := r.readU32()
	return int32(value), err
}

func (r *canonicalReader) readI64() (int64, error) {
	value, err := r.readU64()
	return int64(value), err
}

func (r *canonicalReader) readString() (string, error) {
	length, err := r.readU32()
	if err != nil {
		return "", err
	}
	if uint64(length) > uint64(r.limits.MaxStringBytes) {
		return "", fmt.Errorf("string exceeds %d bytes", r.limits.MaxStringBytes)
	}
	data, err := r.take(int(length))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("string is not valid UTF-8")
	}
	return string(data), nil
}

func (r *canonicalReader) readBytes() ([]byte, error) {
	length, err := r.readU32()
	if err != nil {
		return nil, err
	}
	return r.take(int(length))
}

func (r *canonicalReader) readPalette(maxEntries int, what string) ([]string, error) {
	count, err := r.readU16()
	if err != nil {
		return nil, fmt.Errorf("%s palette_count: %w", what, err)
	}
	if count == 0 || int(count) > maxEntries {
		return nil, fmt.Errorf("invalid %s palette size %d", what, count)
	}
	palette := make([]string, 0, count)
	for index := 0; index < int(count); index++ {
		value, err := r.readString()
		if err != nil {
			return nil, fmt.Errorf("%s palette entry %d: %w", what, index, err)
		}
		if index > 0 {
			previous := palette[index-1]
			if value == previous {
				return nil, fmt.Errorf("%s palette contains duplicate entry %q", what, value)
			}
			if value < previous {
				return nil, fmt.Errorf("%s palette entry %d is not in canonical order", what, index)
			}
		}
		palette = append(palette, value)
	}
	return palette, nil
}

func (r *canonicalReader) readIndices(count, paletteSize int, what string) ([]uint16, error) {
	indices := make([]uint16, count)
	used := make([]bool, paletteSize)
	for position := 0; position < count; position++ {
		index, err := r.readU16()
		if err != nil {
			return nil, fmt.Errorf("%s index %d: %w", what, position, err)
		}
		if int(index) >= paletteSize {
			return nil, fmt.Errorf("%s index %d references palette entry %d of %d", what, position, index, paletteSize)
		}
		indices[position] = index
		used[index] = true
	}
	for index, seen := range used {
		if !seen {
			return nil, fmt.Errorf("%s palette entry %d is unused", what, index)
		}
	}
	return indices, nil
}
