// Package redact records that specific observations must be withheld, and
// works out what withdrawing them can and cannot actually remove.
//
// Two requests motivate this. A contributor withdraws consent and wants their
// observations out. A server operator asks for one area to be excluded, whoever
// observed it. Both are legitimate, and neither is served by pretending an
// archive can forget on request.
//
// Content addressing is what makes the second half necessary. Objects are
// stored by digest, so two contributors who observed the same chunk in the same
// state reference one object. In the first real archive measured here, two
// contributors held 50 of the 52 distinct components between them. Removing one
// contributor's records is straightforward; removing their bytes is usually
// impossible, because those bytes are not uniquely theirs and another
// observation still depends on them.
//
// A redaction is therefore a declaration, like a publication policy: attributed,
// dated, and reversible. Purging is a separate, explicit step that reports what
// it could not remove and why.
package redact

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

const Schema = "worldledger.redaction/v1"

// Region is an inclusive rectangle of chunk coordinates.
// Redaction withholds the observations it matches from anything the archive
// builds for sharing.
//
// An empty Contributor matches every contributor, and a nil Region matches every
// chunk. Both empty means the whole server, which is a real request and stays
// expressible rather than being blocked by a rule that would only invite a
// workaround.
type Redaction struct {
	Schema string `json:"schema"`
	// ID is derived from the scope, so declaring the same scope twice replaces
	// the record instead of accumulating duplicates that would each have to be
	// withdrawn separately.
	ID          string             `json:"id"`
	Server      string             `json:"server"`
	Contributor string             `json:"contributor,omitempty"`
	Dimension   string             `json:"dimension,omitempty"`
	Region      *model.ChunkBounds `json:"region,omitempty"`
	Reason      string             `json:"reason"`
	DeclaredBy  string             `json:"declared_by"`
	DeclaredAt  time.Time          `json:"declared_at"`
}

func (r Redaction) Validate() error {
	if r.Schema != Schema {
		return fmt.Errorf("unsupported redaction schema %q", r.Schema)
	}
	if model.NormalizeToken(r.Server) == "" {
		return errors.New("server is required")
	}
	if strings.TrimSpace(r.DeclaredBy) == "" {
		return errors.New("a redaction must name who declared it")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return errors.New("a redaction must state a reason; an unexplained one cannot be reviewed later")
	}
	if r.DeclaredAt.IsZero() {
		return errors.New("declared_at is required")
	}
	if r.Region != nil {
		if err := r.Region.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Matches reports whether this redaction covers an observation.
//
// Contributor comparison ignores case and surrounding space. Withholding data
// is a consent decision, so failing to match a label that differs only in
// capitalisation would leave behind exactly what someone asked to have removed.
// Matching too much withholds data that could have been shared, which is the
// error worth preferring here.
func (r Redaction) Matches(observation model.Observation) bool {
	if model.NormalizeToken(r.Server) != model.NormalizeToken(observation.Chunk.ServerID) {
		return false
	}
	if r.Contributor != "" && !strings.EqualFold(strings.TrimSpace(r.Contributor), strings.TrimSpace(observation.Source.Contributor)) {
		return false
	}
	if r.Dimension != "" && model.NormalizeToken(r.Dimension) != model.NormalizeToken(observation.Chunk.Dimension) {
		return false
	}
	if r.Region != nil && !r.Region.Contains(observation.Chunk.X, observation.Chunk.Z) {
		return false
	}
	return true
}

// Describe renders the scope for an operator about to act on it.
func (r Redaction) Describe() string {
	parts := []string{"server " + r.Server}
	if r.Contributor != "" {
		parts = append(parts, "contributor "+r.Contributor)
	}
	if r.Dimension != "" {
		parts = append(parts, r.Dimension)
	}
	if r.Region != nil {
		parts = append(parts, r.Region.String())
	}
	if len(parts) == 1 {
		return parts[0] + " (every contributor, every chunk)"
	}
	return strings.Join(parts, ", ")
}

func (r Redaction) deriveID() string {
	var buffer bytes.Buffer
	buffer.WriteString(model.NormalizeToken(r.Server))
	buffer.WriteByte(0)
	buffer.WriteString(strings.ToLower(strings.TrimSpace(r.Contributor)))
	buffer.WriteByte(0)
	buffer.WriteString(model.NormalizeToken(r.Dimension))
	buffer.WriteByte(0)
	if r.Region != nil {
		fmt.Fprintf(&buffer, "%d:%d:%d:%d", r.Region.MinX, r.Region.MinZ, r.Region.MaxX, r.Region.MaxZ)
	}
	sum := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(sum[:])
}

// Set is every redaction declared for an archive.
type Set []Redaction

// Match returns the first redaction covering an observation.
func (s Set) Match(observation model.Observation) (Redaction, bool) {
	for _, redaction := range s {
		if redaction.Matches(observation) {
			return redaction, true
		}
	}
	return Redaction{}, false
}

// Filter splits observations into those that may be used and those withheld.
func (s Set) Filter(observations []model.Observation) (kept, withheld []model.Observation) {
	if len(s) == 0 {
		return observations, nil
	}
	for _, observation := range observations {
		if _, matched := s.Match(observation); matched {
			withheld = append(withheld, observation)
			continue
		}
		kept = append(kept, observation)
	}
	return kept, withheld
}

// Store keeps redactions inside an archive, beside the publication policy.
type Store struct {
	Root string
}

func NewStore(archiveRoot string) Store {
	return Store{Root: filepath.Join(archiveRoot, "policy", "redactions")}
}

func (s Store) path(id string) string {
	return filepath.Join(s.Root, id+".json")
}

func (s Store) Declare(redaction Redaction) (Redaction, error) {
	redaction.Schema = Schema
	redaction.Server = model.NormalizeToken(redaction.Server)
	redaction.Contributor = strings.TrimSpace(redaction.Contributor)
	redaction.Dimension = model.NormalizeToken(redaction.Dimension)
	if redaction.DeclaredAt.IsZero() {
		redaction.DeclaredAt = time.Now().UTC()
	}
	redaction.DeclaredAt = redaction.DeclaredAt.UTC()
	redaction.ID = redaction.deriveID()
	if err := redaction.Validate(); err != nil {
		return Redaction{}, err
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return Redaction{}, err
	}
	encoded, err := json.MarshalIndent(redaction, "", " ")
	if err != nil {
		return Redaction{}, err
	}
	if err := os.WriteFile(s.path(redaction.ID), append(encoded, '\n'), 0o644); err != nil {
		return Redaction{}, err
	}
	return redaction, nil
}

// Withdraw removes a declaration. A redaction is a decision, and decisions are
// sometimes made in error or superseded by consent given later.
func (s Store) Withdraw(id string) (bool, error) {
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s Store) List() (Set, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(Set, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Root, entry.Name()))
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var redaction Redaction
		if err := decoder.Decode(&redaction); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := redaction.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		out = append(out, redaction)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Server != out[j].Server {
			return out[i].Server < out[j].Server
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
