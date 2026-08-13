package anvil

import (
	"fmt"
	"math"
	"sort"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
)

// DataVersion26_2 is the world_version declared by the pinned Minecraft 26.2
// client. Writing chunks with a different value than the target world lets the
// game silently run data fixers over data that was never in an older format.
const DataVersion26_2 = 4903

// UnknownBiome is written for a section whose terrain is known but whose biome
// samples are not. It is a real registry entry that reads as "nothing here"
// rather than a plausible guess such as plains.
const UnknownBiome = "minecraft:the_void"

const chunkStatusFull = "minecraft:full"

// ChunkComponents is one chunk's decoded canonical state. A nil map entry means
// the component was never observed; it is not an empty section.
type ChunkComponents struct {
	Shape         mcjava.Shape
	Blocks        map[int32]mcjava.BlockSection
	Biomes        map[int32]mcjava.BiomeSection
	BlockEntities []mcjava.BlockEntity
	// HasBlockEntities distinguishes a known-empty block entity baseline from
	// the absence of any baseline.
	HasBlockEntities bool
}

// SectionYs returns the section coordinates that will be written, which are the
// ones with observed terrain. A section whose biomes were observed but whose
// blocks were not is skipped rather than filled with fabricated air.
func (c ChunkComponents) SectionYs() []int32 {
	out := make([]int32, 0, len(c.Blocks))
	for sectionY := range c.Blocks {
		out = append(out, sectionY)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func BuildChunk(chunkX, chunkZ int32, dataVersion int32, components ChunkComponents) (mcjava.NBTValue, error) {
	if components.Shape.SectionCount == 0 {
		return mcjava.NBTValue{}, fmt.Errorf("chunk (%d,%d) has no observed shape", chunkX, chunkZ)
	}

	sections := make([]mcjava.NBTValue, 0, len(components.Blocks))
	for _, sectionY := range components.SectionYs() {
		if !components.Shape.Contains(sectionY) {
			return mcjava.NBTValue{}, fmt.Errorf("chunk (%d,%d) has a block section at Y=%d outside its shape", chunkX, chunkZ, sectionY)
		}
		section, err := buildSection(sectionY, components.Blocks[sectionY], components.Biomes[sectionY])
		if err != nil {
			return mcjava.NBTValue{}, fmt.Errorf("chunk (%d,%d) section %d: %w", chunkX, chunkZ, sectionY, err)
		}
		sections = append(sections, section)
	}

	blockEntities, err := buildBlockEntities(chunkX, chunkZ, components)
	if err != nil {
		return mcjava.NBTValue{}, fmt.Errorf("chunk (%d,%d): %w", chunkX, chunkZ, err)
	}

	return tagCompound(
		named("DataVersion", tagInt(dataVersion)),
		named("xPos", tagInt(chunkX)),
		named("yPos", tagInt(components.Shape.MinSectionY)),
		named("zPos", tagInt(chunkZ)),
		named("Status", tagString(chunkStatusFull)),
		named("LastUpdate", tagLong(0)),
		named("InhabitedTime", tagLong(0)),
		// Lighting is not part of the observed collection boundary, so the game
		// is asked to relight rather than trust fabricated light arrays.
		named("isLightOn", tagByte(0)),
		named("sections", tagList(mcjava.TagCompound, sections...)),
		named("block_entities", tagList(mcjava.TagCompound, blockEntities...)),
		named("Heightmaps", tagCompound()),
		named("block_ticks", tagList(mcjava.TagCompound)),
		named("fluid_ticks", tagList(mcjava.TagCompound)),
		named("PostProcessing", tagList(mcjava.TagList)),
		named("structures", tagCompound(
			named("starts", tagCompound()),
			named("References", tagCompound()),
		)),
	), nil
}

func buildSection(sectionY int32, blocks mcjava.BlockSection, biomes mcjava.BiomeSection) (mcjava.NBTValue, error) {
	if sectionY < math.MinInt8 || sectionY > math.MaxInt8 {
		return mcjava.NBTValue{}, fmt.Errorf("section Y=%d does not fit in a byte", sectionY)
	}

	blockPalette, err := blocks.ParsedPalette()
	if err != nil {
		return mcjava.NBTValue{}, err
	}
	paletteEntries := make([]mcjava.NBTValue, len(blockPalette))
	for index, state := range blockPalette {
		paletteEntries[index] = buildBlockStateEntry(state)
	}
	blockStates := []mcjava.NamedNBT{named("palette", tagList(mcjava.TagCompound, paletteEntries...))}
	if packed := packIndices(blocks.Indices, len(blockPalette), 4); packed != nil {
		blockStates = append(blockStates, named("data", tagLongArray(packed)))
	}

	biomePalette := []string{UnknownBiome}
	biomeIndices := make([]uint16, mcjava.BiomeCount)
	if len(biomes.Palette) > 0 {
		biomePalette = biomes.Palette
		biomeIndices = biomes.Indices
	}
	biomeEntries := make([]mcjava.NBTValue, len(biomePalette))
	for index, biome := range biomePalette {
		biomeEntries[index] = tagString(biome)
	}
	biomeContainer := []mcjava.NamedNBT{named("palette", tagList(mcjava.TagString, biomeEntries...))}
	if packed := packIndices(biomeIndices, len(biomePalette), 1); packed != nil {
		biomeContainer = append(biomeContainer, named("data", tagLongArray(packed)))
	}

	return tagCompound(
		named("Y", tagByte(int8(sectionY))),
		named("block_states", tagCompound(blockStates...)),
		named("biomes", tagCompound(biomeContainer...)),
	), nil
}

func buildBlockStateEntry(state mcjava.BlockState) mcjava.NBTValue {
	entry := []mcjava.NamedNBT{named("Name", tagString(state.Name))}
	if len(state.Properties) > 0 {
		properties := make([]mcjava.NamedNBT, len(state.Properties))
		for index, property := range state.Properties {
			properties[index] = named(property.Name, tagString(property.Value))
		}
		entry = append(entry, named("Properties", tagCompound(properties...)))
	}
	return tagCompound(entry...)
}

func buildBlockEntities(chunkX, chunkZ int32, components ChunkComponents) ([]mcjava.NBTValue, error) {
	if !components.HasBlockEntities {
		return nil, nil
	}
	out := make([]mcjava.NBTValue, 0, len(components.BlockEntities))
	for _, entry := range components.BlockEntities {
		if entry.NBT.Type != mcjava.TagCompound {
			return nil, fmt.Errorf("block entity at (%d,%d,%d) has a non-compound payload", entry.LocalX, entry.BlockY, entry.LocalZ)
		}
		// The canonical payload may already carry id/x/y/z. The chunk-relative
		// coordinates and the type from the component are authoritative, so they
		// replace whatever the network representation happened to contain.
		fields := []mcjava.NamedNBT{
			named("id", tagString(entry.Type)),
			named("x", tagInt(chunkX*16+int32(entry.LocalX))),
			named("y", tagInt(entry.BlockY)),
			named("z", tagInt(chunkZ*16+int32(entry.LocalZ))),
		}
		for _, field := range entry.NBT.Compound {
			switch field.Name {
			case "id", "x", "y", "z":
				continue
			}
			fields = append(fields, field)
		}
		out = append(out, tagCompound(fields...))
	}
	return out, nil
}

// packIndices writes palette indices into the Anvil long array layout used since
// Minecraft 1.16: a fixed bit width per entry, entries never split across two
// longs, and no data array at all for a single-entry palette.
func packIndices(indices []uint16, paletteSize, minimumBits int) []int64 {
	if paletteSize <= 1 {
		return nil
	}
	bits := minimumBits
	for (1 << bits) < paletteSize {
		bits++
	}
	perLong := 64 / bits
	longCount := (len(indices) + perLong - 1) / perLong
	packed := make([]int64, longCount)
	for position, index := range indices {
		slot := position / perLong
		offset := uint((position % perLong) * bits)
		packed[slot] |= int64(uint64(index) << offset)
	}
	return packed
}

func named(name string, value mcjava.NBTValue) mcjava.NamedNBT {
	return mcjava.NamedNBT{Name: name, Value: value}
}

func tagCompound(entries ...mcjava.NamedNBT) mcjava.NBTValue {
	return mcjava.NBTValue{Type: mcjava.TagCompound, Compound: entries}
}

func tagList(elementType mcjava.TagType, values ...mcjava.NBTValue) mcjava.NBTValue {
	if len(values) == 0 {
		return mcjava.NBTValue{Type: mcjava.TagList, List: &mcjava.NBTList{ElementType: mcjava.TagEnd}}
	}
	return mcjava.NBTValue{Type: mcjava.TagList, List: &mcjava.NBTList{ElementType: elementType, Values: values}}
}

func tagString(value string) mcjava.NBTValue {
	return mcjava.NBTValue{Type: mcjava.TagString, String: value}
}

func tagInt(value int32) mcjava.NBTValue {
	return mcjava.NBTValue{Type: mcjava.TagInt, Int: value}
}

func tagLong(value int64) mcjava.NBTValue {
	return mcjava.NBTValue{Type: mcjava.TagLong, Long: value}
}

func tagByte(value int8) mcjava.NBTValue {
	return mcjava.NBTValue{Type: mcjava.TagByte, Byte: value}
}

func tagLongArray(values []int64) mcjava.NBTValue {
	return mcjava.NBTValue{Type: mcjava.TagLongArray, LongArray: values}
}
