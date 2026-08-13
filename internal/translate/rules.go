// Package translate rewrites canonical observed state so that a target
// Minecraft release can represent it.
//
// Translation is always lossy in the direction of an older release: a block
// introduced later, or added by a mod, simply does not exist there. This package
// never resolves that silently. Every state it cannot carry across is either
// refused, dropped with the chunk, or replaced under a rule the operator chose,
// and every replacement is counted and reported.
package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/mcprofile"
)

const RulesSchema = "worldledger.translation-rules/v1"

// Policy decides what happens to a state the target release cannot represent
// and no rule covers.
type Policy string

const (
	// PolicyReport refuses the translation and reports everything that does not
	// fit. Nothing is written.
	PolicyReport Policy = "report"
	// PolicySkipChunk leaves the whole chunk unwritten, so the target world
	// shows nothing there rather than an approximation.
	PolicySkipChunk Policy = "skip-chunk"
	// PolicyFill replaces the state with the configured filler block.
	PolicyFill Policy = "fill"
)

func ParsePolicy(value string) (Policy, error) {
	switch Policy(strings.TrimSpace(value)) {
	case PolicyReport:
		return PolicyReport, nil
	case PolicySkipChunk:
		return PolicySkipChunk, nil
	case PolicyFill:
		return PolicyFill, nil
	}
	return "", fmt.Errorf("unknown policy %q; want report, skip-chunk, or fill", value)
}

type Outcome string

const (
	// OutcomeIdentity means the target release already has the state.
	OutcomeIdentity Outcome = "identity"
	// OutcomeRenamed means the same block under a different identifier. The
	// state is preserved.
	OutcomeRenamed Outcome = "renamed"
	// OutcomeSubstituted means a different block chosen by the operator as
	// functionally close. The state is an approximation.
	OutcomeSubstituted Outcome = "substituted"
	// OutcomeFilled means no rule applied and the filler block was used.
	OutcomeFilled Outcome = "filled"
	// OutcomeUnrepresentable means the state was not carried across at all.
	OutcomeUnrepresentable Outcome = "unrepresentable"
)

// Lossy reports whether an outcome changed what the world asserts.
func (o Outcome) Lossy() bool {
	return o != OutcomeIdentity && o != OutcomeRenamed
}

// Substitution names a functionally similar block in the target release.
type Substitution struct {
	Block string `json:"block"`
	// KeepProperties carries the source properties onto the replacement. It is
	// only sound when the replacement genuinely shares them; a replacement that
	// does not will produce a state the target release rejects, so it defaults
	// to dropping them.
	KeepProperties bool `json:"keep_properties,omitempty"`
}

type Rules struct {
	Schema string `json:"schema"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	// Renames are identity preserving: the same block under a new identifier.
	Renames map[string]string `json:"renames,omitempty"`
	// Substitutions are approximations chosen by the operator.
	Substitutions      map[string]Substitution `json:"substitutions,omitempty"`
	BiomeRenames       map[string]string       `json:"biome_renames,omitempty"`
	BiomeSubstitutions map[string]string       `json:"biome_substitutions,omitempty"`
}

func LoadRules(path string) (Rules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Rules{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(mcprofile.TrimBOM(data)))
	decoder.DisallowUnknownFields()
	var rules Rules
	if err := decoder.Decode(&rules); err != nil {
		return Rules{}, fmt.Errorf("%s: %w", path, err)
	}
	if rules.Schema != RulesSchema {
		return Rules{}, fmt.Errorf("%s: unsupported rules schema %q", path, rules.Schema)
	}
	return rules, nil
}

// Validate rejects rules that point at something the target release does not
// have. Catching a typo or an impossible mapping here is far better than
// discovering it as an unreadable chunk.
func (r Rules) Validate(target mcprofile.Profile) error {
	for source, replacement := range r.Renames {
		if err := checkResourceLocation(source, "rename source"); err != nil {
			return err
		}
		if !target.HasBlock(replacement) {
			return fmt.Errorf("rename %s -> %s: target release has no such block", source, replacement)
		}
		if _, duplicated := r.Substitutions[source]; duplicated {
			return fmt.Errorf("%s is both renamed and substituted", source)
		}
	}
	for source, substitution := range r.Substitutions {
		if err := checkResourceLocation(source, "substitution source"); err != nil {
			return err
		}
		if !target.HasBlock(substitution.Block) {
			return fmt.Errorf("substitution %s -> %s: target release has no such block", source, substitution.Block)
		}
	}
	for source, replacement := range r.BiomeRenames {
		if err := checkResourceLocation(source, "biome rename source"); err != nil {
			return err
		}
		if !target.HasBiome(replacement) {
			return fmt.Errorf("biome rename %s -> %s: target release has no such biome", source, replacement)
		}
		if _, duplicated := r.BiomeSubstitutions[source]; duplicated {
			return fmt.Errorf("%s is both renamed and substituted", source)
		}
	}
	for source, replacement := range r.BiomeSubstitutions {
		if err := checkResourceLocation(source, "biome substitution source"); err != nil {
			return err
		}
		if !target.HasBiome(replacement) {
			return fmt.Errorf("biome substitution %s -> %s: target release has no such biome", source, replacement)
		}
	}
	return nil
}

func checkResourceLocation(value, what string) error {
	if strings.Count(value, ":") != 1 {
		return fmt.Errorf("%s %q must be a namespaced resource location", what, value)
	}
	parts := strings.SplitN(value, ":", 2)
	if parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("%s %q is not a valid resource location", what, value)
	}
	return nil
}

// Change records what happened to one distinct source state.
type Change struct {
	Source    string  `json:"source"`
	Outcome   Outcome `json:"outcome"`
	Target    string  `json:"target,omitempty"`
	Positions int     `json:"positions"`
}

type Report struct {
	Target         string   `json:"target"`
	Policy         Policy   `json:"policy"`
	Chunks         int      `json:"chunks"`
	SkippedChunks  int      `json:"skipped_chunks"`
	DroppedSection int      `json:"dropped_sections"`
	Blocks         []Change `json:"blocks,omitempty"`
	Biomes         []Change `json:"biomes,omitempty"`
}

// Lossy reports whether anything was approximated, filled, or dropped.
func (r Report) Lossy() bool {
	if r.SkippedChunks > 0 || r.DroppedSection > 0 {
		return true
	}
	for _, change := range append(append([]Change(nil), r.Blocks...), r.Biomes...) {
		if change.Outcome.Lossy() {
			return true
		}
	}
	return false
}

type counter struct {
	outcome   Outcome
	target    string
	positions int
}

func summarize(counts map[string]*counter) []Change {
	changes := make([]Change, 0, len(counts))
	for source, entry := range counts {
		if entry.outcome == OutcomeIdentity {
			continue
		}
		changes = append(changes, Change{
			Source:    source,
			Outcome:   entry.outcome,
			Target:    entry.target,
			Positions: entry.positions,
		})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Positions != changes[j].Positions {
			return changes[i].Positions > changes[j].Positions
		}
		return changes[i].Source < changes[j].Source
	})
	return changes
}

func canonicalOrError(state mcjava.BlockState) (string, error) {
	return mcjava.CanonicalBlockState(state)
}
