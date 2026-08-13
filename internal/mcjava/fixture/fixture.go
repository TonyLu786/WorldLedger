// Package fixture loads the machine-readable canonical fixture descriptions.
package fixture

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
)

const Schema = "worldledger.minecraft.java.chunk-fixtures/v1"

type Set struct {
	Schema   string    `json:"schema"`
	Fixtures []Fixture `json:"fixtures"`
}

type Fixture struct {
	Name          string             `json:"name"`
	Component     string             `json:"component"`
	Output        string             `json:"output"`
	Shape         *ShapeSpec         `json:"shape,omitempty"`
	BlockSection  *BlockSectionSpec  `json:"block_section,omitempty"`
	BiomeSection  *BiomeSectionSpec  `json:"biome_section,omitempty"`
	BlockEntities *BlockEntitiesSpec `json:"block_entities,omitempty"`
}

type ShapeSpec struct {
	MinSectionY  int32  `json:"min_section_y"`
	SectionCount uint32 `json:"section_count"`
}

type BlockSectionSpec struct {
	SectionY int32    `json:"section_y"`
	States   StateSet `json:"states"`
}

type StateSet struct {
	Kind       string             `json:"kind"`
	State      *mcjava.BlockState `json:"state,omitempty"`
	Namespace  string             `json:"namespace,omitempty"`
	PathPrefix string             `json:"path_prefix,omitempty"`
	Count      int                `json:"count,omitempty"`
	Width      int                `json:"width,omitempty"`
}

type BiomeSectionSpec struct {
	SectionY int32       `json:"section_y"`
	Biomes   ResourceSet `json:"biomes"`
}

type ResourceSet struct {
	Kind   string   `json:"kind"`
	Values []string `json:"values"`
}

type BlockEntitiesSpec struct {
	Entries []BlockEntitySpec `json:"entries"`
}

type BlockEntitySpec struct {
	LocalX int     `json:"local_x"`
	BlockY int32   `json:"block_y"`
	LocalZ int     `json:"local_z"`
	Type   string  `json:"type"`
	NBT    NBTSpec `json:"nbt"`
}

type NBTSpec struct {
	Type        string         `json:"type"`
	Byte        *int8          `json:"byte,omitempty"`
	Short       *int16         `json:"short,omitempty"`
	Int         *int32         `json:"int,omitempty"`
	Long        string         `json:"long,omitempty"`
	FloatBits   string         `json:"float_bits,omitempty"`
	DoubleBits  string         `json:"double_bits,omitempty"`
	BytesHex    string         `json:"bytes_hex,omitempty"`
	String      *string        `json:"string,omitempty"`
	ElementType string         `json:"element_type,omitempty"`
	Values      []NBTSpec      `json:"values,omitempty"`
	Entries     []NamedNBTSpec `json:"entries,omitempty"`
	Ints        []int32        `json:"ints,omitempty"`
	Longs       []string       `json:"longs,omitempty"`
}

type NamedNBTSpec struct {
	Name  string  `json:"name"`
	Value NBTSpec `json:"value"`
}

func Load(path string) (Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Set{}, err
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Set{}, fmt.Errorf("decode fixture descriptions: %w", err)
	}
	var set Set
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&set); err != nil {
		return Set{}, fmt.Errorf("decode fixture descriptions: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Set{}, err
	}
	if set.Schema != Schema {
		return Set{}, fmt.Errorf("unsupported fixture schema %q", set.Schema)
	}
	if len(set.Fixtures) == 0 {
		return Set{}, errors.New("fixture set is empty")
	}
	seenNames := make(map[string]struct{}, len(set.Fixtures))
	seenOutputs := make(map[string]struct{}, len(set.Fixtures))
	for index, item := range set.Fixtures {
		if item.Name == "" {
			return Set{}, fmt.Errorf("fixture %d has no name", index)
		}
		if _, exists := seenNames[item.Name]; exists {
			return Set{}, fmt.Errorf("duplicate fixture name %q", item.Name)
		}
		seenNames[item.Name] = struct{}{}
		if item.Output == "" || strings.ContainsAny(item.Output, "/\\") || !strings.HasSuffix(item.Output, ".bin") {
			return Set{}, fmt.Errorf("fixture %q has invalid output %q", item.Name, item.Output)
		}
		if _, exists := seenOutputs[item.Output]; exists {
			return Set{}, fmt.Errorf("duplicate fixture output %q", item.Output)
		}
		seenOutputs[item.Output] = struct{}{}
	}
	return set, nil
}

