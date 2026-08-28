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
	//
	// Two things follow from that word, and both are enforced or relied on
	// elsewhere, so a rule that is not really a rename does damage quietly.
	// The source's block state properties are carried onto the replacement
	// unconditionally, because the same block has the same properties -- unlike
	// a substitution, which drops them unless KeepProperties says otherwise.
	// And a rename is not counted as a loss, because nothing was lost.
	//
	// Neither holds for a rule that is really a substitution wearing a rename's
	// name. Validate refuses the case it can detect, two sources becoming one
	// block; it cannot detect a replacement that does not share the source's
	// properties, because a release profile records block identifiers and not
	// their states. Use a substitution whenever the replacement is a different
	// block.
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
	// Two renames onto one block is a merge, and a merge is not a rename.
	//
	// A rename asserts that this is the same block under a different name, which
	// is why it carries the source's properties and why it is not counted as a
	// loss. Two sources arriving at one name breaks both halves of that: the
	// palette rebuild collapses them, the world can no longer tell them apart,
	// and the run reported "translated with no loss".
	//
	// It is also an easy mistake rather than an exotic one. The shipped rename
	// table has the chain kelp_top -> kelp -> kelp_plant, and an operator
	// reversing it for a downgrade produces exactly this.
	//
	// A merge is a substitution: that is what substitutions are for, they are
	// counted as lossy, and they do not carry properties unless asked.
	mergedInto := map[string]string{}
	for _, source := range sortedKeys(r.Renames) {
		replacement := r.Renames[source]
		if first, seen := mergedInto[replacement]; seen {
			return fmt.Errorf(
				"renames %s and %s both become %s; two blocks becoming one is a loss, "+
					"so declare it as a substitution rather than a rename",
				first, source, replacement)
		}
		mergedInto[replacement] = source
	}

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

// sortedKeys gives a stable order, so a ruleset with two mistakes in it names
// the same one every time rather than whichever the map iterator reached first.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
