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

// PolicyCorroboratedWithinWindow decides what a chunk holds from the
// observations closest in time to the most recent one, and counts older
// agreement as corroboration rather than as a vote.
//
// The name changed with the rule. It used to be corroborated-first, and it
// counted contributors before it consulted time at all, so four who last looked
// an hour ago outvoted two who had watched the chunk change a minute before.
// A snapshot manifest records its policy, so an export from either side of that
// change says which one produced it. See
// docs/decisions/0003-corroboration-and-time.md.
const PolicyCorroboratedWithinWindow Policy = "corroborated-within-window"

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
	// Support is how old the observations behind this are. A status is one word
	// and cannot carry it: two contributors who agree ten seconds apart and two
	// who agree a fortnight apart are both corroborated, and anybody deciding
	// how far to rely on that wants to know which they have.
	Support Support `json:"support"`
}

// Support describes the observations a selection rests on, and what the window
// left out of deciding it.
//
// A snapshot manifest carries this and a manifest is handed to other people, so
// nothing in here is formatted by a language. The span was a Go duration string
// for about an hour: "30m0s" is readable and it is also Go's spelling of a
// number, and the identity encoding elsewhere in this project goes to some
// trouble not to leave that choice open to whoever writes the second
// implementation. Integers do not have the choice.
type Support struct {
	Observations int       `json:"observations"`
	Earliest     time.Time `json:"earliest"`
	Latest       time.Time `json:"latest"`
	// SpanSeconds and SpanNanoseconds are Latest minus Earliest, split the way
	// an instant is split for identity: whole seconds and the remainder within
	// one second.
	SpanSeconds     int64 `json:"span_seconds"`
	SpanNanoseconds int32 `json:"span_nanoseconds"`
	// Counted is how many of the eligible observations were inside the window
	// and so decided what this chunk holds. Earlier is how many fell outside it.
	//
	// The second number is here because of what the window does when a clock is
	// wrong. The most recent eligible observation is what the window is measured
	// back from, so a contributor whose clock runs fast moves it, and
	// observations that were genuinely simultaneous with theirs fall outside and
	// stop voting. They still corroborate if they agree; if they disagree they
	// become what the chunk used to hold rather than a contradiction, so a
	// skewed clock can turn a conflict into a superseded and nothing would have
	// said so.
	//
	// It cannot be defended against here. An observed time is supplied by
	// whoever captured it and the trust model says so. What it can be is
	// visible, which is what this counts.
	Counted int `json:"counted"`
	Earlier int `json:"earlier"`
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
		Policy:     PolicyCorroboratedWithinWindow,
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

// SelectChunk applies PolicyCorroboratedWithinWindow to one chunk using the
// simultaneity window.
func SelectChunk(chunk model.ChunkRef, observations []model.Observation, at time.Time) Selection {
	return SelectChunkWithin(chunk, observations, at, DefaultSimultaneityWindow)
}

// SelectChunkWithin decides one chunk. Each contributor is represented by its
// most recent observation at or before the epoch, so a contributor cannot
// outweigh others by submitting the same state repeatedly.
//
// Agreement and currency are two questions and this used to answer the first
// while being read as an answer to both. The count came first and took no
// account of when anybody looked, so four contributors whose last visit was an
// hour ago outvoted two who had watched the chunk change a minute ago, and the
// older state came out under this project's strongest confidence word with no
// conflict and no superseded anywhere. That was reproduced, written up as
// ADR 0003, and is what this now does differently.
//
// The window is consulted before the vote rather than after it. The most recent
// eligible observation fixes a window, and only what falls inside it decides
// what the chunk holds now:
//
//   - two states inside the window is a contradiction, because the world is
//     unlikely to have changed between them. That is a conflict and it is
//     settled among those observations alone;
//   - otherwise the state inside the window is the state. Older observations
//     agreeing with it still corroborate it, because nothing has contradicted
//     them; older observations disagreeing with it are what the chunk used to
//     hold, and they are evidence rather than votes.
//
// A state cannot be reported as the state at a moment on the strength of votes
// that predate a contradicting observation. That is the whole of the change.
func SelectChunkWithin(
	chunk model.ChunkRef, observations []model.Observation, at time.Time, window time.Duration) Selection {
	eligible := latestPerContributor(observations, at)
	if len(eligible) == 0 {
		return Selection{Chunk: chunk, Status: StatusUnknown}
	}

	contemporary, earlier := splitByRecency(eligible, window)
	current := groupByState(contemporary)

	var selected StateGroup
	status := StatusSingleSource
	if len(current) > 1 {
		// Seen differently at about the same moment. Which of them is right is
		// not something a count settles, and saying so is the point of the
		// label.
		selected = mostRecentGroup(current)
		status = StatusConflict
	} else {
		selected = current[0]
		supporters := selected.Contributors
		if len(earlier) > 0 {
			agreeing := observationsWithState(earlier, selected.StateDigest)
			selected.Observations = append(append([]model.Observation(nil), selected.Observations...), agreeing...)
			sort.Slice(selected.Observations, func(i, j int) bool {
				return observationBefore(selected.Observations[i], selected.Observations[j])
			})
			supporters = uniqueContributors(selected.Observations)
			selected.Contributors = supporters
		}
		switch {
		case len(supporters) >= 2:
			status = StatusCorroborated
		case disagrees(earlier, selected.StateDigest):
			// Nobody else has seen what is there now, and somebody saw
			// something else before it. The world changed and one person has
			// been back since.
			status = StatusSuperseded
		}
	}

	rejected := rejectedGroups(eligible, selected.StateDigest)
	winner := mostRecent(selected.Observations)
	return Selection{
		Chunk:        chunk,
		Status:       status,
		Selected:     &winner,
		Contributors: selected.Contributors,
		Rejected:     rejected,
		Support:      supportOf(selected.Observations, len(contemporary), len(earlier)),
	}
}

// splitByRecency divides the eligible observations at one window back from the
// most recent of them.
//
// The most recent decides where the window sits, rather than the epoch,
// because the question is what the last people to look agreed about. An epoch
// far in the future would otherwise put every observation outside its own
// window and leave nothing to decide with.
func splitByRecency(eligible []model.Observation, window time.Duration) (contemporary, earlier []model.Observation) {
	newest := eligible[0].ObservedAt
	for _, o := range eligible[1:] {
		if o.ObservedAt.After(newest) {
			newest = o.ObservedAt
		}
	}
	cutoff := newest.Add(-window)
	for _, o := range eligible {
		if o.ObservedAt.Before(cutoff) {
			earlier = append(earlier, o)
			continue
		}
		contemporary = append(contemporary, o)
	}
	return contemporary, earlier
}

func observationsWithState(observations []model.Observation, digest string) []model.Observation {
	var out []model.Observation
	for _, o := range observations {
		if o.StateDigest == digest {
			out = append(out, o)
		}
	}
	return out
}

func disagrees(observations []model.Observation, digest string) bool {
	for _, o := range observations {
		if o.StateDigest != digest {
			return true
		}
	}
	return false
}

// rejectedGroups is every state that was not selected, over the whole eligible
// set rather than only the recent part of it. What the chunk used to hold is
// evidence whether or not it was allowed to vote.
func rejectedGroups(eligible []model.Observation, selectedDigest string) []StateGroup {
	var losing []model.Observation
	for _, o := range eligible {
		if o.StateDigest != selectedDigest {
			losing = append(losing, o)
		}
	}
	if len(losing) == 0 {
		return nil
	}
	return groupByState(losing)
}

// supportOf describes how old the observations behind a selection are, and how
// many of the eligible ones the window left out of deciding it.
//
// A status is one word and cannot carry this. Two contributors who agree ten
// seconds apart and two who agree a fortnight apart are both corroborated, and
// anybody deciding how much to rely on that wants to know which they have.
func supportOf(observations []model.Observation, counted, earlier int) Support {
	support := Support{Counted: counted, Earlier: earlier}
	if len(observations) == 0 {
		return support
	}
	earliestAt, latestAt := observations[0].ObservedAt, observations[0].ObservedAt
	for _, o := range observations[1:] {
		if o.ObservedAt.Before(earliestAt) {
			earliestAt = o.ObservedAt
		}
		if o.ObservedAt.After(latestAt) {
			latestAt = o.ObservedAt
		}
	}
	span := latestAt.Sub(earliestAt)
	support.Observations = len(observations)
	support.Earliest = earliestAt.UTC()
	support.Latest = latestAt.UTC()
	support.SpanSeconds = int64(span / time.Second)
	support.SpanNanoseconds = int32(span % time.Second)
	return support
}

func latestPerContributor(observations []model.Observation, at time.Time) []model.Observation {
	byContributor := map[string]model.Observation{}
	for _, o := range observations {
		if o.ObservedAt.After(at) {
			continue
		}
		key := model.ContributorKey(o.Source.Contributor)
		current, exists := byContributor[key]
		if !exists || observationBefore(current, o) {
			byContributor[key] = o
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
		seen[model.ContributorKey(o.Source.Contributor)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for contributor := range seen {
		out = append(out, contributor)
	}
	sort.Strings(out)
	return out
}
