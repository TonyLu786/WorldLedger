package seed

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
)

const InputSchema = "worldledger.seed-observations/v1"

// Input is the evidence a search runs against. Placement parameters are part of
// the input because they are per-world data: a datapack can change them, and
// assuming vanilla values would produce confident wrong answers on modified
// servers.
type Input struct {
	Schema       string        `json:"schema"`
	World        string        `json:"world,omitempty"`
	Note         string        `json:"note,omitempty"`
	Observations []Observation `json:"observations"`
}

func LoadInput(path string) (Input, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Input{}, err
	}
	var input Input
	if err := json.Unmarshal(data, &input); err != nil {
		return Input{}, fmt.Errorf("%s: %w", path, err)
	}
	if input.Schema != InputSchema {
		return Input{}, fmt.Errorf("%s: unsupported schema %q; want %q", path, input.Schema, InputSchema)
	}
	if len(input.Observations) == 0 {
		return Input{}, fmt.Errorf("%s: no observations", path)
	}
	for index, observation := range input.Observations {
		if err := observation.Placement.Validate(); err != nil {
			return Input{}, fmt.Errorf("%s: observation %d: %w", path, index, err)
		}
	}
	return input, nil
}

// Result is what a search produced, including the record of who ran it.
type Result struct {
	Acknowledgement Acknowledgement `json:"acknowledgement"`
	World           string          `json:"world,omitempty"`
	Observations    int             `json:"observations"`
	From            int64           `json:"from"`
	To              int64           `json:"to"`
	Scanned         uint64          `json:"scanned"`
	// SearchSpaceFraction is how much of the 48-bit space the scan covered.
	// Reported so a run that found nothing cannot be mistaken for a proof that
	// no seed exists.
	SearchSpaceFraction float64  `json:"search_space_fraction"`
	Candidates          []int64  `json:"candidates"`
	Truncated           bool     `json:"truncated"`
	Elapsed             string   `json:"elapsed"`
	Caveats             []string `json:"caveats"`
}

// Structure placement is driven by the low 48 bits, so a scan covers at most
// that much and a "seed" here is really a structure seed.
const structureSeedSpace = uint64(1) << 48

// Search scans a closed range of candidate seeds. It reports candidates rather
// than an answer: structure placement constrains the low 48 bits only, and
// nothing in this package checks the biome and terrain conditions that decide
// whether a structure really generates at a candidate position.
func Search(input Input, acknowledgement Acknowledgement, from, to int64, limit int) (Result, error) {
	if acknowledgement.Operator == "" {
		return Result{}, ErrNotAccepted
	}
	if to < from {
		return Result{}, errors.New("range end is before its start")
	}
	if limit <= 0 {
		limit = 1000
	}

	started := time.Now()
	result := Result{
		Acknowledgement: acknowledgement,
		World:           input.World,
		Observations:    len(input.Observations),
		From:            from,
		To:              to,
		Candidates:      []int64{},
	}

	for candidate := from; ; candidate++ {
		result.Scanned++
		if Consistent(candidate, input.Observations) {
			if len(result.Candidates) < limit {
				result.Candidates = append(result.Candidates, candidate)
			} else {
				result.Truncated = true
			}
		}
		if candidate == to {
			break
		}
	}

	result.SearchSpaceFraction = float64(result.Scanned) / float64(structureSeedSpace)
	if result.SearchSpaceFraction > 1 {
		result.SearchSpaceFraction = 1
	}
	result.Elapsed = time.Since(started).Round(time.Millisecond).String()
	result.Caveats = caveats(input, result)
	return result, nil
}

func caveats(input Input, result Result) []string {
	notes := []string{
		"Structure placement depends on the low 48 bits, so these are structure seeds; the remaining bits need biome or terrain evidence that this tool does not model.",
		"A match means a structure could start there, not that one does. Biome and terrain conditions are not checked.",
	}
	if len(input.Observations) < 3 {
		notes = append(notes, fmt.Sprintf("Only %d observation(s) were supplied; few constraints leave many unrelated seeds consistent.", len(input.Observations)))
	}
	if result.SearchSpaceFraction < 1 {
		notes = append(notes, fmt.Sprintf("The scan covered %.6f%% of the 48-bit space; finding nothing does not mean no seed exists.", result.SearchSpaceFraction*100))
	}
	if result.Truncated {
		notes = append(notes, "The candidate list was truncated; add observations rather than raising the limit.")
	}
	if len(result.Candidates) > 1 {
		notes = append(notes, "More than one candidate survived; treat the result as ambiguous.")
	}
	return notes
}

// EstimateFullScan reports how long the whole 48-bit space would take at the
// rate a scan actually achieved, so the cost is stated rather than implied.
func EstimateFullScan(result Result) (time.Duration, error) {
	elapsed, err := time.ParseDuration(result.Elapsed)
	if err != nil {
		return 0, err
	}
	if result.Scanned == 0 || elapsed <= 0 {
		return 0, errors.New("no measurable scan rate")
	}
	perSeed := float64(elapsed) / float64(result.Scanned)
	total := perSeed * float64(structureSeedSpace)
	if total > math.MaxInt64 {
		return time.Duration(math.MaxInt64), nil
	}
	return time.Duration(total), nil
}
