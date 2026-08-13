// Package epoch selects, for a point in time, which observed state a
// reconstruction should use for each chunk. It never discards a state it did not
// select: rejected states are returned alongside the selection as evidence.
package epoch

import (
	"sort"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

type Policy string

// PolicyCorroboratedFirst prefers a state reported by two or more independent
// contributors and falls back to the most recent state when no state is
// corroborated or when corroboration is tied.
const PolicyCorroboratedFirst Policy = "corroborated-first"

type Status string

const (
	// StatusUnknown means the chunk has observations, but none at or before the
	// epoch. It is never rendered as air or as an empty chunk.
	StatusUnknown      Status = "unknown"
	StatusSingleSource Status = "single-source"
	StatusCorroborated Status = "corroborated"
	// StatusConflict means contributors reported different states close enough
	// in time that the world is unlikely to have changed between them. This is
	// the case worth a human's attention.
	StatusConflict Status = "conflict"
	// StatusSuperseded means contributors reported different states, but far
	// enough apart that an ordinary world edit explains it. The later state was
	// used. Every earlier state is still preserved.
	//
	// Separating this from StatusConflict matters: a Minecraft world is mutable,
	// so two different states minutes apart are the expected case rather than a
	// disagreement, and reporting them as conflicts buries the few that are.
	StatusSuperseded Status = "superseded"
)

// DefaultSimultaneityWindow is how close two observations must be before their
// disagreement is treated as contributors contradicting each other rather than
// as the world having changed between them.
//
// It is a judgement about Minecraft rather than a measurement: a player can
// change a chunk at any moment, so no window makes the distinction certain. A
// short one keeps the conflict label meaningful, which is the point of having
// the label at all.
const DefaultSimultaneityWindow = 30 * time.Second

type StateGroup struct {
	StateDigest  string              `json:"state_digest"`
	Contributors []string            `json:"contributors"`
	Observations []model.Observation `json:"observations"`
}

type Selection struct {
	Chunk        model.ChunkRef     `json:"chunk"`
	Status       Status             `json:"status"`
	Selected     *model.Observation `json:"selected,omitempty"`
	Contributors []string           `json:"contributors,omitempty"`
	Rejected     []StateGroup       `json:"rejected,omitempty"`
}

func (s Selection) Known() bool {
	return s.Selected != nil
}

type ChunkInput struct {
	Chunk        model.ChunkRef
	Observations []model.Observation
}

type Summary struct {
	Chunks       int `json:"chunks"`
	Corroborated int `json:"corroborated"`
	SingleSource int `json:"single_source"`
	// Conflict counts disagreement close enough in time to need a human.
	Conflict int `json:"conflict"`
	// Superseded counts disagreement far enough apart that the world changing
	// explains it.
	Superseded int `json:"superseded"`
	Unknown    int `json:"unknown"`
}

type Snapshot struct {
	Server     string      `json:"server"`
	Dimension  string      `json:"dimension"`
	At         time.Time   `json:"at"`
	Policy     Policy      `json:"policy"`
	Summary    Summary     `json:"summary"`
	Selections []Selection `json:"selections"`
}

func BuildSnapshot(server, dimension string, at time.Time, inputs []ChunkInput) Snapshot {
	snapshot := Snapshot{
		Server:     model.NormalizeToken(server),
		Dimension:  model.NormalizeToken(dimension),
		At:         at.UTC(),
		Policy:     PolicyCorroboratedFirst,
		Selections: make([]Selection, 0, len(inputs)),
	}
	for _, input := range inputs {
		selection := SelectChunk(input.Chunk, input.Observations, at)
		snapshot.Selections = append(snapshot.Selections, selection)
		snapshot.Summary.Chunks++
		switch selection.Status {
		case StatusCorroborated:
			snapshot.Summary.Corroborated++
		case StatusSingleSource:
			snapshot.Summary.SingleSource++
		case StatusConflict:
			snapshot.Summary.Conflict++
		case StatusSuperseded:
			snapshot.Summary.Superseded++
		default:
			snapshot.Summary.Unknown++
		}
	}
	return snapshot
}

// SelectChunk applies PolicyCorroboratedFirst to one chunk using the default
// simultaneity window.
func SelectChunk(chunk model.ChunkRef, observations []model.Observation, at time.Time) Selection {
	return SelectChunkWithin(chunk, observations, at, DefaultSimultaneityWindow)
}

// SelectChunkWithin applies PolicyCorroboratedFirst to one chunk. Each
// contributor is represented by its most recent observation at or before the
// epoch, so a contributor cannot outweigh others by submitting the same state
// repeatedly.
//
// The window decides how disagreement is labelled. States seen within it are a
// conflict; states further apart are a change the archive recorded, and the
// later one supersedes the earlier.
func SelectChunkWithin(
	chunk model.ChunkRef, observations []model.Observation, at time.Time, window time.Duration) Selection {
	eligible := latestPerContributor(observations, at)
	if len(eligible) == 0 {
		return Selection{Chunk: chunk, Status: StatusUnknown}
	}

	groups := groupByState(eligible)
	best := groups[0]
	corroborated := len(best.Contributors) >= 2 &&
		(len(groups) == 1 || len(best.Contributors) > len(groups[1].Contributors))

	selected := best
	status := StatusSingleSource
	switch {
	case corroborated:
		status = StatusCorroborated
	case len(groups) > 1:
		selected = mostRecentGroup(groups)
		// Disagreement only counts as contradiction when the states were seen
		// close together; otherwise the world simply changed.
		if spread(groups) <= window {
			status = StatusConflict
		} else {
			status = StatusSuperseded
		}
	}

	rejected := make([]StateGroup, 0, len(groups)-1)
	for _, group := range groups {
		if group.StateDigest != selected.StateDigest {
			rejected = append(rejected, group)
		}
	}
	if len(rejected) == 0 {
		rejected = nil
	}

	winner := mostRecent(selected.Observations)
	return Selection{
		Chunk:        chunk,
		Status:       status,
		Selected:     &winner,
		Contributors: selected.Contributors,
		Rejected:     rejected,
	}
}

func latestPerContributor(observations []model.Observation, at time.Time) []model.Observation {
	byContributor := map[string]model.Observation{}
	for _, o := range observations {
		if o.ObservedAt.After(at) {
			continue
		}
		current, exists := byContributor[o.Source.Contributor]
		if !exists || observationBefore(current, o) {
			byContributor[o.Source.Contributor] = o
		}
	}
	out := make([]model.Observation, 0, len(byContributor))
	for _, o := range byContributor {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return observationBefore(out[i], out[j]) })
	return out
}

