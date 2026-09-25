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
	"strconv"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/attest"
	"github.com/worldledger/worldledger-mc/internal/model"
	"github.com/worldledger/worldledger-mc/internal/redact"
)

const Schema = "worldledger.transfer-bundle/v1"

// Manifest describes a bundle's contents so a receiver knows what it is about
// to verify before reading any of it.
type Manifest struct {
	Schema       string      `json:"schema"`
	CreatedAt    time.Time   `json:"created_at"`
	Observations []string    `json:"observations"`
	Objects      []ObjectRef `json:"objects"`
	// Attestations are the signatures over the observations above. They travel
	// so that a record naming a contributor can be told apart from one that was
	// merely written naming them.
	Attestations []string `json:"attestations,omitempty"`
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
	Attestations int
	// Withheld counts observations a declared redaction kept out of the bundle.
	// It is reported rather than left silent: somebody who withdrew consent is
	// owed the operator being able to see that it took effect, and an operator
	// who expected 158 records and got 118 is owed the reason.
	Withheld int
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
	if err := requireSeparateFromArchive(out, a.Root); err != nil {
		return Sent{}, err
	}
	local, err := a.Fingerprint("")
	if err != nil {
		return Sent{}, err
	}
	negotiation := archive.Negotiate(local, peer)

	observations, err := allObservations(a)
	if err != nil {
		return Sent{}, err
	}

	// Withdrawn observations do not leave.
	//
	// Every other path that builds something to hand over filters these, and
	// this one did not, which made it the only way a contributor who had
	// withdrawn consent could still be sent to a peer, record and bytes, with
	// nothing printed. It is also the path where it matters most: an export
	// writes a world onto the operator's own disk, and this hands data to
	// somebody else.
	redactions, err := redact.NewStore(a.Root).List()
	if err != nil {
		return Sent{}, fmt.Errorf("read redactions: %w", err)
	}

	wantedChunks, filterChunks := chunksTheyDisagreeAbout(a, peerManifest)
	selected := observations[:0:0]
	var sent Sent
	for _, observation := range observations {
		if filterChunks {
			if _, differs := wantedChunks[observation.Chunk]; !differs {
				continue
			}
		}
		if kept, dropped := redactions.Filter([]model.Observation{observation}); len(kept) == 0 {
			sent.Withheld += len(dropped)
			continue
		}
		selected = append(selected, observation)
	}

	if len(negotiation.Offer) == 0 && len(selected) == 0 {
		// Nothing to send, but what was held back is still worth saying.
		return Sent{Withheld: sent.Withheld}, nil
	}

	// The negotiation was computed from this archive's whole fingerprint, which
	// includes what a redaction withholds. Dropping the records without
	// dropping their bytes would be the same disclosure with an extra step, so
	// an object travels only if a record that is travelling references it.
	//
	// An object shared between a withheld observation and a kept one is still
	// sent: it is needed for the kept one, and content addressing means those
	// are the same bytes rather than a copy belonging to either.
	needed := map[string]struct{}{}
	for _, observation := range selected {
		for _, ref := range observation.Components {
			needed[ref.Digest] = struct{}{}
		}
	}
	offered := make(map[string]archive.FingerprintComponent, len(negotiation.Offer))
	for _, component := range negotiation.Offer {
		if _, wanted := needed[component.Digest]; !wanted {
			continue
		}
		offered[component.Digest] = component
	}

	if err := os.MkdirAll(filepath.Join(out, "observations"), 0o755); err != nil {
		return Sent{}, err
	}

	manifest := Manifest{Schema: Schema, CreatedAt: time.Now().UTC()}

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

		// Signatures travel with the records they are about.
		//
		// Leaving them behind made the exchange the one place attribution
		// stopped meaning anything. Anybody can write a record naming somebody
		// else, since an id is a hash of the record and a made-up one is perfectly
		// well formed. A signature is what tells the two apart, and with the
		// signatures staying home, an honestly transferred record and a
		// fabricated one both arrived unsigned and read identically.
		//
		// Nothing here has to be trusted: Store.Put refuses an attestation that
		// does not verify against the observation id it names.
		attestations, err := attest.NewStore(a.Root).For(observation.ID)
		if err != nil {
			return Sent{}, fmt.Errorf("read attestations for %s: %w", observation.ID, err)
		}
		for _, attestation := range attestations {
			encoded, err := json.MarshalIndent(attestation, "", "  ")
			if err != nil {
				return Sent{}, err
			}
			name := observation.ID + "." + strconv.Itoa(sent.Attestations) + ".json"
			if err := os.MkdirAll(filepath.Join(out, "attestations"), 0o755); err != nil {
				return Sent{}, err
			}
			if err := os.WriteFile(filepath.Join(out, "attestations", name), append(encoded, '\n'), 0o644); err != nil {
				return Sent{}, err
			}
			manifest.Attestations = append(manifest.Attestations, name)
			sent.Attestations++
		}
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

// requireSeparateFromArchive refuses to assemble a bundle inside the archive it
// is being assembled from.
//
// A bundle directory is laid out like an archive: observations/<id>.json and
// objects addressed by digest. Pointed at the archive itself, Send writes its
// own records back over the ones it is reading, and the result is an archive
// whose integrity check reports each observation stored more than once, from a
// command that printed a success and a suggestion for what to do next.
//
// internal/bundle has exactly this guard for the mirror-image case, on the
// reasoning that a path somebody typed is allowed to be wrong and a program is
// not allowed to act on it destructively. This path had none.
func requireSeparateFromArchive(out, archiveRoot string) error {
	outPath, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("resolve the output directory: %w", err)
	}
	archivePath, err := filepath.Abs(archiveRoot)
	if err != nil {
		return fmt.Errorf("resolve the archive: %w", err)
	}
	// Both resolved the same way, which matters more than it looks. The output
	// directory usually does not exist yet, so EvalSymlinks fails on it and
	// leaves it as typed, while the archive does exist and resolves. On Windows
	// that is enough on its own to make one comparison fail: a temporary path
	// arrives with the short form of a name that has a space in it, and the
	// resolved archive has the long one. Same directory, two strings, no
	// overlap detected.
	outPath = resolveExisting(outPath)
	archivePath = resolveExisting(archivePath)
	if within(archivePath, outPath) || within(outPath, archivePath) {
		return fmt.Errorf(
			"refusing to build a bundle at %s, which is inside the archive it would be built from"+
				" (or the other way round): a bundle is laid out like an archive, so writing one"+
				" here would write over the records it is reading. Choose a directory outside %s",
			out, archiveRoot)
	}
	return nil
}

