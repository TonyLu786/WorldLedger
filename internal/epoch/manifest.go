package epoch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

const ManifestSchema = "worldledger.archive-epoch/v1"

// Manifest is what an archive says the world was at one moment, in a form two
// people can compare without either sending the other a world.
//
// An exported world carries no record of where it came from. Two contributors
// can export the same server at the same instant from archives holding
// different observations, get different worlds, and have no way to find that
// out short of comparing region files. This is the missing document: small,
// canonical, and carrying a root digest, so the question is one value.
//
// What the root covers is the whole design. It digests the chunk positions and
// the state selected at each of them, and nothing else. It deliberately leaves
// out:
//
//   - Contributors. Two archives that selected the same state through different
//     people produce the same world. Digesting who provided it would report
//     agreement as disagreement.
//   - Status. An archive holding one observation of a chunk calls it
//     single-source where one holding two agreeing calls it corroborated, and
//     both export the same blocks. Confidence is worth reporting and is not what
//     makes a world.
//   - When the manifest was generated, which is not a fact about the world.
//
// So two manifests with the same root describe the same world, and Compare
// reports the confidence difference separately for anyone who wants it.
type Manifest struct {
	Schema      string    `json:"schema"`
	Server      string    `json:"server"`
	Dimension   string    `json:"dimension"`
	At          time.Time `json:"at"`
	Policy      Policy    `json:"policy"`
	GeneratedAt time.Time `json:"generated_at"`
	Summary     Summary   `json:"summary"`
	Chunks      []Chunk   `json:"chunks"`
	Root        string    `json:"root"`
}

// Chunk is one position and what was chosen there.
type Chunk struct {
	X int32 `json:"x"`
	Z int32 `json:"z"`
	// StateDigest is empty when nothing was observed at or before the instant.
	// An absent state is a real answer and is digested as one, so an archive
	// that has never seen a chunk cannot match one that has.
	StateDigest string `json:"state_digest,omitempty"`
	Status      Status `json:"status"`
	// Contributors is reported, never digested.
	Contributors []string `json:"contributors,omitempty"`
}

// BuildManifest describes a snapshot. Chunks are ordered by position so that
// two archives which imported in different orders produce identical bytes.
func BuildManifest(snapshot Snapshot) Manifest {
	manifest := Manifest{
		Schema:      ManifestSchema,
		Server:      snapshot.Server,
		Dimension:   snapshot.Dimension,
		At:          snapshot.At.UTC(),
		Policy:      snapshot.Policy,
		GeneratedAt: time.Now().UTC(),
		Summary:     snapshot.Summary,
		Chunks:      make([]Chunk, 0, len(snapshot.Selections)),
	}
	for _, selection := range snapshot.Selections {
		chunk := Chunk{
			X:            selection.Chunk.X,
			Z:            selection.Chunk.Z,
			Status:       selection.Status,
			Contributors: selection.Contributors,
		}
		if selection.Known() {
			chunk.StateDigest = selection.Selected.StateDigest
		}
		manifest.Chunks = append(manifest.Chunks, chunk)
	}
	sort.Slice(manifest.Chunks, func(i, j int) bool {
		if manifest.Chunks[i].X != manifest.Chunks[j].X {
			return manifest.Chunks[i].X < manifest.Chunks[j].X
		}
		return manifest.Chunks[i].Z < manifest.Chunks[j].Z
	})
	manifest.Root = manifest.computeRoot()
	return manifest
}

