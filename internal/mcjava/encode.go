// Package mcjava is the language-neutral reference implementation of
// worldledger.minecraft.java.chunk/v1 canonical component encoding.
package mcjava

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	shapeDomain         = "worldledger.minecraft.java.chunk-shape/v1"
	blockSectionDomain  = "worldledger.minecraft.java.block-section/v1"
	biomeSectionDomain  = "worldledger.minecraft.java.biome-section/v1"
	blockEntitiesDomain = "worldledger.minecraft.java.block-entities/v1"

	BlockCount = 16 * 16 * 16
	BiomeCount = 4 * 4 * 4
)

type Limits struct {
	MaxComponentBytes  int
	MaxStringBytes     int
	MaxNBTBytes        int
	MaxNBTDepth        int
	MaxCollectionItems int
}

func DefaultLimits() Limits {
	return Limits{
		MaxComponentBytes:  64 << 20,
		MaxStringBytes:     1 << 20,
		MaxNBTBytes:        1 << 20,
		MaxNBTDepth:        64,
		MaxCollectionItems: 1 << 20,
	}
}

type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type BlockState struct {
	Name       string     `json:"name"`
	Properties []Property `json:"properties,omitempty"`
}

type BlockEntity struct {
	LocalX int
	BlockY int32
	LocalZ int
	Type   string
	NBT    NBTValue
}

func CanonicalBlockState(state BlockState) (string, error) {
	if err := validateResourceLocation(state.Name); err != nil {
		return "", fmt.Errorf("block resource location: %w", err)
	}
	if len(state.Properties) == 0 {
		return state.Name, nil
	}
	properties := append([]Property(nil), state.Properties...)
	sort.Slice(properties, func(i, j int) bool {
		return properties[i].Name < properties[j].Name
	})
	var out strings.Builder
	out.WriteString(state.Name)
	out.WriteByte('[')
	for index, property := range properties {
		if !validStateToken(property.Name) {
			return "", fmt.Errorf("invalid property name %q", property.Name)
		}
		if !validStateToken(property.Value) {
			return "", fmt.Errorf("invalid value %q for property %q", property.Value, property.Name)
		}
		if index > 0 && properties[index-1].Name == property.Name {
			return "", fmt.Errorf("duplicate property %q", property.Name)
		}
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteString(property.Name)
		out.WriteByte('=')
		out.WriteString(property.Value)
	}
	out.WriteByte(']')
	return out.String(), nil
}

func validStateToken(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || strings.ContainsRune(",=[]", r) {
			return false
		}
	}
	return true
}

func EncodeShape(minSectionY int32, sectionCount uint32) ([]byte, error) {
	if sectionCount == 0 {
		return nil, errors.New("section_count must be greater than zero")
	}
	maxSectionY := int64(minSectionY) + int64(sectionCount) - 1
	if maxSectionY > math.MaxInt32 {
		return nil, errors.New("section range exceeds int32")
	}
	limits := DefaultLimits()
	w := newCanonicalWriter(limits.MaxComponentBytes, limits.MaxStringBytes)
	if err := w.writeString(shapeDomain); err != nil {
		return nil, err
	}
	if err := w.writeI32(minSectionY); err != nil {
		return nil, err
	}
	if err := w.writeU32(sectionCount); err != nil {
		return nil, err
	}
	return w.bytes(), nil
}

func EncodeBlockSection(sectionY int32, states []BlockState) ([]byte, error) {
	return EncodeBlockSectionWithLimits(sectionY, states, DefaultLimits())
}

func EncodeBlockSectionWithLimits(sectionY int32, states []BlockState, limits Limits) ([]byte, error) {
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	if len(states) != BlockCount {
		return nil, fmt.Errorf("block section has %d states; want %d", len(states), BlockCount)
	}
	canonical := make([]string, len(states))
	paletteSet := make(map[string]struct{})
	for index, state := range states {
		encoded, err := CanonicalBlockState(state)
		if err != nil {
			return nil, fmt.Errorf("block state %d: %w", index, err)
		}
		if len(encoded) > limits.MaxStringBytes {
			return nil, fmt.Errorf("block state %d exceeds string limit", index)
		}
		canonical[index] = encoded
		paletteSet[encoded] = struct{}{}
	}
	palette := sortedKeys(paletteSet)
	if len(palette) == 0 || len(palette) > BlockCount {
		return nil, fmt.Errorf("invalid block palette size %d", len(palette))
	}
	paletteIndex := make(map[string]uint16, len(palette))
	for index, value := range palette {
		paletteIndex[value] = uint16(index)
	}

	w := newCanonicalWriter(limits.MaxComponentBytes, limits.MaxStringBytes)
	if err := w.writeString(blockSectionDomain); err != nil {
		return nil, err
	}
	if err := w.writeI32(sectionY); err != nil {
		return nil, err
	}
	if err := w.writeU16(uint16(len(palette))); err != nil {
		return nil, err
	}
	for _, value := range palette {
		if err := w.writeString(value); err != nil {
			return nil, err
		}
	}
	for _, value := range canonical {
		if err := w.writeU16(paletteIndex[value]); err != nil {
			return nil, err
		}
	}
	return w.bytes(), nil
}

