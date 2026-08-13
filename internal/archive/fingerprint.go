package archive

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const FingerprintSchema = "worldledger.capture-fingerprint/v1"

// Fingerprint describes what an archive holds by content alone.
//
// It exists to answer one question a manifest cannot: does the same observed
// world state canonicalize to the same bytes on two different machines? A
// manifest digests observation identities, and an identity carries the instant
// and the session that produced it, so two captures of the same world always
// disagree there. That is correct for comparing mirrors and useless for
// comparing platforms.
//
// A fingerprint therefore carries only state digests and component digests.
// Nothing in it depends on when an observation was made, who made it, which
// session it belonged to, or the order in which observations were imported.
// Two runs that observed the same states agree exactly, whatever else differed.
type Fingerprint struct {
	Schema     string
	States     []FingerprintState
	Components []FingerprintComponent
	// Root is a digest over every line below, so two runs compare with one value
	// before anything looks closer.
	Root string
}

// FingerprintState is one distinct state a chunk was observed in. A chunk that
// changed during a session contributes one entry per distinct state, which is
// how a difference in what was captured stays visible rather than averaging
// away.
type FingerprintState struct {
	Server    string
	Dimension string
	X         int32
	Z         int32
	Digest    string
}

type FingerprintComponent struct {
	Name   string
	Digest string
	Size   int64
}

// Fingerprint walks the whole archive under one lock. Passing a server limits
// the result to that server; an empty string covers every server.
func (a Archive) Fingerprint(serverFilter string) (Fingerprint, error) {
	servers, err := a.Servers()
	if err != nil {
		return Fingerprint{}, err
	}

	fingerprint := Fingerprint{Schema: FingerprintSchema}
	seenState := map[string]struct{}{}
	seenComponent := map[string]struct{}{}

	for _, server := range servers {
		if serverFilter != "" && server != serverFilter {
			continue
		}
		dimensions, err := a.Dimensions(server)
		if err != nil {
			return Fingerprint{}, err
		}
		for _, dimension := range dimensions {
			chunks, err := a.DimensionObservations(server, dimension)
			if err != nil {
				return Fingerprint{}, err
			}
			for _, chunk := range chunks {
				for _, observation := range chunk.Observations {
					state := FingerprintState{
						Server:    server,
						Dimension: dimension,
						X:         chunk.Chunk.X,
						Z:         chunk.Chunk.Z,
						Digest:    observation.StateDigest,
					}
					key := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", state.Server, state.Dimension, state.X, state.Z, state.Digest)
					if _, exists := seenState[key]; !exists {
						seenState[key] = struct{}{}
						fingerprint.States = append(fingerprint.States, state)
					}
					for name, ref := range observation.Components {
						componentKey := name + "\x00" + ref.Digest
						if _, exists := seenComponent[componentKey]; !exists {
							seenComponent[componentKey] = struct{}{}
							fingerprint.Components = append(fingerprint.Components, FingerprintComponent{
								Name: name, Digest: ref.Digest, Size: ref.Size,
							})
						}
					}
				}
			}
		}
	}

	fingerprint.sort()
	fingerprint.Root = fingerprint.computeRoot()
	return fingerprint, nil
}

func (f *Fingerprint) sort() {
	sort.Slice(f.States, func(i, j int) bool {
		a, b := f.States[i], f.States[j]
		switch {
		case a.Server != b.Server:
			return a.Server < b.Server
		case a.Dimension != b.Dimension:
			return a.Dimension < b.Dimension
		case a.X != b.X:
			return a.X < b.X
		case a.Z != b.Z:
			return a.Z < b.Z
		default:
			return a.Digest < b.Digest
		}
	})
	sort.Slice(f.Components, func(i, j int) bool {
		if f.Components[i].Name != f.Components[j].Name {
			return f.Components[i].Name < f.Components[j].Name
		}
		return f.Components[i].Digest < f.Components[j].Digest
	})
}

func (f Fingerprint) computeRoot() string {
	return digestOf(func(h *bytes.Buffer) {
		for _, line := range f.lines() {
			h.WriteString(line)
			h.WriteByte(0)
		}
	})
}

func (f Fingerprint) lines() []string {
	out := make([]string, 0, len(f.States)+len(f.Components))
	for _, state := range f.States {
		out = append(out, fmt.Sprintf("state %s %s %d %d %s", state.Server, state.Dimension, state.X, state.Z, state.Digest))
	}
	for _, component := range f.Components {
		out = append(out, fmt.Sprintf("component %s %s %d", component.Name, component.Digest, component.Size))
	}
	return out
}

