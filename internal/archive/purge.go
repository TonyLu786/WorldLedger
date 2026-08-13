package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/model"
)

const purgeDirectory = "purge"

// RetainedObject is an object a purge could not remove because a surviving
// observation still references it.
//
// This is the normal outcome rather than an edge case. Objects are addressed by
// content, so two contributors who saw the same chunk in the same state share
// one object, and removing one contributor's records cannot remove bytes the
// other independently observed. Reporting this is the difference between
// withdrawing someone's records and telling them their data is gone.
type RetainedObject struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	// Contributors are the surviving observers that still reference it.
	Contributors []string `json:"contributors"`
}

type PurgeResult struct {
	ObservationsRemoved int              `json:"observations_removed"`
	ObjectsRemoved      int              `json:"objects_removed"`
	BytesRemoved        int64            `json:"bytes_removed"`
	ObjectsRetained     []RetainedObject `json:"objects_retained"`
}

// RemoveObservations deletes observations and any object no surviving
// observation still references.
//
// The removals are journalled first. An interrupted purge leaves an archive
// that would otherwise fail its own integrity check, with an observation
// missing from its index or an index entry pointing at nothing, so the journal
// is replayed when the archive is next opened. Every step is idempotent, which
// is what makes replaying safe.
func (a Archive) RemoveObservations(ids []string) (PurgeResult, error) {
	if len(ids) == 0 {
		return PurgeResult{}, nil
	}
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()

	journal, err := a.writePurgeJournal(ids)
	if err != nil {
		return PurgeResult{}, err
	}
	result, err := a.applyPurgeLocked(ids)
	if err != nil {
		return PurgeResult{}, err
	}
	if err := os.Remove(journal); err != nil && !os.IsNotExist(err) {
		return PurgeResult{}, fmt.Errorf("remove purge journal: %w", err)
	}
	if err := syncDirectory(filepath.Dir(journal)); err != nil {
		return PurgeResult{}, err
	}
	return result, nil
}

func (a Archive) writePurgeJournal(ids []string) (string, error) {
	dir := filepath.Join(a.Root, purgeDirectory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	data, err := json.MarshalIndent(sorted, "", " ")
	if err != nil {
		return "", err
	}
	// One journal at a time. The archive lock already serialises purges, and a
	// fixed name means a crashed purge is found rather than accumulating.
	path := filepath.Join(dir, "pending.json")
	if err := writeAtomic(path, append(data, '\n')); err != nil {
		return "", fmt.Errorf("write purge journal: %w", err)
	}
	return path, nil
}

func (a Archive) applyPurgeLocked(ids []string) (PurgeResult, error) {
	removing := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		removing[id] = struct{}{}
	}

	// Gather what is being removed before anything is deleted, so the objects
	// each one referenced are still known afterwards.
	doomedRefs := map[string]model.BlobRef{}
	var result PurgeResult
	for _, id := range ids {
		observation, err := a.readObservation(id)
		if os.IsNotExist(err) {
			// Already gone, which happens when a journal is replayed.
			continue
		}
		if err != nil {
			return PurgeResult{}, fmt.Errorf("read observation %s: %w", id, err)
		}
		for _, ref := range observation.Components {
			doomedRefs[ref.Digest] = ref
		}
		if err := a.removeFromIndexLocked(observation); err != nil {
			return PurgeResult{}, err
		}
		if err := os.Remove(a.observationPath(id)); err != nil && !os.IsNotExist(err) {
			return PurgeResult{}, fmt.Errorf("remove observation %s: %w", id, err)
		}
		result.ObservationsRemoved++
	}

	if len(doomedRefs) == 0 {
		return result, nil
	}

	survivors, err := a.referencesByDigestLocked(doomedRefs)
	if err != nil {
		return PurgeResult{}, err
	}

	digests := make([]string, 0, len(doomedRefs))
	for digest := range doomedRefs {
		digests = append(digests, digest)
	}
	sort.Strings(digests)

	for _, digest := range digests {
		ref := doomedRefs[digest]
		if contributors := survivors[digest]; len(contributors) > 0 {
			sort.Strings(contributors)
			result.ObjectsRetained = append(result.ObjectsRetained, RetainedObject{
				Digest: digest, Size: ref.Size, Contributors: contributors,
			})
			continue
		}
		removed, err := a.CAS.Remove(ref)
		if err != nil {
			return PurgeResult{}, fmt.Errorf("remove object %s: %w", digest, err)
		}
		if removed {
			result.ObjectsRemoved++
			result.BytesRemoved += ref.Size
		}
	}
	return result, nil
}

// referencesByDigestLocked walks the surviving observations and reports, for
// each digest of interest, the distinct contributors that still reference it.
func (a Archive) referencesByDigestLocked(interesting map[string]model.BlobRef) (map[string][]string, error) {
	out := map[string][]string{}
	seen := map[string]map[string]struct{}{}

	root := filepath.Join(a.Root, "observations")
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var observation model.Observation
		if err := json.Unmarshal(data, &observation); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, ref := range observation.Components {
			if _, wanted := interesting[ref.Digest]; !wanted {
				continue
			}
			contributor := observation.Source.Contributor
			if contributor == "" {
				contributor = "(unnamed)"
			}
			if seen[ref.Digest] == nil {
				seen[ref.Digest] = map[string]struct{}{}
			}
			if _, exists := seen[ref.Digest][contributor]; exists {
				continue
			}
			seen[ref.Digest][contributor] = struct{}{}
			out[ref.Digest] = append(out[ref.Digest], contributor)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

func (a Archive) removeFromIndexLocked(observation model.Observation) error {
	indexPath := a.chunkIndexPath(observation.Chunk)
	ids, err := readIndexIDs(indexPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	remaining := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != observation.ID {
			remaining = append(remaining, id)
		}
	}
	if len(remaining) == len(ids) {
		return nil
	}
	if len(remaining) == 0 {
		// An empty index file would be listed as a chunk the archive knows
		// about while holding nothing, which is exactly the confusion between
		// unobserved and observed-empty the archive exists to avoid.
		if err := os.Remove(indexPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty chunk index: %w", err)
		}
		return syncDirectory(filepath.Dir(indexPath))
	}
	if err := writeAtomic(indexPath, []byte(strings.Join(remaining, "\n")+"\n")); err != nil {
		return fmt.Errorf("rewrite chunk index: %w", err)
	}
	return nil
}

func (a Archive) observationPath(id string) string {
	return filepath.Join(a.Root, "observations", id[:2], id+".json")
}

// recoverPurges finishes a purge that was interrupted. Without it the archive
// could be opened in a state its own integrity check rejects.
func (a Archive) recoverPurges() error {
	path := filepath.Join(a.Root, purgeDirectory, "pending.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return fmt.Errorf("%s: invalid purge journal: %w", path, err)
	}
	if _, err := a.applyPurgeLocked(ids); err != nil {
		return fmt.Errorf("replay purge journal: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