func (m Manifest) computeRoot() string {
	var buffer bytes.Buffer
	// The instant and the policy are part of what is being described: the same
	// archive read at a different moment, or under a different policy, is a
	// different world and must not compare equal.
	fmt.Fprintf(&buffer, "%s\x00%s\x00%s\x00%s\x00",
		m.Schema, m.Server, m.Dimension, m.At.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&buffer, "%s\x00", m.Policy)
	for _, chunk := range m.Chunks {
		fmt.Fprintf(&buffer, "%d\x00%d\x00%s\x00", chunk.X, chunk.Z, chunk.StateDigest)
	}
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
		return Manifest{}, fmt.Errorf("%s: unsupported epoch manifest schema %q", path, manifest.Schema)
	}
	// The root is recomputed rather than trusted. A file whose recorded root
	// does not match its own entries has been edited or truncated, and comparing
	// against it would compare against something that never existed.
	if recomputed := manifest.computeRoot(); recomputed != manifest.Root {
		return Manifest{}, fmt.Errorf(
			"%s: recorded root %s does not match its entries, which digest to %s",
			path, manifest.Root, recomputed)
	}
	return manifest, nil
}

// ManifestComparison is how two readings of one moment differ.
type ManifestComparison struct {
	SameWorld bool
	// Mismatched are chunks whose selected state differs, which is what makes
	// two exports different worlds.
	Mismatched []ChunkDifference
	// OnlyLocal and OnlyRemote are chunks one manifest has entries for and the
	// other does not.
	OnlyLocal  []Chunk
	OnlyRemote []Chunk
	// Confidence lists chunks that agree on the state but not on how well it is
	// attested. These do not change the exported world.
	Confidence []ChunkDifference
}

type ChunkDifference struct {
	X, Z   int32
	Local  Chunk
	Remote Chunk
}

// CompareManifests reports where two readings of the same moment differ.
//
// It refuses two manifests describing different servers, dimensions or
// instants, because a difference between those is not a disagreement about the
// world; it is a comparison nobody meant to make.
func CompareManifests(local, remote Manifest) (ManifestComparison, error) {
	if local.Server != remote.Server || local.Dimension != remote.Dimension {
		return ManifestComparison{}, fmt.Errorf(
			"these describe different places: %s %s and %s %s",
			local.Server, local.Dimension, remote.Server, remote.Dimension)
	}
	if !local.At.Equal(remote.At) {
		return ManifestComparison{}, fmt.Errorf(
			"these describe different moments: %s and %s",
			local.At.Format(time.RFC3339Nano), remote.At.Format(time.RFC3339Nano))
	}

	comparison := ManifestComparison{SameWorld: local.Root == remote.Root}
	remoteByPosition := make(map[[2]int32]Chunk, len(remote.Chunks))
	for _, chunk := range remote.Chunks {
		remoteByPosition[[2]int32{chunk.X, chunk.Z}] = chunk
	}

	for _, mine := range local.Chunks {
		position := [2]int32{mine.X, mine.Z}
		theirs, ok := remoteByPosition[position]
		if !ok {
			comparison.OnlyLocal = append(comparison.OnlyLocal, mine)
			continue
		}
		delete(remoteByPosition, position)
		difference := ChunkDifference{X: mine.X, Z: mine.Z, Local: mine, Remote: theirs}
		switch {
		case mine.StateDigest != theirs.StateDigest:
			comparison.Mismatched = append(comparison.Mismatched, difference)
		case mine.Status != theirs.Status:
			comparison.Confidence = append(comparison.Confidence, difference)
		}
	}
	for _, theirs := range remoteByPosition {
		comparison.OnlyRemote = append(comparison.OnlyRemote, theirs)
	}

	sortChunks(comparison.OnlyLocal)
	sortChunks(comparison.OnlyRemote)
	sortDifferences(comparison.Mismatched)
	sortDifferences(comparison.Confidence)
	return comparison, nil
}

func sortChunks(chunks []Chunk) {
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].X != chunks[j].X {
			return chunks[i].X < chunks[j].X
		}
		return chunks[i].Z < chunks[j].Z
	})
}

func sortDifferences(differences []ChunkDifference) {
	sort.Slice(differences, func(i, j int) bool {
		if differences[i].X != differences[j].X {
			return differences[i].X < differences[j].X
		}
		return differences[i].Z < differences[j].Z
	})
}
