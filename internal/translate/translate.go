package translate

import (
	"fmt"
	"sort"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/mcprofile"
)

// DefaultFiller leaves a hole rather than asserting a block that was never
// observed. Any block the target release has can be configured instead; a
// visible marker makes the loss obvious in game, at the cost of putting
// something in the world that nobody ever saw.
const DefaultFiller = "minecraft:air"

// DefaultBiomeFiller reads as "nothing here" instead of guessing a plausible
// biome such as plains.
const DefaultBiomeFiller = "minecraft:the_void"

// Chunk is the canonical state of one chunk, independent of any output format.
type Chunk struct {
	Shape  mcjava.Shape
	Blocks map[int32]mcjava.BlockSection
	Biomes map[int32]mcjava.BiomeSection
}

type Translator struct {
	target      mcprofile.Profile
	rules       Rules
	policy      Policy
	filler      string
	biomeFiller string

	blockCounts     map[string]*counter
	biomeCounts     map[string]*counter
	chunks          int
	skippedChunks   int
	droppedSections int
	refused         bool
}

func New(target mcprofile.Profile, rules Rules, policy Policy, filler, biomeFiller string) (*Translator, error) {
	if err := rules.Validate(target); err != nil {
		return nil, err
	}
	if filler == "" {
		filler = DefaultFiller
	}
	if biomeFiller == "" {
		biomeFiller = DefaultBiomeFiller
	}
	// Only the fill policy ever writes the filler. Validating it under the other
	// policies would reject an export whose rules already cover everything.
	if policy == PolicyFill {
		state, err := mcjava.ParseBlockState(filler)
		if err != nil {
			return nil, fmt.Errorf("filler block: %w", err)
		}
		if err := target.CheckBlockState(state); err != nil {
			return nil, fmt.Errorf("filler block: %w", err)
		}
		if !target.HasBiome(biomeFiller) {
			return nil, fmt.Errorf("filler biome %s does not exist in %s", biomeFiller, target.Version)
		}
	}
	return &Translator{
		target:      target,
		rules:       rules,
		policy:      policy,
		filler:      filler,
		biomeFiller: biomeFiller,
		blockCounts: map[string]*counter{},
		biomeCounts: map[string]*counter{},
	}, nil
}

// Refused reports whether the report policy found state the target release
// cannot represent. Nothing should be written when it is true.
func (t *Translator) Refused() bool {
	return t.refused
}

func (t *Translator) Report() Report {
	return Report{
		Target:         t.target.Version,
		Policy:         t.policy,
		Chunks:         t.chunks,
		SkippedChunks:  t.skippedChunks,
		DroppedSection: t.droppedSections,
		Blocks:         summarize(t.blockCounts),
		Biomes:         summarize(t.biomeCounts),
	}
}

// Chunk rewrites one chunk for the target release. The returned flag is false
// when the chunk must not be written at all.
//
// The output shape is the target dimension's own build range, and sections
// outside it are dropped: a section at Y=-4 has nowhere to go in a release whose
// world starts at Y=0.
func (t *Translator) Chunk(in Chunk, dimension mcprofile.Dimension) (Chunk, bool, error) {
	t.chunks++
	out := Chunk{
		Shape:  mcjava.Shape{MinSectionY: dimension.MinSectionY, SectionCount: dimension.SectionCount},
		Blocks: map[int32]mcjava.BlockSection{},
		Biomes: map[int32]mcjava.BiomeSection{},
	}

	for _, sectionY := range sortedSectionYs(in.Blocks) {
		if !dimension.Contains(sectionY) {
			t.droppedSections++
			if t.policy == PolicyReport {
				t.refused = true
			}
			continue
		}
		section, keep, err := t.blockSection(in.Blocks[sectionY])
		if err != nil {
			return Chunk{}, false, err
		}
		if !keep {
			t.skippedChunks++
			return Chunk{}, false, nil
		}
		out.Blocks[sectionY] = section
	}

	for _, sectionY := range sortedBiomeSectionYs(in.Biomes) {
		if !dimension.Contains(sectionY) {
			t.droppedSections++
			if t.policy == PolicyReport {
				t.refused = true
			}
			continue
		}
		section, keep, err := t.biomeSection(in.Biomes[sectionY])
		if err != nil {
			return Chunk{}, false, err
		}
		if !keep {
			t.skippedChunks++
			return Chunk{}, false, nil
		}
		out.Biomes[sectionY] = section
	}

	if len(out.Blocks) == 0 {
		t.skippedChunks++
		return Chunk{}, false, nil
	}
	return out, true, nil
}

