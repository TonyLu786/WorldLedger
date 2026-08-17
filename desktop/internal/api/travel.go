package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/epoch"
	"github.com/worldledger/worldledger-mc/internal/reconstruct"
)

// Time travel is the thing a world downloader cannot do, and it has been
// available from the command line only, where nobody who would care about it
// will find it.
//
// The distinction that matters is the one a picture makes easy to lose. Between
// two moments a chunk either changed, or did not change, or was never looked at
// again -- and the third is not the second. A map that paints "no change" over
// somewhere nobody visited is telling a story about a place it has no reading
// from, which is the whole failure this project is against.

type momentsAnswer struct {
	Server    string   `json:"server"`
	Dimension string   `json:"dimension"`
	Moments   []moment `json:"moments"`
}

// moment is a time at which something was observed, offered as a choice.
type moment struct {
	At    string `json:"at"`
	Label string `json:"label"`
	// Chunks is how many chunks were observed within that hour, which is what
	// makes one moment worth choosing over another.
	Chunks int `json:"chunks"`
}

func handleMoments(w http.ResponseWriter, r *http.Request) {
	server := r.URL.Query().Get("server")
	dimension := r.URL.Query().Get("dimension")
	if dimension == "" {
		dimension = "minecraft:overworld"
	}
	if server == "" {
		app.WriteFailure(w, http.StatusBadRequest, "no server was given", "pick a server first")
		return
	}

	a, err := openArchive()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"the archive could not be opened; restarting the application is the first thing to try")
		return
	}
	inputs, err := reconstruct.Gather(a, server, dimension)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"this usually means the archive is damaged")
		return
	}

	// Grouped by hour, because every observation is its own instant and offering
	// a player several hundred timestamps that differ by seconds is offering
	// them nothing they can choose between.
	//
	// The instant reported for a group is the last observation in it, not the
	// hour it starts. Reporting the start looked reasonable and was wrong in a
	// way that made the screen useless: an hour's observations happen after the
	// hour begins, so "as of 02:00" is before all of them, and a player whose
	// whole session fell in one hour opened this to be told that all forty
	// places had never been seen.
	latest := map[time.Time]time.Time{}
	counts := map[time.Time]int{}
	for _, input := range inputs.Chunks {
		for _, observation := range input.Observations {
			at := observation.ObservedAt.UTC()
			bucket := at.Truncate(time.Hour)
			counts[bucket]++
			if at.After(latest[bucket]) {
				latest[bucket] = at
			}
		}
	}

	answer := momentsAnswer{Server: server, Dimension: dimension}
	for bucket, chunks := range counts {
		answer.Moments = append(answer.Moments, moment{
			At:     latest[bucket].Format(time.RFC3339Nano),
			Label:  bucket.Format("2 January 2006, 15:04"),
			Chunks: chunks,
		})
	}
	sort.Slice(answer.Moments, func(i, j int) bool {
		return answer.Moments[i].At < answer.Moments[j].At
	})
	app.WriteJSON(w, http.StatusOK, answer)
}

type travelAnswer struct {
	Server    string `json:"server"`
	Dimension string `json:"dimension"`
	From      string `json:"from"`
	To        string `json:"to"`

	Changed      int `json:"changed"`
	Unchanged    int `json:"unchanged"`
	NotRevisited int `json:"not_revisited"`
	FirstSeen    int `json:"first_seen"`
	NeverSeen    int `json:"never_seen"`

	Chunks []travelChunk `json:"chunks"`
	// Honesty is what the page prints under the picture, so the wording lives
	// with the numbers rather than being written twice.
	Honesty string `json:"honesty"`
}

type travelChunk struct {
	X    int32  `json:"x"`
	Z    int32  `json:"z"`
	Kind string `json:"kind"`
}

func handleTravel(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	server := query.Get("server")
	dimension := query.Get("dimension")
	if dimension == "" {
		dimension = "minecraft:overworld"
	}
	if server == "" || query.Get("from") == "" || query.Get("to") == "" {
		app.WriteFailure(w, http.StatusBadRequest,
			"two moments are needed to compare", "pick a from and a to")
		return
	}

	from, err := time.Parse(time.RFC3339Nano, query.Get("from"))
	if err != nil {
		app.WriteFailure(w, http.StatusBadRequest, "the first moment could not be read", "pick it from the list")
		return
	}
	to, err := time.Parse(time.RFC3339Nano, query.Get("to"))
	if err != nil {
		app.WriteFailure(w, http.StatusBadRequest, "the second moment could not be read", "pick it from the list")
		return
	}

	a, err := openArchive()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"the archive could not be opened; restarting the application is the first thing to try")
		return
	}
	inputs, err := reconstruct.Gather(a, server, dimension)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"this usually means the archive is damaged")
		return
	}

	// BuildDiff orders the two instants itself, so a player who picks them the
	// wrong way round gets the comparison rather than a complaint.
	diff := epoch.BuildDiff(server, dimension, from.UTC(), to.UTC(), inputs.Chunks)

	answer := travelAnswer{
		Server:       server,
		Dimension:    dimension,
		From:         diff.From.Format(time.RFC3339),
		To:           diff.To.Format(time.RFC3339),
		Changed:      diff.Summary.Changed,
		Unchanged:    diff.Summary.Unchanged,
		NotRevisited: diff.Summary.NotRevisited,
		FirstSeen:    diff.Summary.FirstSeen,
		NeverSeen:    diff.Summary.NeverSeen,
		Chunks:       make([]travelChunk, 0, len(diff.Changes)),
		Honesty: "Grey is where nobody looked again, not where nothing happened. " +
			"An archive that cannot tell those apart is guessing.",
	}
	for _, change := range diff.Changes {
		answer.Chunks = append(answer.Chunks, travelChunk{
			X:    change.Chunk.X,
			Z:    change.Chunk.Z,
			Kind: string(change.Kind),
		})
	}
	app.WriteJSON(w, http.StatusOK, answer)
}
