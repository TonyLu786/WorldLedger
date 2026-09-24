package archive

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// RebuildReport is what a rebuild did.
type RebuildReport struct {
	Observations int `json:"observations"`
	Chunks       int `json:"chunks"`
	// Unreadable counts observation files that could not be used. They are left
	// exactly where they are: an index can be built again and a record cannot,
	// so a rebuild that deleted what it could not parse would be trading the
	// recoverable thing for the unrecoverable one.
	Unreadable []string `json:"unreadable,omitempty"`
}

// RebuildIndex derives the chunk index again from the observations.
//
// The index is not a record of anything. Every line in it is an observation id
// under the chunk that observation names, and both of those come out of the
// observation file itself, so the whole of index/ is a restatement of
// observations/ arranged for reading. Nothing is lost by discarding it and
// nothing is decided by writing it.
//
// That property is worth having a function for, and worth having a function
// that proves it. It repairs an archive whose index was damaged, which is
// otherwise a permanent failure: an index entry naming a missing observation
// or an observation missing from its index both fail the integrity check and
// neither had a fix. And it is what makes a layout change affordable, because a
// layout change is almost always a change to the arrangement rather than to the
// records, and rearranging is what this does.
//
// The observations are not touched. An archive that goes through this holds the
// same claims it held before, by the same people, with the same identities.
func (a Archive) RebuildIndex() (RebuildReport, error) {
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return RebuildReport{}, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()

	observations, report, err := a.readAllObservationsLocked()
	if err != nil {
		return RebuildReport{}, err
	}

	// Counted before anything is removed, so the report is of what was read
	// rather than of what happened to survive the writing.
	chunks := map[string]struct{}{}
	for _, o := range observations {
		chunks[a.chunkIndexPath(o.Chunk)] = struct{}{}
	}

	// Everything below runs under the archive lock, and an interruption leaves
	// an archive whose index is incomplete. That is the state this repairs, so
	// running it again finishes it; it is not a state it can be left in
	// silently, because the integrity check reports exactly this.
	live := filepath.Join(a.Root, "index", "chunks")
	if err := os.RemoveAll(live); err != nil {
		return RebuildReport{}, fmt.Errorf("clear the old index: %w", err)
	}
	for _, o := range observations {
		if err := a.commitIndex(o); err != nil {
			return RebuildReport{}, fmt.Errorf("index observation %s: %w", o.ID, err)
		}
	}

	report.Observations = len(observations)
	report.Chunks = len(chunks)
	return report, nil
}

// readAllObservationsLocked reads every stored observation.
func (a Archive) readAllObservationsLocked() ([]model.Observation, RebuildReport, error) {
	var report RebuildReport
	var out []model.Observation

	root := filepath.Join(a.Root, "observations")
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			report.Unreadable = append(report.Unreadable, path)
			return nil
		}
		var o model.Observation
		if err := json.Unmarshal(data, &o); err != nil {
			report.Unreadable = append(report.Unreadable, path)
			return nil
		}
		// An observation whose file name does not match its own identity cannot
		// be placed, and guessing which of the two is right is not something an
		// index rebuild is entitled to do.
		if strings.TrimSuffix(entry.Name(), ".json") != o.ID {
			report.Unreadable = append(report.Unreadable, path)
			return nil
		}
		out = append(out, o)
		return nil
	})
	if walkErr != nil {
		return nil, report, walkErr
	}
	return out, report, nil
}
