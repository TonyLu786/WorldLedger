// Package transfer moves observations between archives as an immutable
// directory, with no service in between.
//
// Negotiation works out what a mirror lacks. This is what carries it. The
// bundle is a plain directory: it can be copied, mailed, mirrored, or served
// as static files, and importing it verifies every byte against the digest the
// bundle declares rather than trusting where it came from.
//
// Nothing here is a network protocol. That is the point: two operators can
// exchange and merge archives with a USB stick, and whatever protocol arrives
// later is an optimisation of the transport rather than a prerequisite for the
// exchange working at all.
package transfer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/model"
)

const Schema = "worldledger.transfer-bundle/v1"

// Manifest describes a bundle's contents so a receiver knows what it is about
// to verify before reading any of it.
type Manifest struct {
	Schema       string      `json:"schema"`
	CreatedAt    time.Time   `json:"created_at"`
	Observations []string    `json:"observations"`
	Objects      []ObjectRef `json:"objects"`
}

type ObjectRef struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// Sent reports what a send produced.
type Sent struct {
	Observations int
	Objects      int
	Bytes        int64
}

// Send writes a transfer bundle carrying the objects and observation records a
// peer lacks.
//
// The two are negotiated by different means because they answer different
// questions. A fingerprint is content only, by design, so that two platforms
// can compare captures at all; it says exactly which objects are missing and
// nothing about which records a peer holds. A manifest digests observation
// identities per chunk, so it says which chunks the two sides disagree about.
//
// Pass the peer's manifest when there is one. Without it every record is
// included, which is correct but can be wasteful: on an archive where
// deduplication was extreme, 158 records outweighed the 8 KiB of objects
// actually missing. Sending only the records that reference a missing object
// would be smaller still and wrong, because it leaves two mirrors agreeing on
// every byte and disagreeing about who observed what.
func Send(a archive.Archive, peer archive.Fingerprint, peerManifest *archive.Manifest, out string) (Sent, error) {
	local, err := a.Fingerprint("")
	if err != nil {
		return Sent{}, err
	}
	negotiation := archive.Negotiate(local, peer)

	observations, err := allObservations(a)
	if err != nil {
		return Sent{}, err
	}

	wantedChunks, filterChunks := chunksTheyDisagreeAbout(a, peerManifest)
	selected := observations[:0:0]
	for _, observation := range observations {
		if filterChunks {
			if _, differs := wantedChunks[observation.Chunk]; !differs {
				continue
			}
		}
		selected = append(selected, observation)
	}

	if len(negotiation.Offer) == 0 && len(selected) == 0 {
		return Sent{}, nil
	}

	offered := make(map[string]archive.FingerprintComponent, len(negotiation.Offer))
	for _, component := range negotiation.Offer {
		offered[component.Digest] = component
	}

	if err := os.MkdirAll(filepath.Join(out, "observations"), 0o755); err != nil {
		return Sent{}, err
	}

	manifest := Manifest{Schema: Schema, CreatedAt: time.Now().UTC()}
	var sent Sent

	for _, observation := range selected {
		encoded, err := json.MarshalIndent(observation, "", "  ")
		if err != nil {
			return Sent{}, err
		}
		path := filepath.Join(out, "observations", observation.ID+".json")
		if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
			return Sent{}, err
		}
		manifest.Observations = append(manifest.Observations, observation.ID)
		sent.Observations++
	}

	digests := make([]string, 0, len(offered))
	for digest := range offered {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	for _, digest := range digests {
		component := offered[digest]
		ref := model.BlobRef{Algorithm: "sha256", Digest: digest, Size: component.Size}
		if err := copyObject(a, ref, out); err != nil {
			return Sent{}, err
		}
		manifest.Objects = append(manifest.Objects, ObjectRef{Digest: digest, Size: component.Size})
		sent.Objects++
		sent.Bytes += component.Size
	}

	sort.Strings(manifest.Observations)
	encoded, err := json.MarshalIndent(manifest, "", " ")
	if err != nil {
		return Sent{}, err
	}
	if err := os.WriteFile(filepath.Join(out, "bundle.json"), append(encoded, '\n'), 0o644); err != nil {
		return Sent{}, err
	}
	return sent, nil
}

// chunksTheyDisagreeAbout compares this archive's manifest against the peer's
// and reports the chunks whose observation sets differ.
//
// A manifest digest covers the sorted observation ids for a chunk, so a
// mismatch says the two sides disagree without saying which record is missing.
// Sending every record for those chunks is the smallest safe answer to that.
// Returning false means no manifest was supplied and nothing should be filtered.
func chunksTheyDisagreeAbout(a archive.Archive, peer *archive.Manifest) (map[model.ChunkRef]struct{}, bool) {
	if peer == nil {
		return nil, false
	}
	local, err := a.Manifest()
	if err != nil {
		// A manifest this archive cannot build is a reason to send everything,
		// not a reason to send a subset chosen by a failure.
		return nil, false
	}
	out := map[model.ChunkRef]struct{}{}
	for _, difference := range archive.Compare(local, *peer) {
		if difference.Chunk == nil {
			// A whole server or dimension differs, so nothing can be excluded
			// for it.
			return nil, false
		}
		out[*difference.Chunk] = struct{}{}
	}
	return out, true
}