func EncodeBiomeSection(sectionY int32, biomes []string) ([]byte, error) {
	return EncodeBiomeSectionWithLimits(sectionY, biomes, DefaultLimits())
}

func EncodeBiomeSectionWithLimits(sectionY int32, biomes []string, limits Limits) ([]byte, error) {
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	if len(biomes) != BiomeCount {
		return nil, fmt.Errorf("biome section has %d samples; want %d", len(biomes), BiomeCount)
	}
	paletteSet := make(map[string]struct{})
	for index, biome := range biomes {
		if err := validateResourceLocation(biome); err != nil {
			return nil, fmt.Errorf("biome %d: %w", index, err)
		}
		if len(biome) > limits.MaxStringBytes {
			return nil, fmt.Errorf("biome %d exceeds string limit", index)
		}
		paletteSet[biome] = struct{}{}
	}
	palette := sortedKeys(paletteSet)
	paletteIndex := make(map[string]uint16, len(palette))
	for index, value := range palette {
		paletteIndex[value] = uint16(index)
	}

	w := newCanonicalWriter(limits.MaxComponentBytes, limits.MaxStringBytes)
	if err := w.writeString(biomeSectionDomain); err != nil {
		return nil, err
	}
	if err := w.writeI32(sectionY); err != nil {
		return nil, err
	}
	if err := w.writeU16(uint16(len(palette))); err != nil {
		return nil, err
	}
	for _, value := range palette {
		if err := w.writeString(value); err != nil {
			return nil, err
		}
	}
	for _, value := range biomes {
		if err := w.writeU16(paletteIndex[value]); err != nil {
			return nil, err
		}
	}
	return w.bytes(), nil
}

func EncodeBlockEntities(entries []BlockEntity) ([]byte, error) {
	return EncodeBlockEntitiesWithLimits(entries, DefaultLimits())
}

func EncodeBlockEntitiesWithLimits(entries []BlockEntity, limits Limits) ([]byte, error) {
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	if len(entries) > limits.MaxCollectionItems || uint64(len(entries)) > math.MaxUint32 {
		return nil, errors.New("block entity count exceeds limit")
	}
	ordered := append([]BlockEntity(nil), entries...)
	for index, entry := range ordered {
		if entry.LocalX < 0 || entry.LocalX > 15 || entry.LocalZ < 0 || entry.LocalZ > 15 {
			return nil, fmt.Errorf("block entity %d has invalid local coordinates (%d,%d)", index, entry.LocalX, entry.LocalZ)
		}
		if err := validateResourceLocation(entry.Type); err != nil {
			return nil, fmt.Errorf("block entity %d type: %w", index, err)
		}
		if entry.NBT.Type != TagCompound {
			return nil, fmt.Errorf("block entity %d NBT root must be a compound", index)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
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
	})
	for index := 1; index < len(ordered); index++ {
		previous, current := ordered[index-1], ordered[index]
		if previous.BlockY == current.BlockY &&
			previous.LocalZ == current.LocalZ &&
			previous.LocalX == current.LocalX &&
			previous.Type == current.Type {
			return nil, fmt.Errorf("duplicate block entity key (%d,%d,%d,%s)", current.LocalX, current.BlockY, current.LocalZ, current.Type)
		}
	}

	w := newCanonicalWriter(limits.MaxComponentBytes, limits.MaxStringBytes)
	if err := w.writeString(blockEntitiesDomain); err != nil {
		return nil, err
	}
	if err := w.writeU32(uint32(len(ordered))); err != nil {
		return nil, err
	}
	for index, entry := range ordered {
		nbt, err := EncodeNBTWithLimits(entry.NBT, limits)
		if err != nil {
			return nil, fmt.Errorf("block entity %d NBT: %w", index, err)
		}
		if err := w.writeU8(uint8(entry.LocalX)); err != nil {
			return nil, err
		}
		if err := w.writeI32(entry.BlockY); err != nil {
			return nil, err
		}
		if err := w.writeU8(uint8(entry.LocalZ)); err != nil {
			return nil, err
		}
		if err := w.writeString(entry.Type); err != nil {
			return nil, err
		}
		if err := w.writeBytes(nbt); err != nil {
			return nil, err
		}
	}
	return w.bytes(), nil
}

