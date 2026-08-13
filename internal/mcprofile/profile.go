// Package mcprofile describes what a concrete Minecraft release is able to
// represent: its data version, the build range of each dimension, and its block
// and biome registries.
//
// A profile is extracted from a real game artifact, never written by hand. It
// exists so that exporting to a release other than the one an observation came
// from can say precisely what does not fit, instead of substituting a plausible
// value.
//
// Scope of a v1 profile: block identifiers, biome identifiers, dimension build
// ranges, and the data version. Property-level validation is deliberately out of
// scope. A client jar describes block states only through
// assets/minecraft/blockstates, which enumerates the properties that change a
// rendered model and omits the rest — minecraft:oak_stairs lists facing, half,
// and shape but not waterlogged. Treating that as a full state definition would
// reject valid states, so this profile makes no claim about properties at all.
// Full definitions require the vanilla data generator's reports/blocks.json.
package mcprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/model"
)

const Schema = "worldledger.minecraft-profile/v1"

type Profile struct {
	Schema        string                  `json:"schema"`
	Version       string                  `json:"version"`
	DataVersion   int32                   `json:"data_version"`
	Dimensions    map[string]Dimension    `json:"dimensions"`
	Blocks        []string                `json:"blocks"`
	Biomes        []string                `json:"biomes"`
	StructureSets map[string]StructureSet `json:"structure_sets,omitempty"`
}

// StructureSet is a release's own structure placement configuration.
//
// It is recorded so an operator does not have to hand-enter placement numbers,
// which is error prone and silently produces wrong answers. It describes the
// release only: a datapack or server fork can change these, and then the values
// here no longer describe that world.
type StructureSet struct {
	// Type is the placement algorithm, for example minecraft:random_spread.
	Type       string `json:"type"`
	Spacing    int32  `json:"spacing,omitempty"`
	Separation int32  `json:"separation,omitempty"`
	Salt       int32  `json:"salt"`
	// SpreadType is empty when the release did not state one, which means linear.
	SpreadType string `json:"spread_type,omitempty"`
	// RandomSpread is false for algorithms this project does not model, such as
	// the concentric rings used by strongholds.
	RandomSpread bool `json:"random_spread"`
}

type Dimension struct {
	MinSectionY  int32  `json:"min_section_y"`
	SectionCount uint32 `json:"section_count"`
}

func (d Dimension) MaxSectionY() int32 {
	return d.MinSectionY + int32(d.SectionCount) - 1
}

func (d Dimension) Contains(sectionY int32) bool {
	return d.SectionCount > 0 && sectionY >= d.MinSectionY && sectionY <= d.MaxSectionY()
}

func Load(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(TrimBOM(data)))
	decoder.DisallowUnknownFields()
	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", path, err)
	}
	return profile, nil
}

// TrimBOM removes a leading UTF-8 byte order mark. Profiles and rules are
// hand-edited JSON and Windows editors add one routinely; a decoder that
// rejects it produces an error that says nothing about the real problem.
func TrimBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

func (p Profile) Save(path string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (p Profile) Validate() error {
	if p.Schema != Schema {
		return fmt.Errorf("unsupported profile schema %q", p.Schema)
	}
	if strings.TrimSpace(p.Version) == "" {
		return errors.New("profile version is required")
	}
	if p.DataVersion <= 0 {
		return fmt.Errorf("invalid data version %d", p.DataVersion)
	}
	if len(p.Dimensions) == 0 {
		return errors.New("profile declares no dimensions")
	}
	if len(p.Blocks) == 0 {
		return errors.New("profile declares no blocks")
	}
	if len(p.Biomes) == 0 {
		return errors.New("profile declares no biomes")
	}
	for id, dimension := range p.Dimensions {
		if dimension.SectionCount == 0 {
			return fmt.Errorf("dimension %q has no sections", id)
		}
	}
	if err := requireSorted(p.Blocks, "blocks"); err != nil {
		return err
	}
	return requireSorted(p.Biomes, "biomes")
}

func requireSorted(values []string, what string) error {
	for index := 1; index < len(values); index++ {
		if values[index] <= values[index-1] {
			return fmt.Errorf("%s must be sorted and unique", what)
		}
	}
	return nil
}

func (p Profile) HasBlock(name string) bool {
	return sortedContains(p.Blocks, name)
}

func (p Profile) HasBiome(name string) bool {
	return sortedContains(p.Biomes, name)
}

func sortedContains(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}

func (p Profile) Dimension(id string) (Dimension, bool) {
	dimension, exists := p.Dimensions[model.NormalizeToken(id)]
	return dimension, exists
}

// CheckBlockState reports why a canonical block state cannot be represented by
// this release, or nil when it can.
//
// Only the block identifier is checked. A profile carries no property
// definitions, so a state whose block exists is reported as representable even
// if one of its properties was introduced by a later release. Translation must
// not present this as a guarantee.
func (p Profile) CheckBlockState(state mcjava.BlockState) error {
	if !p.HasBlock(state.Name) {
		return fmt.Errorf("block %s does not exist in %s", state.Name, p.Version)
	}
	return nil
}