// WriteText emits a line-oriented form so two runs can be compared with an
// ordinary diff when the tooling is not to hand, and so a committed reference
// reviews as text rather than as an opaque blob.
func (f Fingerprint) WriteText(w io.Writer) error {
	buffer := bufio.NewWriter(w)
	if _, err := fmt.Fprintf(buffer, "%s\n", f.Schema); err != nil {
		return err
	}
	for _, line := range f.lines() {
		if _, err := fmt.Fprintf(buffer, "%s\n", line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(buffer, "root %s\n", f.Root); err != nil {
		return err
	}
	return buffer.Flush()
}

// ParseFingerprint reads the text form back. It recomputes the root rather than
// trusting the recorded one, so a hand-edited reference cannot claim agreement
// it does not have.
func ParseFingerprint(r io.Reader) (Fingerprint, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var fingerprint Fingerprint
	var recordedRoot string
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimRight(scanner.Text(), "\r")
		if text == "" {
			continue
		}
		if line == 1 {
			if text != FingerprintSchema {
				return Fingerprint{}, fmt.Errorf("line 1: unsupported fingerprint schema %q", text)
			}
			fingerprint.Schema = text
			continue
		}
		fields := strings.Fields(text)
		switch {
		case fields[0] == "state" && len(fields) == 6:
			x, err := strconv.ParseInt(fields[3], 10, 32)
			if err != nil {
				return Fingerprint{}, fmt.Errorf("line %d: %w", line, err)
			}
			z, err := strconv.ParseInt(fields[4], 10, 32)
			if err != nil {
				return Fingerprint{}, fmt.Errorf("line %d: %w", line, err)
			}
			fingerprint.States = append(fingerprint.States, FingerprintState{
				Server: fields[1], Dimension: fields[2], X: int32(x), Z: int32(z), Digest: fields[5],
			})
		case fields[0] == "component" && len(fields) == 4:
			size, err := strconv.ParseInt(fields[3], 10, 64)
			if err != nil {
				return Fingerprint{}, fmt.Errorf("line %d: %w", line, err)
			}
			fingerprint.Components = append(fingerprint.Components, FingerprintComponent{
				Name: fields[1], Digest: fields[2], Size: size,
			})
		case fields[0] == "root" && len(fields) == 2:
			recordedRoot = fields[1]
		default:
			return Fingerprint{}, fmt.Errorf("line %d: unrecognised entry %q", line, text)
		}
	}
	if err := scanner.Err(); err != nil {
		return Fingerprint{}, err
	}
	if fingerprint.Schema == "" {
		return Fingerprint{}, fmt.Errorf("empty fingerprint")
	}

	fingerprint.sort()
	fingerprint.Root = fingerprint.computeRoot()
	if recordedRoot != "" && recordedRoot != fingerprint.Root {
		return Fingerprint{}, fmt.Errorf("recorded root %s does not match the entries, which digest to %s", recordedRoot, fingerprint.Root)
	}
	return fingerprint, nil
}

// Difference kinds, ordered by how much they tell you.
//
// A digest comparison can prove agreement outright: identical digests mean
// identical bytes, so the two machines canonicalized the same way. It cannot
// prove the opposite on its own, because two different digests may mean the
// encoders disagreed about one state or may mean the sessions observed
// different states. Separating the cases is how a real disagreement stays
// visible instead of being explained away, and how an explainable one stops
// being reported as a defect.
const (
	// FingerprintContentDifference means both captures saw a chunk and each
	// holds a state the other lacks. Against a deterministic fixture this is
	// the signature of an encoding that is not platform independent.
	FingerprintContentDifference = "content"
	// FingerprintStatesDifference means one capture saw every state the other
	// did and more. The shorter session missed a change; the states they share
	// are byte-identical.
	FingerprintStatesDifference = "states"
	// FingerprintCoverageDifference means only one capture saw a chunk at all.
	// Coverage follows how long a player stayed and where they went, so it says
	// nothing about encoding.
	FingerprintCoverageDifference = "coverage"
)

// FingerprintDifference is one place two captures disagree.
type FingerprintDifference struct {
	Kind      string
	Server    string
	Dimension string
	X         int32
	Z         int32
	Detail    string
}

// FingerprintComparison separates the two questions a naive set difference runs
// together.
//
// Real captures were what showed this mattered: two sessions against the same
// server covered 157 and 40 chunks. Reporting 117 missing chunks alongside a
// genuine byte disagreement would bury the one line that matters under the one
// that is merely a different session length.
type FingerprintComparison struct {
	// Shared is how many chunks both captures observed. A comparison over no
	// shared chunks proves nothing, however clean it looks.
	Shared      int
	Differences []FingerprintDifference
}

// ContentDifferences returns only the disagreements that indicate a defect.
func (c FingerprintComparison) ContentDifferences() []FingerprintDifference {
	var out []FingerprintDifference
	for _, difference := range c.Differences {
		if difference.Kind == FingerprintContentDifference {
			out = append(out, difference)
		}
	}
	return out
}

type chunkKey struct {
	server    string
	dimension string
	x, z      int32
}

// CompareFingerprints reports where two captures disagree, separating a
// disagreement about bytes from a difference in how much each one saw.
func CompareFingerprints(local, remote Fingerprint) FingerprintComparison {
	localStates := statesByChunk(local)
	remoteStates := statesByChunk(remote)

	keys := make([]chunkKey, 0, len(localStates)+len(remoteStates))
	seen := map[chunkKey]struct{}{}
	for _, source := range []map[chunkKey][]string{localStates, remoteStates} {
		for key := range source {
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch {
		case a.server != b.server:
			return a.server < b.server
		case a.dimension != b.dimension:
			return a.dimension < b.dimension
		case a.x != b.x:
			return a.x < b.x
		default:
			return a.z < b.z
		}
	})

	comparison := FingerprintComparison{}
	for _, key := range keys {
		mine, hasMine := localStates[key]
		theirs, hasTheirs := remoteStates[key]
		switch {
		case !hasTheirs:
			comparison.Differences = append(comparison.Differences, difference(FingerprintCoverageDifference, key, "only the local capture observed this chunk"))
		case !hasMine:
			comparison.Differences = append(comparison.Differences, difference(FingerprintCoverageDifference, key, "only the remote capture observed this chunk"))
		default:
			comparison.Shared++
			if equalStrings(mine, theirs) {
				continue
			}
			onlyMine := missingFrom(mine, theirs)
			onlyTheirs := missingFrom(theirs, mine)
			switch {
			case len(onlyTheirs) == 0:
				comparison.Differences = append(comparison.Differences, difference(FingerprintStatesDifference, key,
					fmt.Sprintf("the local capture also saw %s", strings.Join(short(onlyMine), ", "))))
			case len(onlyMine) == 0:
				comparison.Differences = append(comparison.Differences, difference(FingerprintStatesDifference, key,
					fmt.Sprintf("the remote capture also saw %s", strings.Join(short(onlyTheirs), ", "))))
			default:
				comparison.Differences = append(comparison.Differences, difference(FingerprintContentDifference, key,
					fmt.Sprintf("local only %s; remote only %s",
						strings.Join(short(onlyMine), ", "), strings.Join(short(onlyTheirs), ", "))))
			}
		}
	}
	return comparison
}

func difference(kind string, key chunkKey, detail string) FingerprintDifference {
	return FingerprintDifference{
		Kind: kind, Server: key.server, Dimension: key.dimension, X: key.x, Z: key.z, Detail: detail,
	}
}

func statesByChunk(f Fingerprint) map[chunkKey][]string {
	out := map[chunkKey][]string{}
	for _, state := range f.States {
		key := chunkKey{state.Server, state.Dimension, state.X, state.Z}
		out[key] = append(out[key], state.Digest)
	}
	for key := range out {
		sort.Strings(out[key])
	}
	return out
}

// missingFrom returns the entries of a that b does not contain.
func missingFrom(a, b []string) []string {
	present := make(map[string]struct{}, len(b))
	for _, value := range b {
		present[value] = struct{}{}
	}
	var out []string
	for _, value := range a {
		if _, exists := present[value]; !exists {
			out = append(out, value)
		}
	}
	return out
}

// short abbreviates digests so a difference line stays readable. The full
// values remain in the fingerprint files being compared.
func short(digests []string) []string {
	out := make([]string, 0, len(digests))
	for _, digest := range digests {
		if len(digest) > 12 {
			digest = digest[:12]
		}
		out = append(out, digest)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
