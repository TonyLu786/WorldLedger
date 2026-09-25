package main

import (
	"errors"
	"fmt"

	"github.com/worldledger/worldledger-mc/internal/anvil"
	"github.com/worldledger/worldledger-mc/internal/mcprofile"
	"github.com/worldledger/worldledger-mc/internal/translate"
)

type translationOptions struct {
	profilePath       string
	rulesPath         string
	policyName        string
	filler            string
	fillerBiome       string
	keepBlockEntities bool
}

// A translation rewrites chunks for another Minecraft release.
//
// It is a value rather than a function because a conversion is now carried out
// one region at a time, and the parts of it that are about the whole dimension
// -- what was lost, and whether the result may be written at all -- have to
// live across those regions. The translator inside it already accumulates its
// report over every chunk it is given, so nothing here adds up anything the
// translator is not already adding up.
//
// This runs only for the convert command. An export never reaches here, so a
// faithful export cannot silently become an approximation.
type translation struct {
	profile   mcprofile.Profile
	dimension mcprofile.Dimension
	policy    translate.Policy
	rules     translate.Rules
	options   translationOptions

	translator *translate.Translator
	// Block entity payloads are the network representation of the release that
	// was captured. Nothing here migrates them, and a payload an older release
	// cannot parse is a chunk it may refuse to load, so they are dropped unless
	// the operator asks for them. They are counted out here rather than inside
	// the translator, which only sees blocks, biomes and the build range.
	droppedBlockEntities int
}

func newTranslation(dimensionID string, options translationOptions) (*translation, error) {
	profile, err := mcprofile.Load(options.profilePath)
	if err != nil {
		return nil, err
	}
	dimension, exists := profile.Dimension(dimensionID)
	if !exists {
		return nil, fmt.Errorf("release %s has no dimension %s", profile.Version, dimensionID)
	}
	policy, err := translate.ParsePolicy(options.policyName)
	if err != nil {
		return nil, err
	}

	rules := translate.Rules{Schema: translate.RulesSchema}
	if options.rulesPath != "" {
		rules, err = translate.LoadRules(options.rulesPath)
		if err != nil {
			return nil, err
		}
	}

	t := &translation{
		profile:   profile,
		dimension: dimension,
		policy:    policy,
		rules:     rules,
		options:   options,
	}
	if err := t.restart(); err != nil {
		return nil, err
	}
	return t, nil
}

// restart gives the translation a fresh translator and forgets what was
// counted.
//
// The deciding pass and the writing pass translate the same chunks, so one
// translator across both would report every loss twice and say the dimension
// holds twice the chunks it holds. The report that is printed is the deciding
// pass's, which is the complete one; the writing pass's is discarded.
func (t *translation) restart() error {
	translator, err := translate.New(t.profile, t.rules, t.policy, t.options.filler, t.options.fillerBiome)
	if err != nil {
		return err
	}
	t.translator = translator
	t.droppedBlockEntities = 0
	return nil
}

// canRefuse answers whether this policy may decide, after seeing everything,
// that nothing should be written.
//
// Only the report policy does. skip-chunk and fill decide each chunk on its own
// and never revisit one, so a conversion under either can be written as it goes.
func (t *translation) canRefuse() bool {
	return t.policy == translate.PolicyReport
}

// chunks translates one region's worth. It matches what ExportByRegion asks of
// a transform, and is also what the deciding pass runs with the result thrown
// away.
func (t *translation) chunks(prepared []anvil.PreparedChunk) ([]anvil.PreparedChunk, error) {
	translated := make([]anvil.PreparedChunk, 0, len(prepared))
	for _, entry := range prepared {
		out, keep, err := t.translator.Chunk(translate.Chunk{
			Shape:  entry.Components.Shape,
			Blocks: entry.Components.Blocks,
			Biomes: entry.Components.Biomes,
		}, t.dimension)
		if err != nil {
			return nil, fmt.Errorf("chunk (%d,%d): %w", entry.Chunk.X, entry.Chunk.Z, err)
		}
		if !keep {
			continue
		}
		entry.Components.Shape = out.Shape
		entry.Components.Blocks = out.Blocks
		entry.Components.Biomes = out.Biomes
		if !t.options.keepBlockEntities && entry.Components.HasBlockEntities {
			t.droppedBlockEntities += len(entry.Components.BlockEntities)
			entry.Components.BlockEntities = nil
			entry.Components.HasBlockEntities = false
		}
		translated = append(translated, entry)
	}
	return translated, nil
}

