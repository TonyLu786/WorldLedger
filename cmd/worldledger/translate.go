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

// translateForTarget rewrites prepared chunks for another Minecraft release and
// prints what the conversion cost. It returns the chunks the target can carry
// and the data version to stamp them with.
//
// This runs only for the convert command. An export never reaches here, so a
// faithful export cannot silently become an approximation.
func translateForTarget(prepared []anvil.PreparedChunk, dimensionID string, options translationOptions) ([]anvil.PreparedChunk, int32, error) {
	profile, err := mcprofile.Load(options.profilePath)
	if err != nil {
		return nil, 0, err
	}
	dimension, exists := profile.Dimension(dimensionID)
	if !exists {
		return nil, 0, fmt.Errorf("release %s has no dimension %s", profile.Version, dimensionID)
	}
	policy, err := translate.ParsePolicy(options.policyName)
	if err != nil {
		return nil, 0, err
	}

	rules := translate.Rules{Schema: translate.RulesSchema}
	if options.rulesPath != "" {
		rules, err = translate.LoadRules(options.rulesPath)
		if err != nil {
			return nil, 0, err
		}
	}
	translator, err := translate.New(profile, rules, policy, options.filler, options.fillerBiome)
	if err != nil {
		return nil, 0, err
	}

	// Block entity payloads are the network representation of the release that
	// was captured. Nothing here migrates them, and a payload an older release
	// cannot parse is a chunk it may refuse to load, so they are dropped unless
	// the operator asks for them.
	droppedBlockEntities := 0

	translated := make([]anvil.PreparedChunk, 0, len(prepared))
	for _, entry := range prepared {
		out, keep, err := translator.Chunk(translate.Chunk{
			Shape:  entry.Components.Shape,
			Blocks: entry.Components.Blocks,
			Biomes: entry.Components.Biomes,
		}, dimension)
		if err != nil {
			return nil, 0, fmt.Errorf("chunk (%d,%d): %w", entry.Chunk.X, entry.Chunk.Z, err)
		}
		if !keep {
			continue
		}
		entry.Components.Shape = out.Shape
		entry.Components.Blocks = out.Blocks
		entry.Components.Biomes = out.Biomes
		if !options.keepBlockEntities && entry.Components.HasBlockEntities {
			if len(entry.Components.BlockEntities) > 0 {
				droppedBlockEntities += len(entry.Components.BlockEntities)
			}
			entry.Components.BlockEntities = nil
			entry.Components.HasBlockEntities = false
		}
		translated = append(translated, entry)
	}

	report := translator.Report()
	printTranslationReport(profile, report, droppedBlockEntities, options.keepBlockEntities)

	if translator.Refused() {
		return nil, 0, errors.New("the target release cannot represent some observed state; nothing was written (choose --on-unrepresentable skip-chunk or fill, or supply --rules)")
	}
	fmt.Printf("converted world targets Minecraft %s (data version %d)\n\n", profile.Version, profile.DataVersion)
	return translated, profile.DataVersion, nil
}

func printTranslationReport(profile mcprofile.Profile, report translate.Report, droppedBlockEntities int, keptBlockEntities bool) {
	fmt.Printf("translating to %s (data version %d) under policy %s\n", profile.Version, profile.DataVersion, report.Policy)

	if !report.Lossy() && droppedBlockEntities == 0 {
		fmt.Printf("%d chunk(s) translated with no loss\n\n", report.Chunks)
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
