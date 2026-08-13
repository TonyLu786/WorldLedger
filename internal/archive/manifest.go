package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

const ManifestSchema = "worldledger.archive-manifest/v1"

// Manifest describes an archive's contents well enough for a second party to
// tell whether they hold the same archive, and to find out what differs when
// they do not.
//
// It is derived, never authoritative: everything in it is recomputed from the
// observations on disk. A manifest that disagrees with the archive is wrong
// about the archive, not the other way round.
//
// It deliberately does not list every observation. A mirror needs to answer
// "do we agree, and where do we differ" without transferring the archive, so
// the manifest carries per-chunk digests over observation identities. Comparing
// two manifests localises a difference to a chunk; only then does anything need
// to be fetched.
type Manifest struct {
	Schema string `json:"schema"`
	// FormatVersion is the on-disk archive layout this was taken from.
	FormatVersion string    `json:"format_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Observations  int       `json:"observations"`
	Objects       int       `json:"objects"`
	ObjectBytes   int64     `json:"object_bytes"`
	// Root is a digest over every server entry, so two archives can be compared
	// with one value before looking closer.
	Root    string           `json:"root"`
	Servers []ServerManifest `json:"servers"`
}

type ServerManifest struct {
	Server     string              `json:"server"`
	Dimensions []DimensionManifest `json:"dimensions"`
	// Digest covers this server's dimensions.
	Digest string `json:"digest"`
}

type DimensionManifest struct {
	Dimension    string          `json:"dimension"`
	Chunks       int             `json:"chunks"`
	Observations int             `json:"observations"`
	Earliest     time.Time       `json:"earliest"`
	Latest       time.Time       `json:"latest"`
	Digest       string          `json:"digest"`
	ChunkDigests []ChunkManifest `json:"chunk_digests"`
}

type ChunkManifest struct {
	X int32 `json:"x"`
	Z int32 `json:"z"`
	// Digest covers the sorted observation identities for this chunk, so two
	// mirrors that hold the same observations agree regardless of import order.
	Digest       string `json:"digest"`
	Observations int    `json:"observations"`
}

// Manifest builds a manifest by walking the archive under one lock, so a
// concurrent import cannot produce a manifest describing a state that never
// existed.
func (a Archive) Manifest() (Manifest, error) {
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return Manifest{}, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()

	manifest := Manifest{
		Schema:        ManifestSchema,
		FormatVersion: FormatVersion,
		GeneratedAt:   time.Now().UTC(),
	}

	servers, err := readEncodedNames(filepath.Join(a.Root, "index", "chunks"))
	if err != nil {
		return Manifest{}, err
	}

	objects := map[string]int64{}
	for _, server := range servers {
		entry, err := a.serverManifest(server, objects)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Observations += observationCount(entry)
		manifest.Servers = append(manifest.Servers, entry)
	}

	manifest.Objects = len(objects)
	for _, size := range objects {
		manifest.ObjectBytes += size
	}
	manifest.Root = digestOf(func(h *bytes.Buffer) {
		for _, server := range manifest.Servers {
			h.WriteString(server.Server)
			h.WriteByte(0)
			h.WriteString(server.Digest)
			h.WriteByte(0)
		}
	})
	return manifest, nil
}

func (a Archive) serverManifest(server string, objects map[string]int64) (ServerManifest, error) {
	dimensions, err := readEncodedNames(filepath.Join(a.Root, "index", "chunks", safe(server)))
	if err != nil {
		return ServerManifest{}, err
	}

	entry := ServerManifest{Server: server}
	for _, dimension := range dimensions {
		chunks, err := a.chunksLocked(server, dimension)
		if err != nil {
			return ServerManifest{}, err
		}

		dimensionEntry := DimensionManifest{Dimension: dimension, Chunks: len(chunks)}
		for _, chunk := range chunks {
			observations, err := a.observationsLocked(chunk)
			if err != nil {
				return ServerManifest{}, err
			}
			if len(observations) == 0 {
				continue
			}

			ids := make([]string, 0, len(observations))
			for _, observation := range observations {
				ids = append(ids, observation.ID)
				for _, ref := range observation.Components {
					objects[ref.Digest] = ref.Size
				}
				if dimensionEntry.Earliest.IsZero() || observation.ObservedAt.Before(dimensionEntry.Earliest) {
					dimensionEntry.Earliest = observation.ObservedAt.UTC()
				}
				if observation.ObservedAt.After(dimensionEntry.Latest) {
					dimensionEntry.Latest = observation.ObservedAt.UTC()
				}
			}
			sort.Strings(ids)

			dimensionEntry.Observations += len(ids)
			dimensionEntry.ChunkDigests = append(dimensionEntry.ChunkDigests, ChunkManifest{
				X:            chunk.X,
				Z:            chunk.Z,
				Observations: len(ids),
				Digest: digestOf(func(h *bytes.Buffer) {
					for _, id := range ids {
						h.WriteString(id)
						h.WriteByte(0)
					}
				}),
			})
		}

		dimensionEntry.Digest = digestOf(func(h *bytes.Buffer) {
			for _, chunk := range dimensionEntry.ChunkDigests {
				fmt.Fprintf(h, "%d:%d:%s\x00", chunk.X, chunk.Z, chunk.Digest)
			}
		})
		entry.Dimensions = append(entry.Dimensions, dimensionEntry)
	}

	entry.Digest = digestOf(func(h *bytes.Buffer) {
		for _, dimension := range entry.Dimensions {
			h.WriteString(dimension.Dimension)
			h.WriteByte(0)
			h.WriteString(dimension.Digest)
			h.WriteByte(0)
		}
	})
	return entry, nil
}

func observationCount(entry ServerManifest) int {
	total := 0
	for _, dimension := range entry.Dimensions {
		total += dimension.Observations
	}
	return total
}

func digestOf(write func(*bytes.Buffer)) string {
	var buffer bytes.Buffer
	write(&buffer)
	sum := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(sum[:])
}

func (m Manifest) Save(path string) error {
	data, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	if manifest.Schema != ManifestSchema {
		return Manifest{}, fmt.Errorf("%s: unsupported manifest schema %q", path, manifest.Schema)
	}
	return manifest, nil
}

// Difference is one place two archives disagree.
type Difference struct {
	Server    string          `json:"server,omitempty"`
	Dimension string          `json:"dimension,omitempty"`
	Chunk     *model.ChunkRef `json:"chunk,omitempty"`
	Detail    string          `json:"detail"`
}

// Compare localises where two manifests disagree, without needing either
// archive. An empty result means the two hold the same observations.
func Compare(local, remote Manifest) []Difference {
	if local.Root == remote.Root {
		return nil
	}

	var differences []Difference
	localServers := indexServers(local)
	remoteServers := indexServers(remote)

	for _, name := range unionKeys(localServers, remoteServers) {
		mine, hasMine := localServers[name]
		theirs, hasTheirs := remoteServers[name]
		switch {
		case !hasTheirs:
			differences = append(differences, Difference{Server: name, Detail: "only in the local archive"})
			continue
		case !hasMine:
			differences = append(differences, Difference{Server: name, Detail: "only in the remote archive"})
			continue
		case mine.Digest == theirs.Digest:
			continue
		}
		differences = append(differences, compareDimensions(name, mine, theirs)...)
	}
	return differences
}

func compareDimensions(server string, local, remote ServerManifest) []Difference {
	var differences []Difference
	localDimensions := indexDimensions(local)
	remoteDimensions := indexDimensions(remote)

	for _, name := range unionKeys(localDimensions, remoteDimensions) {
		mine, hasMine := localDimensions[name]
		theirs, hasTheirs := remoteDimensions[name]
		switch {
		case !hasTheirs:
			differences = append(differences, Difference{Server: server, Dimension: name, Detail: "only in the local archive"})
			continue
		case !hasMine:
			differences = append(differences, Difference{Server: server, Dimension: name, Detail: "only in the remote archive"})
			continue
		case mine.Digest == theirs.Digest:
			continue
		}
		differences = append(differences, compareChunks(server, name, mine, theirs)...)
	}
	return differences
}

func compareChunks(server, dimension string, local, remote DimensionManifest) []Difference {
	type key struct{ x, z int32 }
	localChunks := map[key]ChunkManifest{}
	remoteChunks := map[key]ChunkManifest{}
	for _, chunk := range local.ChunkDigests {
		localChunks[key{chunk.X, chunk.Z}] = chunk
	}
	for _, chunk := range remote.ChunkDigests {
		remoteChunks[key{chunk.X, chunk.Z}] = chunk
	}

	seen := map[key]struct{}{}
	ordered := make([]key, 0, len(localChunks)+len(remoteChunks))
	for _, source := range []map[key]ChunkManifest{localChunks, remoteChunks} {
		for k := range source {
			if _, exists := seen[k]; !exists {
				seen[k] = struct{}{}
				ordered = append(ordered, k)
			}
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].x == ordered[j].x {
			return ordered[i].z < ordered[j].z
		}
		return ordered[i].x < ordered[j].x
	})

	var differences []Difference
	for _, k := range ordered {
		mine, hasMine := localChunks[k]
		theirs, hasTheirs := remoteChunks[k]
		chunk := model.ChunkRef{ServerID: server, Dimension: dimension, X: k.x, Z: k.z}
		switch {
		case !hasTheirs:
			differences = append(differences, Difference{Server: server, Dimension: dimension, Chunk: &chunk, Detail: "only in the local archive"})
		case !hasMine:
			differences = append(differences, Difference{Server: server, Dimension: dimension, Chunk: &chunk, Detail: "only in the remote archive"})
		case mine.Digest != theirs.Digest:
			differences = append(differences, Difference{
				Server: server, Dimension: dimension, Chunk: &chunk,
				Detail: fmt.Sprintf("different observations (local %d, remote %d)", mine.Observations, theirs.Observations),
			})
		}
	}
	return differences
}

func indexServers(m Manifest) map[string]ServerManifest {
	out := make(map[string]ServerManifest, len(m.Servers))
	for _, server := range m.Servers {
		out[server.Server] = server
	}
	return out
}

func indexDimensions(m ServerManifest) map[string]DimensionManifest {
	out := make(map[string]DimensionManifest, len(m.Dimensions))
	for _, dimension := range m.Dimensions {
		out[dimension.Dimension] = dimension
	}
	return out
}

func unionKeys[T any](a, b map[string]T) []string {
	seen := map[string]struct{}{}
	for key := range a {
		seen[key] = struct{}{}
	}
	for key := range b {
		seen[key] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