func validateResourceLocation(value string) error {
	if !utf8.ValidString(value) || value == "" || strings.Count(value, ":") != 1 {
		return fmt.Errorf("invalid resource location %q", value)
	}
	parts := strings.SplitN(value, ":", 2)
	if parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid resource location %q", value)
	}
	for _, r := range parts[0] {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return fmt.Errorf("invalid resource location %q", value)
		}
	}
	for _, r := range parts[1] {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/') {
			return fmt.Errorf("invalid resource location %q", value)
		}
	}
	return nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func (limits Limits) normalized() (Limits, error) {
	defaults := DefaultLimits()
	if limits.MaxComponentBytes == 0 {
		limits.MaxComponentBytes = defaults.MaxComponentBytes
	}
	if limits.MaxStringBytes == 0 {
		limits.MaxStringBytes = defaults.MaxStringBytes
	}
	if limits.MaxNBTBytes == 0 {
		limits.MaxNBTBytes = defaults.MaxNBTBytes
	}
	if limits.MaxNBTDepth == 0 {
		limits.MaxNBTDepth = defaults.MaxNBTDepth
	}
	if limits.MaxCollectionItems == 0 {
		limits.MaxCollectionItems = defaults.MaxCollectionItems
	}
	if limits.MaxComponentBytes < 0 || limits.MaxStringBytes < 0 || limits.MaxNBTBytes < 0 || limits.MaxNBTDepth < 0 || limits.MaxCollectionItems < 0 {
		return Limits{}, errors.New("canonicalization limits must be positive")
	}
	if limits.MaxNBTBytes > limits.MaxComponentBytes {
		return Limits{}, errors.New("NBT byte limit exceeds component byte limit")
	}
	return limits, nil
}

type canonicalWriter struct {
	buffer         bytes.Buffer
	maxBytes       int
	maxStringBytes int
}

func newCanonicalWriter(maxBytes, maxStringBytes int) *canonicalWriter {
	return &canonicalWriter{maxBytes: maxBytes, maxStringBytes: maxStringBytes}
}

func (w *canonicalWriter) bytes() []byte {
	return append([]byte(nil), w.buffer.Bytes()...)
}

func (w *canonicalWriter) write(data []byte) error {
	if len(data) > w.maxBytes-w.buffer.Len() {
		return fmt.Errorf("canonical component exceeds %d bytes", w.maxBytes)
	}
	_, _ = w.buffer.Write(data)
	return nil
}

func (w *canonicalWriter) writeU8(value uint8) error {
	return w.write([]byte{value})
}

func (w *canonicalWriter) writeU16(value uint16) error {
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], value)
	return w.write(data[:])
}

func (w *canonicalWriter) writeU32(value uint32) error {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	return w.write(data[:])
}

func (w *canonicalWriter) writeU64(value uint64) error {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	return w.write(data[:])
}

func (w *canonicalWriter) writeI8(value int8) error {
	return w.writeU8(uint8(value))
}

func (w *canonicalWriter) writeI16(value int16) error {
	return w.writeU16(uint16(value))
}

func (w *canonicalWriter) writeI32(value int32) error {
	return w.writeU32(uint32(value))
}

func (w *canonicalWriter) writeI64(value int64) error {
	return w.writeU64(uint64(value))
}

func (w *canonicalWriter) writeString(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("string is not valid UTF-8")
	}
	if len(value) > w.maxStringBytes || uint64(len(value)) > math.MaxUint32 {
		return fmt.Errorf("string exceeds %d bytes", w.maxStringBytes)
	}
	if err := w.writeU32(uint32(len(value))); err != nil {
		return err
	}
	return w.write([]byte(value))
}

func (w *canonicalWriter) writeBytes(value []byte) error {
	if uint64(len(value)) > math.MaxUint32 {
		return errors.New("byte sequence exceeds uint32 length")
	}
	if err := w.writeU32(uint32(len(value))); err != nil {
		return err
	}
	return w.write(value)
}