// verdict is whether what was seen may be written, and is meaningful only after
// every chunk has been through chunks.
func (t *translation) verdict() error {
	if t.translator.Refused() {
		return errors.New("the target release cannot represent some observed state; nothing was written (choose --on-unrepresentable skip-chunk or fill, or supply --rules)")
	}
	// A dropped block entity is a loss, and `report` means do not write.
	//
	// Refusal was decided inside the translator, which only sees blocks, biomes
	// and the build range. Block entities are dropped out here, so a conversion
	// whose only loss was every chest, sign and furnace in the world reported
	// them and then wrote the world anyway, under the one policy whose entire
	// purpose is to write nothing and tell you what would have gone.
	if t.policy == translate.PolicyReport && t.droppedBlockEntities > 0 {
		return fmt.Errorf(
			"%d block entit(ies) would be dropped and nothing was written; "+
				"pass --keep-block-entities to carry them across unchanged, or choose "+
				"--on-unrepresentable skip-chunk or fill",
			t.droppedBlockEntities)
	}
	return nil
}

// printHeader says what is about to happen. It is printed before anything is
// written, under every policy.
//
// It is separate from the outcome below because the two are known at different
// times. What a conversion targets and how it is configured comes from the
// arguments; what it cost is known only once every region has been through,
// which under the streaming policies is after the world has been written.
func (t *translation) printHeader() {
	fmt.Printf("translating to %s (data version %d) under policy %s\n\n",
		t.profile.Version, t.profile.DataVersion, t.policy)
}

// printOutcome is what the conversion cost and what it produced, in that order.
// It reads the same whether it comes before the write, because the policy may
// still refuse, or after it.
func (t *translation) printOutcome() {
	printTranslationLosses(t.translator.Report(), t.droppedBlockEntities, t.options.keepBlockEntities)
	fmt.Printf("converted world targets Minecraft %s (data version %d)\n\n",
		t.profile.Version, t.profile.DataVersion)
}

func printTranslationLosses(report translate.Report, droppedBlockEntities int, keptBlockEntities bool) {
	if !report.Lossy() && droppedBlockEntities == 0 {
		fmt.Printf("%d chunk(s) translated with no loss\n", report.Chunks)
		return
	}

	fmt.Printf("%d chunk(s) examined\n", report.Chunks)
	if report.SkippedChunks > 0 {
		fmt.Printf("  %d chunk(s) skipped entirely\n", report.SkippedChunks)
	}
	if report.DroppedSection > 0 {
		fmt.Printf("  %d section(s) dropped: outside the target build range\n", report.DroppedSection)
	}
	if droppedBlockEntities > 0 {
		fmt.Printf("  %d block entit(ies) dropped: payloads are not migrated across releases (--keep-block-entities to carry them anyway)\n", droppedBlockEntities)
	}
	if keptBlockEntities {
		fmt.Println("  block entity payloads were carried across unchanged and may not be readable by the target release")
	}

	printChanges("blocks", report.Blocks)
	printChanges("biomes", report.Biomes)
	fmt.Println()
}

func printChanges(what string, changes []translate.Change) {
	if len(changes) == 0 {
		return
	}
	fmt.Printf("  %s:\n", what)
	for _, change := range changes {
		switch change.Outcome {
		case translate.OutcomeUnrepresentable:
			fmt.Printf("    %-44s UNREPRESENTABLE  %d position(s)\n", change.Source, change.Positions)
		case translate.OutcomeRenamed:
			fmt.Printf("    %-44s renamed to %s  %d position(s)\n", change.Source, change.Target, change.Positions)
		case translate.OutcomeSubstituted:
			fmt.Printf("    %-44s substituted with %s  %d position(s)\n", change.Source, change.Target, change.Positions)
		case translate.OutcomeFilled:
			fmt.Printf("    %-44s filled with %s  %d position(s)\n", change.Source, change.Target, change.Positions)
		}
	}
}