// resolveExisting resolves as much of a path as exists and keeps the rest.
//
// A directory that has not been created cannot be resolved, and refusing to
// compare it would be refusing to check the one case that matters: send is
// usually pointed at somewhere new.
func resolveExisting(path string) string {
	remainder := ""
	current := filepath.Clean(path)
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(path)
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}

func within(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
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
	// Attestations are signatures that arrived with the records they sign and
	// verified against them.
	Attestations int
}

// Receive verifies and merges a bundle.
//
// Every object is stored through the verifying path, so bytes that do not hash
// to the digest the bundle claims are refused rather than written. An
// observation is only added once its components are present, and the archive's
// own identity rules reject a record whose id does not match its contents. A
// bundle from an untrusted peer therefore cannot introduce anything the archive
// would not have accepted from its own adapter.
// Sizes a peer does not get to choose.
//
// internal/bundle caps four dimensions on the adapter path, which is the less
// untrusted of the two: the bundles it reads were written by a mod on the same
// machine. This path reads a directory somebody else assembled and had no caps
// at all, so a peer's bundle.json was read whole into memory whatever its size.
// The numbers are generous against any real bundle and finite against a hostile
// one.
const (
	maxTransferManifestBytes = 8 << 20
	maxRecordBytes           = 1 << 20
)

func readAtMost(path string, max int64) ([]byte, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	// One byte past the limit, so a file exactly at it is accepted and anything
	// larger is refused rather than silently truncated into something that
	// might still parse.
	data, err := io.ReadAll(io.LimitReader(handle, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", filepath.Base(path), max)
	}
	return data, nil
}

func Receive(a archive.Archive, dir string) (Received, error) {
	data, err := readAtMost(filepath.Join(dir, "bundle.json"), maxTransferManifestBytes)
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

	// What the bundle declares it carries, so a signature cannot be stored for a
	// record that is not here.
	wantedIDs := make(map[string]struct{}, len(manifest.Observations))
	for _, id := range manifest.Observations {
		wantedIDs[id] = struct{}{}
	}

	for _, id := range manifest.Observations {
		if err := validateDigest(id); err != nil {
			return Received{}, err
		}
		raw, err := readAtMost(filepath.Join(dir, "observations", id+".json"), maxRecordBytes)
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

	// Signatures last, so that an attestation is only stored for a record the
	// archive has accepted.
	//
	// None of this is trusted. Store.Put re-derives the signed preimage from the
	// observation id and checks the signature against the key in the
	// attestation, so a bundle can only add a signature that is genuinely valid.
	// What it cannot do is add one that makes an unknown key known: whether a
	// key is recognised is a separate, local, attributed decision, and
	// `attest verify` reports a valid signature from an unregistered key as
	// exactly that rather than as an endorsement.
	attestations := attest.NewStore(a.Root)
	for _, name := range manifest.Attestations {
		if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
			return Received{}, fmt.Errorf("attestation %q: a name may not contain a path", name)
		}
		body, err := readAtMost(filepath.Join(dir, "attestations", name), maxRecordBytes)
		if err != nil {
			return Received{}, fmt.Errorf("attestation %s: %w", name, err)
		}
		var attestation attest.Attestation
		if err := json.Unmarshal(body, &attestation); err != nil {
			return Received{}, fmt.Errorf("attestation %s: %w", name, err)
		}
		if _, wanted := wantedIDs[attestation.ObservationID]; !wanted {
			return Received{}, fmt.Errorf(
				"attestation %s signs %s, which this bundle does not carry", name, attestation.ObservationID)
		}
		if err := attestations.Put(attestation); err != nil {
			return Received{}, fmt.Errorf("attestation %s: %w", name, err)
		}
		received.Attestations++
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
