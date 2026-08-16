// Package landmark names places in an archive.
//
// A chunk coordinate is not how anybody thinks about where they have been.
// Coverage reports "157 chunks, x -3..3, z -3..3" where a person would say
// "spawn" or "the nether hub", and that gap is why an archive full of real
// exploration reads like a spreadsheet.
//
// A landmark is a declaration, not an observation, and the distinction is the
// whole reason this is a separate package rather than a field on a chunk. An
// observation is evidence: someone's client saw these bytes at this instant.
// A landmark is an assertion by a person that an area means something. Mixing
// the two would let an opinion be mistaken for a measurement, so landmarks are
// stored beside redactions and publication policies, attributed to whoever
// declared them, and never merged into the record of what was seen.
//
// Landmarks are local. Transfer bundles carry observations, and a name someone
// chose for a place is not one; two archives that merged everything they saw
// still each keep their own names for it.
package landmark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

const Schema = "worldledger.landmark/v1"

// MaxNameBytes bounds a name so a declaration cannot be used to store a payload
// in an archive that is otherwise all digests and coordinates.
const MaxNameBytes = 128

// Landmark is a named area of one dimension of one server.
type Landmark struct {
	Schema string `json:"schema"`
	// ID is derived from the server, dimension and name, so declaring the same
	// place twice moves it rather than leaving two landmarks with one name.
	ID        string            `json:"id"`
	Server    string            `json:"server"`
	Dimension string            `json:"dimension"`
	Name      string            `json:"name"`
	Bounds    model.ChunkBounds `json:"bounds"`
	Note      string            `json:"note,omitempty"`
	// DeclaredBy is who says so. A landmark with no author is an anonymous
	// assertion about somebody's world, which is not what this is for.
	DeclaredBy string    `json:"declared_by"`
	DeclaredAt time.Time `json:"declared_at"`
}

func (l Landmark) Validate() error {
	if model.NormalizeToken(l.Server) == "" {
		return errors.New("a landmark needs a server")
	}
	if model.NormalizeToken(l.Dimension) == "" {
		return errors.New("a landmark needs a dimension")
	}
	if strings.TrimSpace(l.Name) == "" {
		return errors.New("a landmark needs a name")
	}
	if len(l.Name) > MaxNameBytes {
		return fmt.Errorf("name exceeds %d bytes", MaxNameBytes)
	}
	if strings.TrimSpace(l.DeclaredBy) == "" {
		return errors.New("a landmark needs to say who declared it")
	}
	return l.Bounds.Validate()
}

// Contains reports whether a chunk falls inside this landmark. A chunk in
// another server or dimension never does, however the coordinates line up.
func (l Landmark) Contains(chunk model.ChunkRef) bool {
	if model.NormalizeToken(chunk.ServerID) != l.Server ||
		model.NormalizeToken(chunk.Dimension) != l.Dimension {
		return false
	}
	return l.Bounds.Contains(chunk.X, chunk.Z)
}

func (l Landmark) deriveID() string {
	var buffer bytes.Buffer
	buffer.WriteString(Schema)
	buffer.WriteByte(0)
	buffer.WriteString(l.Server)
	buffer.WriteByte(0)
	buffer.WriteString(l.Dimension)
	buffer.WriteByte(0)
	// The name decides identity, folded so that "Spawn" and "spawn" are one
	// place. The bounds do not: moving a landmark is editing it, not making a
	// second one with the same name.
	buffer.WriteString(strings.ToLower(strings.TrimSpace(l.Name)))
	sum := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(sum[:])
}

type Store struct {
	Root string
}

func NewStore(archiveRoot string) Store {
	return Store{Root: filepath.Join(archiveRoot, "landmarks")}
}

func (s Store) path(id string) string {
	return filepath.Join(s.Root, id+".json")
}

// Declare records a landmark, replacing any earlier one for the same place.
func (s Store) Declare(landmark Landmark) (Landmark, error) {
	landmark.Schema = Schema
	landmark.Server = model.NormalizeToken(landmark.Server)
	landmark.Dimension = model.NormalizeToken(landmark.Dimension)
	landmark.Name = strings.TrimSpace(landmark.Name)
	landmark.DeclaredBy = strings.TrimSpace(landmark.DeclaredBy)
	landmark.Note = strings.TrimSpace(landmark.Note)
	if landmark.DeclaredAt.IsZero() {
		landmark.DeclaredAt = time.Now().UTC()
	}
	landmark.DeclaredAt = landmark.DeclaredAt.UTC()
	landmark.ID = landmark.deriveID()
	if err := landmark.Validate(); err != nil {
		return Landmark{}, err
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return Landmark{}, err
	}
	encoded, err := json.MarshalIndent(landmark, "", " ")
	if err != nil {
		return Landmark{}, err
	}
	if err := os.WriteFile(s.path(landmark.ID), append(encoded, '\n'), 0o644); err != nil {
		return Landmark{}, err
	}
	return landmark, nil
}

// Remove deletes a landmark by id, reporting whether one was there. Removing a
// name that was never declared is not an error, so a script can be run twice.
func (s Store) Remove(id string) (bool, error) {
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Set is every landmark an archive holds, ordered by server, dimension, name so
// that two listings of the same archive read the same way.
type Set []Landmark

func (s Store) List() (Set, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var set Set
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Root, entry.Name()))
		if err != nil {
			return nil, err
		}
		var landmark Landmark
		if err := json.Unmarshal(data, &landmark); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if landmark.Schema != Schema {
			return nil, fmt.Errorf("%s: unsupported landmark schema %q", entry.Name(), landmark.Schema)
		}
		set = append(set, landmark)
	}
	sort.Slice(set, func(i, j int) bool {
		if set[i].Server != set[j].Server {
			return set[i].Server < set[j].Server
		}
		if set[i].Dimension != set[j].Dimension {
			return set[i].Dimension < set[j].Dimension
		}
		return strings.ToLower(set[i].Name) < strings.ToLower(set[j].Name)
	})
	return set, nil
}

// Find returns the landmark with this name in this server and dimension.
func (s Set) Find(server, dimension, name string) (Landmark, bool) {
	server = model.NormalizeToken(server)
	dimension = model.NormalizeToken(dimension)
	folded := strings.ToLower(strings.TrimSpace(name))
	for _, landmark := range s {
		if landmark.Server == server && landmark.Dimension == dimension &&
			strings.ToLower(landmark.Name) == folded {
			return landmark, true
		}
	}
	return Landmark{}, false
}

// Names lists the landmark names for one server and dimension, which is what an
// error message needs when somebody asks for one that is not there.
func (s Set) Names(server, dimension string) []string {
	server = model.NormalizeToken(server)
	dimension = model.NormalizeToken(dimension)
	var names []string
	for _, landmark := range s {
		if landmark.Server == server && landmark.Dimension == dimension {
			names = append(names, landmark.Name)
		}
	}
	return names
}

// Coverage is how much of a landmark an archive has observations for.
type Coverage struct {
	Landmark Landmark
	// Observed is how many chunks inside the landmark the archive has a reading
	// for at the moment being asked about.
	Observed int
	// Total is how many chunks the landmark covers.
	Total int
}

// Fraction is Observed over Total, or zero for a landmark covering nothing.
func (c Coverage) Fraction() float64 {
	if c.Total == 0 {
		return 0
	}
	return float64(c.Observed) / float64(c.Total)
}

// Complete reports whether every chunk in the landmark has been observed.
func (c Coverage) Complete() bool {
	return c.Total > 0 && c.Observed == c.Total
}
