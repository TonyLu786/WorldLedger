package epoch

import (
	"sort"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// A snapshot answers what the world looked like at one moment. Two snapshots
// answer what changed between two moments, and that is a different question
// with a trap in it.
//
// A chunk selection carries the newest state at or before its epoch, so a chunk
// observed once in January still reports that state in June. Subtracting one
// snapshot from another would call such a chunk unchanged, which reads as "the
// world did not change here" when what happened is that nobody went back to
// look. Those two are not the same claim, and a world export cannot tell them
// apart at all: it has to write some block into every position.
//
// So the comparison here asks a second question the snapshots do not: was this
// chunk observed again during the interval? A state that was re-observed and
// found identical is a fact about the world. A state carried forward is a fact
// about the archive.

type ChangeKind string

const (
	// ChangeChanged means both moments have an observed state and the states
	// differ. Somebody saw the chunk on each side and saw something different.
	ChangeChanged ChangeKind = "changed"
	// ChangeUnchanged means the chunk was observed again during the interval and
	// the state was the same. This is a claim about the world.
	ChangeUnchanged ChangeKind = "unchanged"
	// ChangeNotRevisited means the state is identical only because it was carried
	// forward from before the interval began. Nobody observed this chunk while
	// the interval ran, so the archive has nothing to say about whether it
	// changed. This is a claim about the archive.
	ChangeNotRevisited ChangeKind = "not-revisited"
	// ChangeFirstSeen means the chunk had no observed state at the earlier moment
	// and has one at the later moment. What changed is what the archive knows,
	// not necessarily what the world holds.
	ChangeFirstSeen ChangeKind = "first-seen"
	// ChangeNeverSeen means neither moment has an observed state. The chunk is in
	// the index because something was observed after the interval ended.
	ChangeNeverSeen ChangeKind = "never-seen"
)

// Settled reports whether the kind is a statement about the world rather than
// about the limits of the archive. Callers that summarise a diff for a human
// use this to keep the two apart.
func (k ChangeKind) Settled() bool {
	return k == ChangeChanged || k == ChangeUnchanged
}

// Revisit records one observation made during the interval. A change is worth
// little without knowing who reported it and when.
type Revisit struct {
	Contributor string    `json:"contributor"`
	ObservedAt  time.Time `json:"observed_at"`
	StateDigest string    `json:"state_digest"`
	ID          string    `json:"id"`
}

type ChunkChange struct {
	Chunk model.ChunkRef `json:"chunk"`
	Kind  ChangeKind     `json:"kind"`

	// FromDigest and ToDigest are empty when the corresponding moment has no
	// observed state.
	FromDigest string `json:"from_digest,omitempty"`
	ToDigest   string `json:"to_digest,omitempty"`

	// FromStatus and ToStatus carry the snapshot's verdict on each side, so a
	// change between two states that were themselves disputed is not reported as
	// though both were settled.
	FromStatus Status `json:"from_status"`
	ToStatus   Status `json:"to_status"`

	// Revisits lists every observation of this chunk made during the interval,
	// oldest first. It is empty exactly when the kind is ChangeNotRevisited or
	// ChangeNeverSeen.
	Revisits []Revisit `json:"revisits,omitempty"`
}

// Contributors names everyone who observed this chunk during the interval, in
// first-observation order and without repeats.
func (c ChunkChange) Contributors() []string {
	seen := make(map[string]struct{}, len(c.Revisits))
	out := make([]string, 0, len(c.Revisits))
	for _, r := range c.Revisits {
		if _, dup := seen[r.Contributor]; dup {
			continue
		}
		seen[r.Contributor] = struct{}{}
		out = append(out, r.Contributor)
	}
	return out
}

type DiffSummary struct {
	Chunks int `json:"chunks"`

	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`

	// NotRevisited and NeverSeen are the chunks the archive cannot speak for.
	// They are counted separately from Unchanged on purpose: folding them in
	// would report an archive that stopped being updated as a world that stopped
	// changing.
	NotRevisited int `json:"not_revisited"`
	NeverSeen    int `json:"never_seen"`
	FirstSeen    int `json:"first_seen"`

	// Contributors names everyone whose observations fall inside the interval.
	Contributors []string `json:"contributors,omitempty"`
}

// Covered is the number of chunks the diff can make a claim about.
func (s DiffSummary) Covered() int {
	return s.Changed + s.Unchanged
}

type Diff struct {
	Server    string    `json:"server"`
	Dimension string    `json:"dimension"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Policy    Policy    `json:"policy"`

	Summary DiffSummary   `json:"summary"`
	Changes []ChunkChange `json:"changes"`
}

// BuildDiff compares the dimension at two moments. Both sides are selected from
// the same observations, so the comparison cannot straddle a concurrent import.
//
// The two instants are put in order first. The set of observations at or before
// a moment only ever grows, so with the earlier one first a chunk can gain an
// observed state across the interval but never lose one, and every kind below
// means what it says. A caller that passes them reversed is asking about the
// same interval and gets the same answer rather than a mirror image of it.
func BuildDiff(server, dimension string, from, to time.Time, inputs []ChunkInput) Diff {
	if to.Before(from) {
		from, to = to, from
	}
	diff := Diff{
		Server:    model.NormalizeToken(server),
		Dimension: model.NormalizeToken(dimension),
		From:      from.UTC(),
		To:        to.UTC(),
		Policy:    PolicyCorroboratedFirst,
		Changes:   make([]ChunkChange, 0, len(inputs)),
	}

	contributors := map[string]struct{}{}
	for _, input := range inputs {
		change := compareChunk(input, diff.From, diff.To)
		diff.Changes = append(diff.Changes, change)
		diff.Summary.Chunks++
		switch change.Kind {
		case ChangeChanged:
			diff.Summary.Changed++
		case ChangeUnchanged:
			diff.Summary.Unchanged++
		case ChangeNotRevisited:
			diff.Summary.NotRevisited++
		case ChangeFirstSeen:
			diff.Summary.FirstSeen++
		default:
			diff.Summary.NeverSeen++
		}
		for _, r := range change.Revisits {
			contributors[r.Contributor] = struct{}{}
		}
	}

	diff.Summary.Contributors = make([]string, 0, len(contributors))
	for name := range contributors {
		diff.Summary.Contributors = append(diff.Summary.Contributors, name)
	}
	sort.Strings(diff.Summary.Contributors)

	sort.Slice(diff.Changes, func(i, j int) bool {
		left, right := diff.Changes[i].Chunk, diff.Changes[j].Chunk
		if left.X != right.X {
			return left.X < right.X
		}
		return left.Z < right.Z
	})
	return diff
}

func compareChunk(input ChunkInput, from, to time.Time) ChunkChange {
	before := SelectChunk(input.Chunk, input.Observations, from)
	after := SelectChunk(input.Chunk, input.Observations, to)

	change := ChunkChange{
		Chunk:      input.Chunk,
		FromStatus: before.Status,
		ToStatus:   after.Status,
		Revisits:   revisitsWithin(input.Observations, from, to),
	}
	if before.Known() {
		change.FromDigest = before.Selected.StateDigest
	}
	if after.Known() {
		change.ToDigest = after.Selected.StateDigest
	}

	switch {
	case !before.Known() && !after.Known():
		change.Kind = ChangeNeverSeen
	case !before.Known():
		change.Kind = ChangeFirstSeen
	case !after.Known():
		// Unreachable: BuildDiff orders the instants, and the set of observations
		// at or before a moment only grows, so a chunk known at the earlier one is
		// known at the later one. Named for the only thing that would be true if
		// it ever happened rather than left to fall through to a wrong label.
		change.Kind = ChangeNeverSeen
	case change.FromDigest != change.ToDigest:
		change.Kind = ChangeChanged
	case len(change.Revisits) == 0:
		change.Kind = ChangeNotRevisited
	default:
		change.Kind = ChangeUnchanged
	}
	return change
}

// revisitsWithin collects observations made after from and at or before to.
//
// The bounds are deliberately half-open at the start. An observation exactly at
// from is what the earlier side already selected, so counting it as a revisit
// would let a chunk observed once, at the very instant the interval opened,
// claim it had been checked again.
func revisitsWithin(observations []model.Observation, from, to time.Time) []Revisit {
	var out []Revisit
	for _, o := range observations {
		at := o.ObservedAt.UTC()
		if !at.After(from) || at.After(to) {
			continue
		}
		out = append(out, Revisit{
			Contributor: o.Source.Contributor,
			ObservedAt:  at,
			StateDigest: o.StateDigest,
			ID:          o.ID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ObservedAt.Equal(out[j].ObservedAt) {
			return out[i].ObservedAt.Before(out[j].ObservedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}
