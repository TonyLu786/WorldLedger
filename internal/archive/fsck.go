package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/model"
)

type CheckReport struct {
	Observations int      `json:"observations"`
	Objects      int      `json:"objects"`
	Errors       []string `json:"errors"`
}

func (a Archive) Check() CheckReport {
	report := CheckReport{Errors: []string{}}
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("lock archive: %v", err))
		return report
	}
	defer lock.Close()
	seenObjects := map[string]model.BlobRef{}
	observations := map[string]model.Observation{}
	root := filepath.Join(a.Root, "observations")

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		var o model.Observation
		if err := json.Unmarshal(data, &o); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: invalid json: %v", path, err))
			return nil
		}
		report.Observations++
		if err := o.ValidateStored(); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("observation %s: %v", o.ID, err))
			return nil
		}
		filenameID := strings.TrimSuffix(d.Name(), ".json")
		if filenameID != o.ID {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: filename id %s does not match observation id %s", path, filenameID, o.ID))
			return nil
		}
		if _, exists := observations[o.ID]; exists {
			report.Errors = append(report.Errors, fmt.Sprintf("observation %s is stored more than once", o.ID))
			return nil
		}
		observations[o.ID] = o
		for _, ref := range o.Components {
			if existing, exists := seenObjects[ref.Digest]; exists && existing != ref {
				report.Errors = append(report.Errors, fmt.Sprintf("object %s has conflicting references", ref.Digest))
				continue
			}
			seenObjects[ref.Digest] = ref
		}
		return nil
	})
	if walkErr != nil {
		report.Errors = append(report.Errors, walkErr.Error())
	}

	indexed := map[string]string{}
	indexRoot := filepath.Join(a.Root, "index", "chunks")
	indexWalkErr := filepath.WalkDir(indexRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".idx") {
			return nil
		}
		ids, err := readIndexIDs(path)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			return nil
		}
		for _, id := range ids {
			o, exists := observations[id]
			if !exists {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: index references missing observation %s", path, id))
				continue
			}
			wantPath := filepath.Clean(a.chunkIndexPath(o.Chunk))
			if filepath.Clean(path) != wantPath {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: observation %s belongs in %s", path, id, wantPath))
				continue
			}
			if previous, exists := indexed[id]; exists {
				report.Errors = append(report.Errors, fmt.Sprintf("observation %s is indexed more than once (%s and %s)", id, previous, path))
				continue
			}
			indexed[id] = path
		}
		return nil
	})
	if indexWalkErr != nil {
		report.Errors = append(report.Errors, indexWalkErr.Error())
	}
	for id := range observations {
		if _, exists := indexed[id]; !exists {
			report.Errors = append(report.Errors, fmt.Sprintf("observation %s is missing from its chunk index", id))
		}
	}

	transactionRoot := filepath.Join(a.Root, transactionDirectory)
	if entries, err := os.ReadDir(transactionRoot); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", transactionRoot, err))
	} else {
		for _, entry := range entries {
			if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".tmp-") {
				report.Errors = append(report.Errors, fmt.Sprintf("pending archive transaction %s", entry.Name()))
			}
		}
	}

	for digest, ref := range seenObjects {
		report.Objects++
		if err := a.CAS.Verify(ref); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("object %s: %v", digest, err))
		}
	}
	return report
}