func groupByState(observations []model.Observation) []StateGroup {
	byState := map[string][]model.Observation{}
	for _, o := range observations {
		byState[o.StateDigest] = append(byState[o.StateDigest], o)
	}

	groups := make([]StateGroup, 0, len(byState))
	for digest, items := range byState {
		sort.Slice(items, func(i, j int) bool { return observationBefore(items[i], items[j]) })
		groups = append(groups, StateGroup{
			StateDigest:  digest,
			Contributors: uniqueContributors(items),
			Observations: items,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		left, right := groups[i], groups[j]
		if len(left.Contributors) != len(right.Contributors) {
			return len(left.Contributors) > len(right.Contributors)
		}
		leftLatest, rightLatest := mostRecent(left.Observations), mostRecent(right.Observations)
		if !leftLatest.ObservedAt.Equal(rightLatest.ObservedAt) {
			return rightLatest.ObservedAt.Before(leftLatest.ObservedAt)
		}
		return left.StateDigest < right.StateDigest
	})
	return groups
}

// spread is the time between the earliest and latest observation across all
// competing states.
func spread(groups []StateGroup) time.Duration {
	var earliest, latest time.Time
	for _, group := range groups {
		for _, observation := range group.Observations {
			if earliest.IsZero() || observation.ObservedAt.Before(earliest) {
				earliest = observation.ObservedAt
			}
			if latest.IsZero() || observation.ObservedAt.After(latest) {
				latest = observation.ObservedAt
			}
		}
	}
	return latest.Sub(earliest)
}

func mostRecentGroup(groups []StateGroup) StateGroup {
	best := groups[0]
	for _, group := range groups[1:] {
		if observationBefore(mostRecent(best.Observations), mostRecent(group.Observations)) {
			best = group
		}
	}
	return best
}

func mostRecent(observations []model.Observation) model.Observation {
	best := observations[0]
	for _, o := range observations[1:] {
		if observationBefore(best, o) {
			best = o
		}
	}
	return best
}

// observationBefore is a total order, so selection never depends on map or slice
// iteration order.
func observationBefore(left, right model.Observation) bool {
	if !left.ObservedAt.Equal(right.ObservedAt) {
		return left.ObservedAt.Before(right.ObservedAt)
	}
	return left.ID < right.ID
}

func uniqueContributors(observations []model.Observation) []string {
	seen := map[string]struct{}{}
	for _, o := range observations {
		seen[o.Source.Contributor] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for contributor := range seen {
		out = append(out, contributor)
	}
	sort.Strings(out)
	return out
}