func copyObject(a archive.Archive, ref model.BlobRef, out string) error {
	source, err := a.CAS.Open(ref)
	if err != nil {
		return fmt.Errorf("open object %s: %w", ref.Digest, err)
	}
	defer source.Close()

	dir := filepath.Join(out, "objects", "sha256", ref.Digest[:2], ref.Digest[2:4])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	destination, err := os.Create(filepath.Join(dir, ref.Digest))
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		return err
	}
	return destination.Close()
}

// Received reports what an import took in.
type Received struct {
	Observations int
	Objects      int
	AlreadyHeld  int
}

// Receive verifies and merges a bundle.
//
// Every object is stored through the verifying path, so bytes that do not hash
// to the digest the bundle claims are refused rather than written. An
// observation is only added once its components are present, and the archive's
// own identity rules reject a record whose id does not match its contents. A
// bundle from an untrusted peer therefore cannot introduce anything the archive
// would not have accepted from its own adapter.
func Receive(a archive.Archive, dir string) (Received, error) {
	data, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		return Received{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Received{}, fmt.Errorf("bundle.json: %w", err)
	}
	if manifest.Schema != Schema {
		return Received{}, fmt.Errorf("unsupported transfer bundle schema %q", manifest.Schema)
	}

	var received Received
	for _, object := range manifest.Objects {
		if err := validateDigest(object.Digest); err != nil {
			return Received{}, err
		}
		path := filepath.Join(dir, "objects", "sha256", object.Digest[:2], object.Digest[2:4], object.Digest)
		file, err := os.Open(path)
		if err != nil {
			return Received{}, fmt.Errorf("object %s declared but missing: %w", object.Digest[:12], err)
		}
		ref := model.BlobRef{Algorithm: "sha256", Digest: object.Digest, Size: object.Size}
		_, err = a.CAS.PutVerified(file, ref)
		file.Close()
		if err != nil {
			return Received{}, fmt.Errorf("object %s: %w", object.Digest[:12], err)
		}
		received.Objects++
	}

	for _, id := range manifest.Observations {
		if err := validateDigest(id); err != nil {
			return Received{}, err
		}
		raw, err := os.ReadFile(filepath.Join(dir, "observations", id+".json"))
		if err != nil {
			return Received{}, fmt.Errorf("observation %s declared but missing: %w", id[:12], err)
		}
		var observation model.Observation
		if err := json.Unmarshal(raw, &observation); err != nil {
			return Received{}, fmt.Errorf("observation %s: %w", id[:12], err)
		}
		if observation.ID != id {
			return Received{}, fmt.Errorf("observation file %s contains id %s", id[:12], observation.ID)
		}
		// Recomputing the identity is what stops a peer renaming someone else's
		// observation or moving it to another chunk or moment.
		if err := observation.ValidateStored(); err != nil {
			return Received{}, fmt.Errorf("observation %s: %w", id[:12], err)
		}
		for name, ref := range observation.Components {
			if err := a.CAS.Verify(ref); err != nil {
				return Received{}, fmt.Errorf("observation %s references component %s that this archive cannot resolve: %w", id[:12], name, err)
			}
		}

		existing, err := a.Observations(observation.Chunk)
		if err != nil {
			return Received{}, err
		}
		held := false
		for _, candidate := range existing {
			if candidate.ID == observation.ID {
				held = true
				break
			}
		}
		if held {
			received.AlreadyHeld++
			continue
		}
		if err := a.AddObservation(observation); err != nil {
			return Received{}, fmt.Errorf("observation %s: %w", id[:12], err)
		}
		received.Observations++
	}
	return received, nil
}

func validateDigest(value string) error {
	if len(value) != 64 {
		return fmt.Errorf("%q is not a sha256 digest", value)
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return fmt.Errorf("%q is not a sha256 digest", value)
		}
	}
	return nil
}

func allObservations(a archive.Archive) ([]model.Observation, error) {
	servers, err := a.Servers()
	if err != nil {
		return nil, err
	}
	var out []model.Observation
	for _, server := range servers {
		dimensions, err := a.Dimensions(server)
		if err != nil {
			return nil, err
		}
		for _, dimension := range dimensions {
			chunks, err := a.DimensionObservations(server, dimension)
			if err != nil {
				return nil, err
			}
			for _, chunk := range chunks {
				out = append(out, chunk.Observations...)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("this archive holds no observation to send")
	}
	return out, nil
}
