package verify

import (
	"sort"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

type StateGroup struct {
	StateDigest  string              `json:"state_digest"`
	Contributors []string            `json:"contributors"`
	Observations []model.Observation `json:"observations"`
}

type Window struct {
	Start  time.Time    `json:"start"`
	End    time.Time    `json:"end"`
	Status string       `json:"status"`
	States []StateGroup `json:"states"`
}

func BuildWindows(observations []model.Observation, width time.Duration) []Window {
	if len(observations) == 0 {
		return nil
	}
	if width <= 0 {
		width = 10 * time.Second
	}

	obs := append([]model.Observation(nil), observations...)
	sort.Slice(obs, func(i, j int) bool { return obs[i].ObservedAt.Before(obs[j].ObservedAt) })

	var windows []Window
	for i := 0; i < len(obs); {
		start := obs[i].ObservedAt
		end := start.Add(width)
		j := i
		for j < len(obs) && !obs[j].ObservedAt.After(end) {
			j++
		}
		windows = append(windows, summarize(start, end, obs[i:j]))
		i = j
	}
	return windows
}

func summarize(start, end time.Time, obs []model.Observation) Window {
	byState := map[string][]model.Observation{}
	for _, o := range obs {
		byState[o.StateDigest] = append(byState[o.StateDigest], o)
	}

	states := make([]StateGroup, 0, len(byState))
	maxIndependent := 0
	for digest, items := range byState {
		contributors := uniqueContributors(items)
		if len(contributors) > maxIndependent {
			maxIndependent = len(contributors)
		}
		states = append(states, StateGroup{
			StateDigest:  digest,
			Contributors: contributors,
			Observations: items,
		})
	}
	sort.Slice(states, func(i, j int) bool {
		if len(states[i].Contributors) == len(states[j].Contributors) {
			return states[i].StateDigest < states[j].StateDigest
		}
		return len(states[i].Contributors) > len(states[j].Contributors)
	})

	status := "single-source"
	switch {
	case len(states) > 1:
		status = "conflict"
	case maxIndependent >= 2:
		status = "corroborated"
	}
	return Window{Start: start, End: end, Status: status, States: states}
}

func uniqueContributors(obs []model.Observation) []string {
	seen := map[string]struct{}{}
	for _, o := range obs {
		seen[o.Source.Contributor] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for contributor := range seen {
		out = append(out, contributor)
	}
	sort.Strings(out)
	return out
}
