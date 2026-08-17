// Package reconstruct turns an archive into the state of a dimension at a
// moment.
//
// It exists because the step between the two is not only a lookup. Declared
// redactions have to be applied before anything is selected, and a caller that
// assembles the inputs itself and forgets them does not get a slightly wrong
// answer: it publishes observations somebody asked to have withheld.
//
// That was reachable. The command line did this inline, so the desktop
// application would have had to reimplement it, and the failure would have been
// silent -- an export that looked right and contained what it should not.
package reconstruct

import (
	"fmt"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/epoch"
	"github.com/worldledger/worldledger-mc/internal/redact"
)

// Inputs is what a dimension's chunks look like once redactions are applied.
type Inputs struct {
	Chunks []epoch.ChunkInput
	// Withheld is how many observations were removed, and Redactions how many
	// declarations did it. Both are reported rather than logged, so a caller
	// can tell somebody what is missing in whatever way suits it.
	Withheld   int
	Redactions int
}

// Gather reads a dimension and applies every declared redaction.
func Gather(a archive.Archive, server, dimension string) (Inputs, error) {
	gathered, err := a.DimensionObservations(server, dimension)
	if err != nil {
		return Inputs{}, err
	}
	redactions, err := redact.NewStore(a.Root).List()
	if err != nil {
		return Inputs{}, fmt.Errorf("read redactions: %w", err)
	}

	out := Inputs{
		Chunks:     make([]epoch.ChunkInput, 0, len(gathered)),
		Redactions: len(redactions),
	}
	for _, entry := range gathered {
		kept, dropped := redactions.Filter(entry.Observations)
		out.Withheld += len(dropped)
		if len(kept) == 0 {
			continue
		}
		out.Chunks = append(out.Chunks, epoch.ChunkInput{Chunk: entry.Chunk, Observations: kept})
	}
	return out, nil
}

// SnapshotAt is the state of a dimension as of a moment, with redactions
// already applied.
func SnapshotAt(a archive.Archive, server, dimension string, at time.Time) (epoch.Snapshot, Inputs, error) {
	inputs, err := Gather(a, server, dimension)
	if err != nil {
		return epoch.Snapshot{}, Inputs{}, err
	}
	return epoch.BuildSnapshot(server, dimension, at.UTC(), inputs.Chunks), inputs, nil
}