func (t *Translator) blockSection(section mcjava.BlockSection) (mcjava.BlockSection, bool, error) {
	positions := positionCounts(section.Indices, len(section.Palette))
	replacements := make([]string, len(section.Palette))

	for index, entry := range section.Palette {
		state, err := mcjava.ParseBlockState(entry)
		if err != nil {
			return mcjava.BlockSection{}, false, err
		}
		replacement, outcome, err := t.resolveBlock(state, entry)
		if err != nil {
			return mcjava.BlockSection{}, false, err
		}
		if outcome == OutcomeUnrepresentable && t.policy == PolicySkipChunk {
			t.record(t.blockCounts, entry, outcome, "", positions[index])
			return mcjava.BlockSection{}, false, nil
		}
		t.record(t.blockCounts, entry, outcome, replacement, positions[index])
		replacements[index] = replacement
	}

	palette, indices := rebuild(replacements, section.Indices)
	return mcjava.BlockSection{SectionY: section.SectionY, Palette: palette, Indices: indices}, true, nil
}

func (t *Translator) biomeSection(section mcjava.BiomeSection) (mcjava.BiomeSection, bool, error) {
	positions := positionCounts(section.Indices, len(section.Palette))
	replacements := make([]string, len(section.Palette))

	for index, entry := range section.Palette {
		replacement, outcome := t.resolveBiome(entry)
		if outcome == OutcomeUnrepresentable && t.policy == PolicySkipChunk {
			t.record(t.biomeCounts, entry, outcome, "", positions[index])
			return mcjava.BiomeSection{}, false, nil
		}
		t.record(t.biomeCounts, entry, outcome, replacement, positions[index])
		replacements[index] = replacement
	}

	palette, indices := rebuild(replacements, section.Indices)
	return mcjava.BiomeSection{SectionY: section.SectionY, Palette: palette, Indices: indices}, true, nil
}

func (t *Translator) resolveBlock(state mcjava.BlockState, canonical string) (string, Outcome, error) {
	if t.target.HasBlock(state.Name) {
		return canonical, OutcomeIdentity, nil
	}
	if replacement, exists := t.rules.Renames[state.Name]; exists {
		renamed, err := canonicalOrError(mcjava.BlockState{Name: replacement, Properties: state.Properties})
		if err != nil {
			return "", "", err
		}
		return renamed, OutcomeRenamed, nil
	}
	if substitution, exists := t.rules.Substitutions[state.Name]; exists {
		replacement := mcjava.BlockState{Name: substitution.Block}
		if substitution.KeepProperties {
			replacement.Properties = state.Properties
		}
		substituted, err := canonicalOrError(replacement)
		if err != nil {
			return "", "", err
		}
		return substituted, OutcomeSubstituted, nil
	}
	if t.policy == PolicyReport {
		// Nothing is written under this policy, so the state is left alone; the
		// section stays canonical while the refusal is recorded.
		t.refused = true
		return canonical, OutcomeUnrepresentable, nil
	}
	if t.policy == PolicySkipChunk {
		return "", OutcomeUnrepresentable, nil
	}
	return t.filler, OutcomeFilled, nil
}

func (t *Translator) resolveBiome(biome string) (string, Outcome) {
	if t.target.HasBiome(biome) {
		return biome, OutcomeIdentity
	}
	if replacement, exists := t.rules.BiomeRenames[biome]; exists {
		return replacement, OutcomeRenamed
	}
	if replacement, exists := t.rules.BiomeSubstitutions[biome]; exists {
		return replacement, OutcomeSubstituted
	}
	if t.policy == PolicyReport {
		t.refused = true
		return biome, OutcomeUnrepresentable
	}
	if t.policy == PolicySkipChunk {
		return "", OutcomeUnrepresentable
	}
	return t.biomeFiller, OutcomeFilled
}

func (t *Translator) record(counts map[string]*counter, source string, outcome Outcome, target string, positions int) {
	entry, exists := counts[source]
	if !exists {
		entry = &counter{outcome: outcome, target: target}
		counts[source] = entry
	}
	entry.positions += positions
}

// rebuild produces a canonical palette from the replaced entries: sorted,
// duplicate free, and with every entry referenced. Two source states can
// collapse onto one replacement, so the palette is rebuilt rather than patched.
func rebuild(replacements []string, indices []uint16) ([]string, []uint16) {
	distinct := map[string]struct{}{}
	for _, replacement := range replacements {
		distinct[replacement] = struct{}{}
	}
	palette := make([]string, 0, len(distinct))
	for value := range distinct {
		palette = append(palette, value)
	}
	sort.Strings(palette)

	position := make(map[string]uint16, len(palette))
	for index, value := range palette {
		position[value] = uint16(index)
	}
	remapped := make([]uint16, len(indices))
	for offset, index := range indices {
		remapped[offset] = position[replacements[index]]
	}
	return palette, remapped
}

func positionCounts(indices []uint16, paletteSize int) []int {
	counts := make([]int, paletteSize)
	for _, index := range indices {
		counts[index]++
	}
	return counts
}

func sortedSectionYs(sections map[int32]mcjava.BlockSection) []int32 {
	out := make([]int32, 0, len(sections))
	for sectionY := range sections {
		out = append(out, sectionY)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedBiomeSectionYs(sections map[int32]mcjava.BiomeSection) []int32 {
	out := make([]int32, 0, len(sections))
	for sectionY := range sections {
		out = append(out, sectionY)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
