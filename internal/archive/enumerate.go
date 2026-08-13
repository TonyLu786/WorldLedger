package archive

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/model"
)

type ChunkObservations struct {
	Chunk        model.ChunkRef      `json:"chunk"`
	Observations []model.Observation `json:"observations"`
}

func (a Archive) Servers() ([]string, error) {
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return nil, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()
	return readEncodedNames(filepath.Join(a.Root, "index", "chunks"))
}

func (a Archive) Dimensions(serverID string) ([]string, error) {
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return nil, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()
	return readEncodedNames(filepath.Join(a.Root, "index", "chunks", safe(serverID)))
}

func (a Archive) Chunks(serverID, dimension string) ([]model.ChunkRef, error) {
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return nil, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()
	return a.chunksLocked(serverID, dimension)
}

// DimensionObservations gathers every indexed chunk and its observations under a
// single archive lock, so a concurrent import cannot interleave a partially
// updated view into one snapshot.
func (a Archive) DimensionObservations(serverID, dimension string) ([]ChunkObservations, error) {
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return nil, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()

	chunks, err := a.chunksLocked(serverID, dimension)
	if err != nil {
		return nil, err
	}
	out := make([]ChunkObservations, 0, len(chunks))
	for _, chunk := range chunks {
		observations, err := a.observationsLocked(chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, ChunkObservations{Chunk: chunk, Observations: observations})
	}
	return out, nil
}

func (a Archive) chunksLocked(serverID, dimension string) ([]model.ChunkRef, error) {
	root := filepath.Join(a.Root, "index", "chunks", safe(serverID), safe(dimension))
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	normalizedServer := model.NormalizeToken(serverID)
	normalizedDimension := model.NormalizeToken(dimension)
	var chunks []model.ChunkRef
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("%s: unexpected file in chunk index", filepath.Join(root, entry.Name()))
		}
		x, err := strconv.ParseInt(entry.Name(), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid chunk x coordinate: %w", filepath.Join(root, entry.Name()), err)
		}
		column := filepath.Join(root, entry.Name())
		files, err := os.ReadDir(column)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".idx") {
				return nil, fmt.Errorf("%s: unexpected entry in chunk index", filepath.Join(column, file.Name()))
			}
			z, err := strconv.ParseInt(strings.TrimSuffix(file.Name(), ".idx"), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("%s: invalid chunk z coordinate: %w", filepath.Join(column, file.Name()), err)
			}
			chunks = append(chunks, model.ChunkRef{
				ServerID:  normalizedServer,
				Dimension: normalizedDimension,
				X:         int32(x),
				Z:         int32(z),
			})
		}
	}
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].X == chunks[j].X {
			return chunks[i].Z < chunks[j].Z
		}
		return chunks[i].X < chunks[j].X
	})
	return chunks, nil
}

func readEncodedNames(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("%s: unexpected file in chunk index", filepath.Join(root, entry.Name()))
		}
		decoded, err := base64.RawURLEncoding.DecodeString(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%s: undecodable index name: %w", filepath.Join(root, entry.Name()), err)
		}
		names = append(names, string(decoded))
	}
	sort.Strings(names)
	return names, nil
}
