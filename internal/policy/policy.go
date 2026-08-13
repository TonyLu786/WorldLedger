// Package policy records what an operator has decided about publishing a
// server's observations, and measures how much the archive has accumulated
// about that server.
//
// The decision this supports is not "may I read my own archive". Local use is
// not the risk and locking local files would be theatre. The risk is
// distribution: handing the objects, or a world exported from them, to someone
// else. What this package enforces is that the decision is made explicitly,
// once per server, by a named person, before the archive is used to produce
// anything shareable.
//
// See docs/trust-model.md for why aggregation rather than any single
// observation is what creates the exposure.
package policy

import (
	"bytes"
	"encoding/base64"
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

const Schema = "worldledger.publication-policy/v1"

// Disposition is what the operator declared about distributing this server's
// observations.
type Disposition string

const (
	// Private means the observations are not to be distributed at all.
	Private Disposition = "private"
	// Embargoed means not before EmbargoUntil.
	Embargoed Disposition = "embargoed"
	// Research means distribution to named collaborators, not publication.
	Research Disposition = "research"
	// Public means the operator asserts the server's community accepted
	// publication.
	Public Disposition = "public"
)

func ParseDisposition(value string) (Disposition, error) {
	switch Disposition(strings.TrimSpace(strings.ToLower(value))) {
	case Private:
		return Private, nil
	case Embargoed:
		return Embargoed, nil
	case Research:
		return Research, nil
	case Public:
		return Public, nil
	}
	return "", fmt.Errorf("unknown disposition %q; want private, embargoed, research, or public", value)
}

// ServerPolicy is one server's declaration. It carries who declared it, because
// a policy nobody signed records nothing.
type ServerPolicy struct {
	Schema       string      `json:"schema"`
	Server       string      `json:"server"`
	Disposition  Disposition `json:"disposition"`
	EmbargoUntil *time.Time  `json:"embargo_until,omitempty"`
	DeclaredBy   string      `json:"declared_by"`
	DeclaredAt   time.Time   `json:"declared_at"`
	Note         string      `json:"note,omitempty"`
}

func (p ServerPolicy) Validate() error {
	if p.Schema != Schema {
		return fmt.Errorf("unsupported policy schema %q", p.Schema)
	}
	if model.NormalizeToken(p.Server) == "" {
		return errors.New("server is required")
	}
	if _, err := ParseDisposition(string(p.Disposition)); err != nil {
		return err
	}
	if strings.TrimSpace(p.DeclaredBy) == "" {
		return errors.New("a policy must name who declared it")
	}
	if p.Disposition == Embargoed && p.EmbargoUntil == nil {
		return errors.New("an embargo must state when it ends")
	}
	if p.Disposition != Embargoed && p.EmbargoUntil != nil {
		return errors.New("only an embargo carries an end date")
	}
	if p.DeclaredAt.IsZero() {
		return errors.New("declared_at is required")
	}
	return nil
}

// DistributionAllowed reports whether the declaration permits handing this
// server's data to anyone else at the given moment.
func (p ServerPolicy) DistributionAllowed(now time.Time) (bool, string) {
	switch p.Disposition {
	case Public:
		return true, "the operator declared this server public"
	case Research:
		return false, "declared research-only: share with named collaborators, not publicly"
	case Embargoed:
		if p.EmbargoUntil != nil && now.After(*p.EmbargoUntil) {
			return true, fmt.Sprintf("the embargo ended %s", p.EmbargoUntil.UTC().Format(time.RFC3339))
		}
		return false, fmt.Sprintf("embargoed until %s", p.EmbargoUntil.UTC().Format(time.RFC3339))
	default:
		return false, "declared private"
	}
}

// Store keeps policies inside an archive.
type Store struct {
	Root string
}

func NewStore(archiveRoot string) Store {
	return Store{Root: filepath.Join(archiveRoot, "policy", "servers")}
}

func (s Store) path(server string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(model.NormalizeToken(server)))
	return filepath.Join(s.Root, encoded+".json")
}

// Lookup returns the policy for a server, or false when none was ever declared.
// An absent policy is deliberately not a default: the caller must treat it as an
// unanswered question rather than as permission or as refusal.
func (s Store) Lookup(server string) (ServerPolicy, bool, error) {
	data, err := os.ReadFile(s.path(server))
	if os.IsNotExist(err) {
		return ServerPolicy{}, false, nil
	}
	if err != nil {
		return ServerPolicy{}, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var declared ServerPolicy
	if err := decoder.Decode(&declared); err != nil {
		return ServerPolicy{}, false, err
	}
	if err := declared.Validate(); err != nil {
		return ServerPolicy{}, false, err
	}
	return declared, true, nil
}

func (s Store) Declare(declared ServerPolicy) error {
	declared.Schema = Schema
	declared.Server = model.NormalizeToken(declared.Server)
	if declared.DeclaredAt.IsZero() {
		declared.DeclaredAt = time.Now().UTC()
	}
	declared.DeclaredAt = declared.DeclaredAt.UTC()
	if err := declared.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(declared, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(declared.Server), append(encoded, '\n'), 0o644)
}

func (s Store) List() ([]ServerPolicy, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ServerPolicy, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, fmt.Errorf("%s: undecodable policy name: %w", entry.Name(), err)
		}
		declared, found, err := s.Lookup(string(decoded))
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, declared)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Server < out[j].Server })
	return out, nil
}