func Build(item Fixture) ([]byte, error) {
	switch item.Component {
	case "shape":
		if item.Shape == nil || item.BlockSection != nil || item.BiomeSection != nil || item.BlockEntities != nil {
			return nil, errors.New("shape fixture must contain only shape input")
		}
		return mcjava.EncodeShape(item.Shape.MinSectionY, item.Shape.SectionCount)
	case "block_section":
		if item.BlockSection == nil || item.Shape != nil || item.BiomeSection != nil || item.BlockEntities != nil {
			return nil, errors.New("block section fixture must contain only block_section input")
		}
		states, err := buildStates(item.BlockSection.States)
		if err != nil {
			return nil, err
		}
		return mcjava.EncodeBlockSection(item.BlockSection.SectionY, states)
	case "biome_section":
		if item.BiomeSection == nil || item.Shape != nil || item.BlockSection != nil || item.BlockEntities != nil {
			return nil, errors.New("biome section fixture must contain only biome_section input")
		}
		biomes, err := buildResources(item.BiomeSection.Biomes, mcjava.BiomeCount)
		if err != nil {
			return nil, err
		}
		return mcjava.EncodeBiomeSection(item.BiomeSection.SectionY, biomes)
	case "block_entities":
		if item.BlockEntities == nil || item.Shape != nil || item.BlockSection != nil || item.BiomeSection != nil {
			return nil, errors.New("block entity fixture must contain only block_entities input")
		}
		entries := make([]mcjava.BlockEntity, len(item.BlockEntities.Entries))
		for index, entry := range item.BlockEntities.Entries {
			nbt, err := buildNBT(entry.NBT, 0)
			if err != nil {
				return nil, fmt.Errorf("block entity %d: %w", index, err)
			}
			entries[index] = mcjava.BlockEntity{
				LocalX: entry.LocalX,
				BlockY: entry.BlockY,
				LocalZ: entry.LocalZ,
				Type:   entry.Type,
				NBT:    nbt,
			}
		}
		return mcjava.EncodeBlockEntities(entries)
	default:
		return nil, fmt.Errorf("unsupported fixture component %q", item.Component)
	}
}

func buildStates(spec StateSet) ([]mcjava.BlockState, error) {
	states := make([]mcjava.BlockState, mcjava.BlockCount)
	switch spec.Kind {
	case "constant":
		if spec.State == nil || spec.Namespace != "" || spec.PathPrefix != "" || spec.Count != 0 || spec.Width != 0 {
			return nil, errors.New("constant state set requires only state")
		}
		for index := range states {
			states[index] = cloneState(*spec.State)
		}
	case "indexed_resources":
		if spec.State != nil || spec.Namespace == "" || spec.PathPrefix == "" || spec.Count != mcjava.BlockCount || spec.Width < 1 || spec.Width > 9 {
			return nil, errors.New("invalid indexed_resources state set")
		}
		for index := range states {
			states[index] = mcjava.BlockState{Name: fmt.Sprintf("%s:%s%0*d", spec.Namespace, spec.PathPrefix, spec.Width, index)}
		}
	default:
		return nil, fmt.Errorf("unsupported state set kind %q", spec.Kind)
	}
	return states, nil
}

func cloneState(state mcjava.BlockState) mcjava.BlockState {
	state.Properties = append([]mcjava.Property(nil), state.Properties...)
	return state
}

func buildResources(spec ResourceSet, count int) ([]string, error) {
	if spec.Kind != "cycle" || len(spec.Values) == 0 {
		return nil, errors.New("resource set must be a non-empty cycle")
	}
	values := make([]string, count)
	for index := range values {
		values[index] = spec.Values[index%len(spec.Values)]
	}
	return values, nil
}

