package translate

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/mcprofile"
)

func target(t *testing.T) mcprofile.Profile {
	t.Helper()
	profile, err := mcprofile.Load(filepath.Join("..", "..", "profiles", "minecraft-java-26.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func overworld(t *testing.T) mcprofile.Dimension {
	t.Helper()
	dimension, exists := target(t).Dimension("minecraft:overworld")
	if !exists {
		t.Fatal("profile has no overworld")
	}
	return dimension
}

func blockSection(t *testing.T, sectionY int32, names ...string) mcjava.BlockSection {
	t.Helper()
	states := make([]mcjava.BlockState, mcjava.BlockCount)
	for position := range states {
		state, err := mcjava.ParseBlockState(names[position%len(names)])
		if err != nil {
			t.Fatal(err)
		}
		states[position] = state
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

// assertCanonical proves the rebuilt palette still satisfies the canonical
// rules: sorted, duplicate free, and with every entry referenced.
func assertCanonical(t *testing.T, section mcjava.BlockSection) {
	t.Helper()
	states, err := section.ParsedStates()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := mcjava.EncodeBlockSection(section.SectionY, states)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := mcjava.DecodeBlockSection(encoded)
	if err != nil {
		t.Fatalf("translated section is not canonical: %v", err)
	}
	if !reflect.DeepEqual(decoded.Palette, section.Palette) || !reflect.DeepEqual(decoded.Indices, section.Indices) {
		t.Fatal("translated section is not in canonical form")
	}
}

func newTranslator(t *testing.T, rules Rules, policy Policy) *Translator {
	t.Helper()
	translator, err := New(target(t), rules, policy, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return translator
}

func chunkWith(t *testing.T, sections ...mcjava.BlockSection) Chunk {
	t.Helper()
	blocks := map[int32]mcjava.BlockSection{}
	for _, section := range sections {
		blocks[section.SectionY] = section
	}
	return Chunk{
		Shape:  mcjava.Shape{MinSectionY: -4, SectionCount: 24},
		Blocks: blocks,
		Biomes: map[int32]mcjava.BiomeSection{},
	}
}

func TestBlocksTheTargetAlreadyHasAreUntouched(t *testing.T) {
	translator := newTranslator(t, Rules{Schema: RulesSchema}, PolicyFill)
	section := blockSection(t, 0, "minecraft:stone", "minecraft:oak_log[axis=x]")

	out, keep, err := translator.Chunk(chunkWith(t, section), overworld(t))
	if err != nil || !keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	if !reflect.DeepEqual(out.Blocks[0].Palette, section.Palette) {
		t.Fatalf("palette changed: %#v", out.Blocks[0].Palette)
	}
	report := translator.Report()
	if len(report.Blocks) != 0 || report.Lossy() {
		t.Fatalf("an untouched chunk was reported as lossy: %#v", report)
	}
}

func TestRenamePreservesProperties(t *testing.T) {
	rules := Rules{Schema: RulesSchema, Renames: map[string]string{"legacy:grass": "minecraft:short_grass"}}
	translator := newTranslator(t, rules, PolicyFill)
	section := blockSection(t, 0, "legacy:grass[snowy=false]")

	out, keep, err := translator.Chunk(chunkWith(t, section), overworld(t))
	if err != nil || !keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	if got := out.Blocks[0].Palette; len(got) != 1 || got[0] != "minecraft:short_grass[snowy=false]" {
		t.Fatalf("renamed palette = %#v", got)
	}
	report := translator.Report()
	if len(report.Blocks) != 1 || report.Blocks[0].Outcome != OutcomeRenamed {
		t.Fatalf("report = %#v", report.Blocks)
	}
	// A rename is the same block under a new identifier, so it is not lossy.
	if report.Lossy() {
		t.Fatal("a pure rename was reported as lossy")
	}
}

func TestSubstitutionDropsPropertiesUnlessAsked(t *testing.T) {
	rules := Rules{Schema: RulesSchema, Substitutions: map[string]Substitution{
		"examplemod:marble": {Block: "minecraft:stone"},
	}}
	translator := newTranslator(t, rules, PolicyFill)

	out, _, err := translator.Chunk(chunkWith(t, blockSection(t, 0, "examplemod:marble[axis=y]")), overworld(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Blocks[0].Palette; len(got) != 1 || got[0] != "minecraft:stone" {
		t.Fatalf("substituted palette = %#v; properties must not be carried onto a different block", got)
	}

	keeping := Rules{Schema: RulesSchema, Substitutions: map[string]Substitution{
		"examplemod:marble_log": {Block: "minecraft:oak_log", KeepProperties: true},
	}}
	translator = newTranslator(t, keeping, PolicyFill)
	out, _, err = translator.Chunk(chunkWith(t, blockSection(t, 0, "examplemod:marble_log[axis=z]")), overworld(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Blocks[0].Palette; len(got) != 1 || got[0] != "minecraft:oak_log[axis=z]" {
		t.Fatalf("substituted palette = %#v; properties were requested", got)
	}
	if report := translator.Report(); !report.Lossy() || report.Blocks[0].Outcome != OutcomeSubstituted {
		t.Fatalf("a substitution must be reported as lossy: %#v", report)
	}
}

func TestFillPolicyReplacesUnknownBlocksAndCountsPositions(t *testing.T) {
	translator := newTranslator(t, Rules{Schema: RulesSchema}, PolicyFill)
	section := blockSection(t, 0, "minecraft:stone", "examplemod:reactor")

	out, keep, err := translator.Chunk(chunkWith(t, section), overworld(t))
	if err != nil || !keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	assertCanonical(t, out.Blocks[0])

	report := translator.Report()
	if len(report.Blocks) != 1 {
		t.Fatalf("report = %#v", report.Blocks)
	}
	change := report.Blocks[0]
	if change.Source != "examplemod:reactor" || change.Outcome != OutcomeFilled || change.Target != DefaultFiller {
		t.Fatalf("change = %#v", change)
	}
	// Half of the 4096 positions in the section held the mod block.
	if change.Positions != mcjava.BlockCount/2 {
		t.Fatalf("positions = %d; want %d", change.Positions, mcjava.BlockCount/2)
	}
}

func TestSkipChunkPolicyDropsTheWholeChunk(t *testing.T) {
	translator := newTranslator(t, Rules{Schema: RulesSchema}, PolicySkipChunk)

	_, keep, err := translator.Chunk(chunkWith(t, blockSection(t, 0, "examplemod:reactor")), overworld(t))
	if err != nil {
		t.Fatal(err)
	}
	if keep {
		t.Fatal("a chunk containing an unrepresentable block was kept")
	}
	if report := translator.Report(); report.SkippedChunks != 1 || !report.Lossy() {
		t.Fatalf("report = %#v", report)
	}
}

func TestReportPolicyRefusesWithoutWriting(t *testing.T) {
	translator := newTranslator(t, Rules{Schema: RulesSchema}, PolicyReport)

	if _, _, err := translator.Chunk(chunkWith(t, blockSection(t, 0, "examplemod:reactor")), overworld(t)); err != nil {
		t.Fatal(err)
	}
	if !translator.Refused() {
		t.Fatal("the report policy did not refuse an unrepresentable block")
	}
	report := translator.Report()
	if len(report.Blocks) != 1 || report.Blocks[0].Outcome != OutcomeUnrepresentable {
		t.Fatalf("report = %#v", report.Blocks)
	}
}

func TestDistinctSourceBlocksCollapseOntoOneCanonicalPalette(t *testing.T) {
	rules := Rules{Schema: RulesSchema, Substitutions: map[string]Substitution{
		"examplemod:marble": {Block: "minecraft:stone"},
		"examplemod:slate":  {Block: "minecraft:stone"},
	}}
	translator := newTranslator(t, rules, PolicyFill)
	section := blockSection(t, 0, "examplemod:marble", "examplemod:slate")

	out, keep, err := translator.Chunk(chunkWith(t, section), overworld(t))
	if err != nil || !keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	translated := out.Blocks[0]
	if len(translated.Palette) != 1 || translated.Palette[0] != "minecraft:stone" {
		t.Fatalf("collapsed palette = %#v", translated.Palette)
	}
	for position, index := range translated.Indices {
		if index != 0 {
			t.Fatalf("index %d = %d; a collapsed palette has one entry", position, index)
		}
	}
	assertCanonical(t, translated)
}

func TestSectionsOutsideTheTargetBuildRangeAreDropped(t *testing.T) {
	profile := target(t)
	nether, exists := profile.Dimension("minecraft:the_nether")
	if !exists {
		t.Fatal("profile has no nether")
	}
	translator := newTranslator(t, Rules{Schema: RulesSchema}, PolicyFill)

	// Section -4 exists in an overworld observation but has nowhere to go in a
	// dimension whose world starts at Y=0.
	in := chunkWith(t, blockSection(t, -4, "minecraft:stone"), blockSection(t, 3, "minecraft:stone"))
	out, keep, err := translator.Chunk(in, nether)
	if err != nil || !keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	if _, present := out.Blocks[-4]; present {
		t.Fatal("a section below the target build range was written")
	}
	if _, present := out.Blocks[3]; !present {
		t.Fatal("a section inside the target build range was dropped")
	}
	if out.Shape.MinSectionY != 0 || out.Shape.SectionCount != 16 {
		t.Fatalf("output shape = %#v; want the target dimension range", out.Shape)
	}
	if report := translator.Report(); report.DroppedSection != 1 || !report.Lossy() {
		t.Fatalf("report = %#v", report)
	}
}

func TestChunkIsSkippedWhenEverySectionFallsOutsideTheTarget(t *testing.T) {
	nether, _ := target(t).Dimension("minecraft:the_nether")
	translator := newTranslator(t, Rules{Schema: RulesSchema}, PolicyFill)

	_, keep, err := translator.Chunk(chunkWith(t, blockSection(t, -4, "minecraft:stone")), nether)
	if err != nil {
		t.Fatal(err)
	}
	if keep {
		t.Fatal("a chunk with no representable section was kept")
	}
}

func TestBiomesAreTranslatedAndFilled(t *testing.T) {
	rules := Rules{Schema: RulesSchema, BiomeSubstitutions: map[string]string{
		"examplemod:crystal_fields": "minecraft:plains",
	}}
	translator := newTranslator(t, rules, PolicyFill)
	in := chunkWith(t, blockSection(t, 0, "minecraft:stone"))
	in.Biomes[0] = biomeSection(t, 0, "examplemod:crystal_fields", "examplemod:void_reach")

	out, keep, err := translator.Chunk(in, overworld(t))
	if err != nil || !keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	// A canonical palette is sorted, so plains precedes the_void.
	want := []string{"minecraft:plains", DefaultBiomeFiller}
	if got := out.Biomes[0].Palette; !reflect.DeepEqual(got, want) {
		t.Fatalf("biome palette = %#v; want %#v", got, want)
	}
	outcomes := map[string]Outcome{}
	for _, change := range translator.Report().Biomes {
		outcomes[change.Source] = change.Outcome
	}
	if outcomes["examplemod:crystal_fields"] != OutcomeSubstituted || outcomes["examplemod:void_reach"] != OutcomeFilled {
		t.Fatalf("biome outcomes = %#v", outcomes)
	}
}

func TestRulesValidationRejectsImpossibleMappings(t *testing.T) {
	profile := target(t)

	tests := map[string]Rules{
		"rename to a block the target lacks": {
			Renames: map[string]string{"legacy:x": "minecraft:not_a_block"},
		},
		"substitute to a block the target lacks": {
			Substitutions: map[string]Substitution{"legacy:x": {Block: "minecraft:not_a_block"}},
		},
		"biome rename to a biome the target lacks": {
			BiomeRenames: map[string]string{"legacy:x": "minecraft:not_a_biome"},
		},
		"source is both renamed and substituted": {
			Renames:       map[string]string{"legacy:x": "minecraft:stone"},
			Substitutions: map[string]Substitution{"legacy:x": {Block: "minecraft:dirt"}},
		},
		"source is not a resource location": {
			Renames: map[string]string{"bare_name": "minecraft:stone"},
		},
	}
	for name, rules := range tests {
		if err := rules.Validate(profile); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestFillerMustExistInTheTarget(t *testing.T) {
	if _, err := New(target(t), Rules{Schema: RulesSchema}, PolicyFill, "minecraft:not_a_block", ""); err == nil {
		t.Fatal("a filler block outside the target release was accepted")
	}
	if _, err := New(target(t), Rules{Schema: RulesSchema}, PolicyFill, "", "minecraft:not_a_biome"); err == nil {
		t.Fatal("a filler biome outside the target release was accepted")
	}
}

// Only the fill policy writes the filler. Requiring a usable one under the other
// policies would reject an export whose rules already cover every state.
func TestPoliciesThatNeverFillDoNotRequireAUsableFiller(t *testing.T) {
	for _, policy := range []Policy{PolicyReport, PolicySkipChunk} {
		if _, err := New(target(t), Rules{Schema: RulesSchema}, policy, "minecraft:not_a_block", "minecraft:not_a_biome"); err != nil {
			t.Fatalf("policy %s rejected an unused filler: %v", policy, err)
		}
	}
}

// Rules that cover every unrepresentable state leave nothing to refuse.
func TestReportPolicyAcceptsAChunkFullyCoveredByRules(t *testing.T) {
	rules := Rules{Schema: RulesSchema, Substitutions: map[string]Substitution{
		"examplemod:reactor": {Block: "minecraft:stone"},
	}}
	translator := newTranslator(t, rules, PolicyReport)

	_, keep, err := translator.Chunk(chunkWith(t, blockSection(t, 0, "examplemod:reactor")), overworld(t))
	if err != nil || !keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	if translator.Refused() {
		t.Fatal("the report policy refused a chunk that the rules fully covered")
	}
}

func TestParsePolicy(t *testing.T) {
	for _, name := range []string{"report", "skip-chunk", "fill"} {
		if _, err := ParsePolicy(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := ParsePolicy("guess"); err == nil {
		t.Fatal("an unknown policy was accepted")
	}
}