func buildNBT(spec NBTSpec, depth int) (mcjava.NBTValue, error) {
	if depth > 64 {
		return mcjava.NBTValue{}, errors.New("fixture NBT depth exceeds 64")
	}
	tag, err := parseTag(spec.Type)
	if err != nil {
		return mcjava.NBTValue{}, err
	}
	value := mcjava.NBTValue{Type: tag}
	switch tag {
	case mcjava.TagByte:
		if spec.Byte == nil {
			return value, errors.New("byte value is required")
		}
		value.Byte = *spec.Byte
	case mcjava.TagShort:
		if spec.Short == nil {
			return value, errors.New("short value is required")
		}
		value.Short = *spec.Short
	case mcjava.TagInt:
		if spec.Int == nil {
			return value, errors.New("int value is required")
		}
		value.Int = *spec.Int
	case mcjava.TagLong:
		parsed, err := strconv.ParseInt(spec.Long, 10, 64)
		if err != nil {
			return value, fmt.Errorf("invalid long %q: %w", spec.Long, err)
		}
		value.Long = parsed
	case mcjava.TagFloat:
		parsed, err := parseBits(spec.FloatBits, 32)
		if err != nil {
			return value, fmt.Errorf("invalid float bits: %w", err)
		}
		value.FloatBits = uint32(parsed)
	case mcjava.TagDouble:
		parsed, err := parseBits(spec.DoubleBits, 64)
		if err != nil {
			return value, fmt.Errorf("invalid double bits: %w", err)
		}
		value.DoubleBits = parsed
	case mcjava.TagByteArray:
		parsed, err := hex.DecodeString(spec.BytesHex)
		if err != nil {
			return value, fmt.Errorf("invalid byte array hex: %w", err)
		}
		value.ByteArray = parsed
	case mcjava.TagString:
		if spec.String == nil {
			return value, errors.New("string value is required")
		}
		value.String = *spec.String
	case mcjava.TagList:
		elementType, err := parseTag(spec.ElementType)
		if err != nil {
			return value, fmt.Errorf("list element type: %w", err)
		}
		items := make([]mcjava.NBTValue, len(spec.Values))
		for index, itemSpec := range spec.Values {
			item, err := buildNBT(itemSpec, depth+1)
			if err != nil {
				return value, fmt.Errorf("list item %d: %w", index, err)
			}
			items[index] = item
		}
		value.List = &mcjava.NBTList{ElementType: elementType, Values: items}
	case mcjava.TagCompound:
		entries := make([]mcjava.NamedNBT, len(spec.Entries))
		for index, entrySpec := range spec.Entries {
			entryValue, err := buildNBT(entrySpec.Value, depth+1)
			if err != nil {
				return value, fmt.Errorf("compound entry %q: %w", entrySpec.Name, err)
			}
			entries[index] = mcjava.NamedNBT{Name: entrySpec.Name, Value: entryValue}
		}
		value.Compound = entries
	case mcjava.TagIntArray:
		value.IntArray = append([]int32(nil), spec.Ints...)
	case mcjava.TagLongArray:
		value.LongArray = make([]int64, len(spec.Longs))
		for index, raw := range spec.Longs {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return value, fmt.Errorf("invalid long array item %q: %w", raw, err)
			}
			value.LongArray[index] = parsed
		}
	default:
		return value, fmt.Errorf("unsupported fixture NBT type %q", spec.Type)
	}
	return value, nil
}

func parseTag(name string) (mcjava.TagType, error) {
	tags := map[string]mcjava.TagType{
		"end":         mcjava.TagEnd,
		"byte":        mcjava.TagByte,
		"short":       mcjava.TagShort,
		"int":         mcjava.TagInt,
		"long":        mcjava.TagLong,
		"float_bits":  mcjava.TagFloat,
		"double_bits": mcjava.TagDouble,
		"byte_array":  mcjava.TagByteArray,
		"string":      mcjava.TagString,
		"list":        mcjava.TagList,
		"compound":    mcjava.TagCompound,
		"int_array":   mcjava.TagIntArray,
		"long_array":  mcjava.TagLongArray,
	}
	if tag, exists := tags[name]; exists {
		return tag, nil
	}
	return mcjava.TagEnd, fmt.Errorf("unknown NBT type %q", name)
}

func parseBits(value string, bits int) (uint64, error) {
	if !strings.HasPrefix(value, "0x") {
		return 0, errors.New("bit pattern must have 0x prefix")
	}
	wantDigits := bits / 4
	if len(value) != wantDigits+2 {
		return 0, fmt.Errorf("bit pattern must contain %d hex digits", wantDigits)
	}
	return strconv.ParseUint(value[2:], 16, bits)
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("fixture file contains trailing JSON")
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder, "$"); err != nil {
		return err
	}
	if _, err := decoder.Token(); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("fixture file contains trailing JSON")
}

func scanJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key at %s is not a string", path)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON key %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("object at %s is not terminated", path)
		}
		return nil
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("array at %s is not terminated", path)
		}
		return nil
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, path)
	}
}
