// Package bundle imports crash-safe capture bundles into a WorldLedger archive.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/cas"
	"github.com/worldledger/worldledger-mc/internal/model"
)

const Schema = "worldledger.capture-bundle/v1"

var ErrInvalidBundle = errors.New("invalid capture bundle")

// Limits bounds all input whose size is controlled by a bundle producer.
// Zero-valued fields use DefaultLimits.
type Limits struct {
	MaxManifestBytes  int64
	MaxComponents     int
	MaxComponentBytes int64
	MaxTotalBytes     int64
}

// DefaultLimits returns conservative limits that comfortably contain a normal
// canonical Minecraft chunk observation.
func DefaultLimits() Limits {
	return Limits{
		MaxManifestBytes:  1 << 20,
		MaxComponents:     256,
		MaxComponentBytes: 64 << 20,
		MaxTotalBytes:     512 << 20,
	}
}

type Options struct {
	Limits          Limits
	DeleteOnSuccess bool
}

type Result struct {
	ObservationID string
	StateDigest   string
	Components    int
	Deleted       bool
}

type Chunk struct {
	X int32 `json:"x"`
	Z int32 `json:"z"`
}

type Source struct {
	Contributor string `json:"contributor"`
	Agent       string `json:"agent,omitempty"`
}

type Capture struct {
	SessionID string  `json:"session_id,omitempty"`
	Sequence  *uint64 `json:"sequence,omitempty"`
	Trigger   string  `json:"trigger,omitempty"`
}

type ComponentDescriptor struct {
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type Manifest struct {
	Schema        string                         `json:"schema"`
	ServerID      string                         `json:"server_id"`
	ServerAddress string                         `json:"server_address,omitempty"`
	Dimension     string                         `json:"dimension"`
	Chunk         Chunk                          `json:"chunk"`
	ObservedAt    time.Time                      `json:"observed_at"`
	Protocol      string                         `json:"protocol"`
	Source        Source                         `json:"source"`
	Capture       *Capture                       `json:"capture,omitempty"`
	Components    map[string]ComponentDescriptor `json:"components"`
}

type rawManifest struct {
	Schema        string                            `json:"schema"`
	ServerID      string                            `json:"server_id"`
	ServerAddress string                            `json:"server_address"`
	Dimension     string                            `json:"dimension"`
	Chunk         *rawChunk                         `json:"chunk"`
	ObservedAt    string                            `json:"observed_at"`
	Protocol      string                            `json:"protocol"`
	Source        *Source                           `json:"source"`
	Capture       *Capture                          `json:"capture"`
	Components    map[string]rawComponentDescriptor `json:"components"`
}

type rawChunk struct {
	X *int32 `json:"x"`
	Z *int32 `json:"z"`
}

type rawComponentDescriptor struct {
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
	Size      *int64 `json:"size"`
}

type preparedBundle struct {
	root         string
	realRoot     string
	rootInfo     os.FileInfo
	manifestInfo os.FileInfo
	manifest     Manifest
	components   []preparedComponent
}

type preparedComponent struct {
	name       string
	descriptor ComponentDescriptor
}

// Import validates every component before making an observation visible. CAS
// objects are immutable, so a later failure may leave only unreferenced objects.
func Import(a archive.Archive, bundleDir string, options Options) (Result, error) {
	limits, err := options.Limits.withDefaults()
	if err != nil {
		return Result{}, err
	}
	prepared, err := prepare(bundleDir, limits)
	if err != nil {
		return Result{}, err
	}
	if options.DeleteOnSuccess {
		if err := validateCleanupTarget(prepared.realRoot, a.Root); err != nil {
			return Result{}, err
		}
	}

	refs := make(map[string]model.BlobRef, len(prepared.components))
	resolver := pathResolver{}
	for _, component := range prepared.components {
		f, info, err := openRegularWithin(prepared.root, prepared.realRoot, component.descriptor.Path, resolver)
		if err != nil {
			return Result{}, invalidf("component %q: %v", component.name, err)
		}
		if os.SameFile(prepared.manifestInfo, info) {
			_ = f.Close()
			return Result{}, invalidf("component %q resolves to bundle.json", component.name)
		}

		expected := model.BlobRef{
			Algorithm: component.descriptor.Algorithm,
			Digest:    component.descriptor.Digest,
			Size:      component.descriptor.Size,
		}
		ref, putErr := a.CAS.PutVerified(f, expected)
		closeErr := f.Close()
		if putErr != nil {
			if errors.Is(putErr, cas.ErrObjectMismatch) {
				return Result{}, invalidf("component %q changed while importing: %v", component.name, putErr)
			}
			return Result{}, fmt.Errorf("store component %q: %w", component.name, putErr)
		}
		if closeErr != nil {
			return Result{}, fmt.Errorf("close component %q: %w", component.name, closeErr)
		}
		refs[component.name] = ref
	}

	o := model.Observation{
		Chunk: model.ChunkRef{
			ServerID:  prepared.manifest.ServerID,
			Dimension: prepared.manifest.Dimension,
			X:         prepared.manifest.Chunk.X,
			Z:         prepared.manifest.Chunk.Z,
		},
		ObservedAt: prepared.manifest.ObservedAt,
		Protocol:   prepared.manifest.Protocol,
		Source: model.Source{
			Contributor: prepared.manifest.Source.Contributor,
			Agent:       prepared.manifest.Source.Agent,
		},
		Components: refs,
	}
	if err := o.Finalize(); err != nil {
		return Result{}, invalidf("observation: %v", err)
	}
	if err := a.AddObservation(o); err != nil {
		return Result{}, fmt.Errorf("commit observation: %w", err)
	}

	result := Result{
		ObservationID: o.ID,
		StateDigest:   o.StateDigest,
		Components:    len(refs),
	}
	if options.DeleteOnSuccess {
		if err := removeImportedBundle(prepared); err != nil {
			return result, fmt.Errorf("observation %s imported, but bundle cleanup failed: %w", o.ID, err)
		}
		result.Deleted = true
	}
	return result, nil
}

func prepare(bundleDir string, limits Limits) (preparedBundle, error) {
	if strings.TrimSpace(bundleDir) == "" {
		return preparedBundle{}, invalidf("bundle directory is required")
	}
	root, err := filepath.Abs(bundleDir)
	if err != nil {
		return preparedBundle{}, invalidf("resolve bundle directory: %v", err)
	}
	root = filepath.Clean(root)
	if filepath.Dir(root) == root {
		return preparedBundle{}, invalidf("filesystem root cannot be a bundle")
	}
	if strings.HasPrefix(filepath.Base(root), ".tmp-") {
		return preparedBundle{}, invalidf("temporary bundle directories are not importable")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return preparedBundle{}, invalidf("open bundle directory: %v", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return preparedBundle{}, invalidf("bundle path must be a real directory, not a symlink or special file")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return preparedBundle{}, invalidf("resolve bundle directory: %v", err)
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return preparedBundle{}, invalidf("resolve bundle directory: %v", err)
	}

	manifestFile, manifestInfo, err := openRegularWithin(root, realRoot, "bundle.json", nil)
	if err != nil {
		return preparedBundle{}, invalidf("bundle.json: %v", err)
	}
	data, readErr := readLimited(manifestFile, limits.MaxManifestBytes)
	closeErr := manifestFile.Close()
	if readErr != nil {
		return preparedBundle{}, invalidf("bundle.json: %v", readErr)
	}
	if closeErr != nil {
		return preparedBundle{}, fmt.Errorf("close bundle.json: %w", closeErr)
	}
	manifest, err := parseManifest(data, limits)
	if err != nil {
		return preparedBundle{}, err
	}

	names := make([]string, 0, len(manifest.Components))
	for name := range manifest.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	components := make([]preparedComponent, 0, len(names))
	for _, name := range names {
		descriptor := manifest.Components[name]
		f, info, err := openRegularWithin(root, realRoot, descriptor.Path, nil)
		if err != nil {
			return preparedBundle{}, invalidf("component %q: %v", name, err)
		}
		if os.SameFile(manifestInfo, info) {
			_ = f.Close()
			return preparedBundle{}, invalidf("component %q resolves to bundle.json", name)
		}
		if info.Size() != descriptor.Size {
			_ = f.Close()
			return preparedBundle{}, invalidf("component %q size mismatch: have %d want %d", name, info.Size(), descriptor.Size)
		}
		digest, size, verifyErr := hashLimited(f, descriptor.Size)
		closeErr := f.Close()
		if verifyErr != nil {
			return preparedBundle{}, invalidf("component %q: %v", name, verifyErr)
		}
		if closeErr != nil {
			return preparedBundle{}, fmt.Errorf("close component %q: %w", name, closeErr)
		}
		if size != descriptor.Size {
			return preparedBundle{}, invalidf("component %q size mismatch: have %d want %d", name, size, descriptor.Size)
		}
		if digest != descriptor.Digest {
			return preparedBundle{}, invalidf("component %q digest mismatch: have %s want %s", name, digest, descriptor.Digest)
		}
		components = append(components, preparedComponent{name: name, descriptor: descriptor})
	}

	return preparedBundle{
		root:         root,
		realRoot:     filepath.Clean(realRoot),
		rootInfo:     rootInfo,
		manifestInfo: manifestInfo,
		manifest:     manifest,
		components:   components,
	}, nil
}

func parseManifest(data []byte, limits Limits) (Manifest, error) {
	if !utf8.Valid(data) {
		return Manifest{}, invalidf("bundle.json is not valid UTF-8")
	}
	if err := validateJSONStructure(data); err != nil {
		return Manifest{}, invalidf("bundle.json: %v", err)
	}

	var raw rawManifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Manifest{}, invalidf("bundle.json: %v", err)
	}

	if raw.Schema != Schema {
		return Manifest{}, invalidf("unsupported schema %q", raw.Schema)
	}
	if strings.TrimSpace(raw.ServerID) == "" {
		return Manifest{}, invalidf("server_id is required")
	}
	if strings.TrimSpace(raw.Dimension) == "" {
		return Manifest{}, invalidf("dimension is required")
	}
	if raw.Chunk == nil || raw.Chunk.X == nil || raw.Chunk.Z == nil {
		return Manifest{}, invalidf("chunk.x and chunk.z are required")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, raw.ObservedAt)
	if err != nil || observedAt.IsZero() {
		if err == nil {
			err = errors.New("zero timestamp is not allowed")
		}
		return Manifest{}, invalidf("observed_at: %v", err)
	}
	if strings.TrimSpace(raw.Protocol) == "" {
		return Manifest{}, invalidf("protocol is required")
	}
	if raw.Source == nil || strings.TrimSpace(raw.Source.Contributor) == "" {
		return Manifest{}, invalidf("source.contributor is required")
	}
	if len(raw.Components) == 0 {
		return Manifest{}, invalidf("at least one component is required")
	}
	if len(raw.Components) > limits.MaxComponents {
		return Manifest{}, invalidf("component count %d exceeds limit %d", len(raw.Components), limits.MaxComponents)
	}

	components := make(map[string]ComponentDescriptor, len(raw.Components))
	var total int64
	for name, rawDescriptor := range raw.Components {
		if strings.TrimSpace(name) == "" {
			return Manifest{}, invalidf("component name must not be empty")
		}
		if err := validateRelativePOSIXPath(rawDescriptor.Path); err != nil {
			return Manifest{}, invalidf("component %q path: %v", name, err)
		}
		if rawDescriptor.Algorithm != "sha256" {
			return Manifest{}, invalidf("component %q uses unsupported algorithm %q", name, rawDescriptor.Algorithm)
		}
		if !validLowerSHA256(rawDescriptor.Digest) {
			return Manifest{}, invalidf("component %q has invalid lowercase SHA-256 digest", name)
		}
		if rawDescriptor.Size == nil {
			return Manifest{}, invalidf("component %q size is required", name)
		}
		if *rawDescriptor.Size < 0 {
			return Manifest{}, invalidf("component %q has negative size", name)
		}
		if *rawDescriptor.Size > limits.MaxComponentBytes {
			return Manifest{}, invalidf("component %q size %d exceeds limit %d", name, *rawDescriptor.Size, limits.MaxComponentBytes)
		}
		if *rawDescriptor.Size > limits.MaxTotalBytes-total {
			return Manifest{}, invalidf("aggregate component size exceeds limit %d", limits.MaxTotalBytes)
		}
		total += *rawDescriptor.Size
		components[name] = ComponentDescriptor{
			Path:      rawDescriptor.Path,
			Algorithm: rawDescriptor.Algorithm,
			Digest:    rawDescriptor.Digest,
			Size:      *rawDescriptor.Size,
		}
	}

	return Manifest{
		Schema:        raw.Schema,
		ServerID:      raw.ServerID,
		ServerAddress: raw.ServerAddress,
		Dimension:     raw.Dimension,
		Chunk:         Chunk{X: *raw.Chunk.X, Z: *raw.Chunk.Z},
		ObservedAt:    observedAt,
		Protocol:      raw.Protocol,
		Source:        *raw.Source,
		Capture:       raw.Capture,
		Components:    components,
	}, nil
}

func validateJSONStructure(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := consumeJSONValue(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func consumeJSONValue(dec *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeds limit 64")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return errors.New("unterminated object")
		}
	case '[':
		for dec.More() {
			if err := consumeJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("unterminated array")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	return nil
}

func validateRelativePOSIXPath(value string) error {
	if value == "" {
		return errors.New("path is required")
	}
	if strings.ContainsRune(value, '\x00') {
		return errors.New("NUL byte is not allowed")
	}
	if strings.ContainsRune(value, '\\') {
		return errors.New("backslashes are not allowed in POSIX paths")
	}
	if path.IsAbs(value) || strings.HasPrefix(value, "//") {
		return errors.New("absolute paths are not allowed")
	}
	if len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return errors.New("volume-qualified paths are not allowed")
	}
	if runtime.GOOS == "windows" && strings.ContainsRune(value, ':') {
		return errors.New("NTFS alternate data stream paths are not allowed")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("path must be normalized and must not contain empty, . or .. segments")
		}
	}
	if cleaned := path.Clean(value); cleaned != value || cleaned == "." {
		return errors.New("path is not normalized")
	}
	return nil
}

// pathResolver memoises directory resolution for one import.
//
// Resolving a component's whole path walks from the volume root and opens a
// handle for each element, and every component in a bundle shares nearly all of
// that walk. Fifty components measured 98 ms of repeated resolution against 8 ms
// of actually opening the files.
//
// Caching is sound here rather than merely faster. The loop above has already
// established by lstat that the final element is not a symlink, and resolving a
// path whose last element is not a symlink is the resolution of its parent with
// that element appended. The check itself is unchanged; only the number of times
// the same parent is walked.
type pathResolver map[string]string

func (r pathResolver) directory(path string) (string, error) {
	if resolved, seen := r[path]; seen {
		return resolved, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	r[path] = resolved
	return resolved, nil
}

// resolvePath resolves a file whose final element is known not to be a symlink.
func resolvePath(path string, resolver pathResolver) (string, error) {
	if resolver == nil {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		return filepath.Abs(resolved)
	}
	parent, err := resolver.directory(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func openRegularWithin(root, realRoot, relative string, resolver pathResolver) (*os.File, os.FileInfo, error) {
	if err := validateRelativePOSIXPath(relative); err != nil {
		return nil, nil, err
	}
	current := root
	parts := strings.Split(relative, "/")
	var finalInfo os.FileInfo
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("symlinks are not allowed")
		}
		if index < len(parts)-1 {
			if !info.IsDir() {
				return nil, nil, errors.New("path parent is not a directory")
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, nil, errors.New("component is not a regular file")
		}
		finalInfo = info
	}

	resolved, err := resolvePath(current, resolver)
	if err != nil {
		return nil, nil, err
	}
	if !pathWithin(realRoot, resolved) {
		return nil, nil, errors.New("path resolves outside the bundle")
	}

	f, err := os.Open(current)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(finalInfo, openedInfo) {
		_ = f.Close()
		return nil, nil, errors.New("file changed while it was being opened")
	}
	return f, openedInfo, nil
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("size exceeds limit %d", max)
	}
	return data, nil
}

func hashLimited(r io.Reader, expectedSize int64) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, expectedSize+1))
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func validLowerSHA256(digest string) bool {
	if len(digest) != sha256.Size*2 || digest != strings.ToLower(digest) {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func validateCleanupTarget(bundleRoot, archiveRoot string) error {
	archivePath, err := filepath.Abs(archiveRoot)
	if err != nil {
		return fmt.Errorf("resolve archive for cleanup safety: %w", err)
	}
	if resolved, evalErr := filepath.EvalSymlinks(archivePath); evalErr == nil {
		archivePath = resolved
	}
	if pathWithin(bundleRoot, archivePath) || pathWithin(archivePath, bundleRoot) {
		return invalidf("refusing cleanup because bundle directory overlaps the archive")
	}
	return nil
}

func removeImportedBundle(prepared preparedBundle) error {
	currentInfo, err := os.Lstat(prepared.root)
	if err != nil {
		return err
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.IsDir() || !os.SameFile(prepared.rootInfo, currentInfo) {
		return errors.New("bundle directory changed during import")
	}
	return os.RemoveAll(prepared.root)
}

func (limits Limits) withDefaults() (Limits, error) {
	defaults := DefaultLimits()
	if limits.MaxManifestBytes == 0 {
		limits.MaxManifestBytes = defaults.MaxManifestBytes
	}
	if limits.MaxComponents == 0 {
		limits.MaxComponents = defaults.MaxComponents
	}
	if limits.MaxComponentBytes == 0 {
		limits.MaxComponentBytes = defaults.MaxComponentBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if limits.MaxManifestBytes < 0 || limits.MaxManifestBytes == math.MaxInt64 {
		return Limits{}, errors.New("max manifest bytes must be positive and less than MaxInt64")
	}
	if limits.MaxComponents < 0 {
		return Limits{}, errors.New("max components must be positive")
	}
	if limits.MaxComponentBytes < 0 || limits.MaxComponentBytes == math.MaxInt64 {
		return Limits{}, errors.New("max component bytes must be positive and less than MaxInt64")
	}
	if limits.MaxTotalBytes < 0 {
		return Limits{}, errors.New("max total bytes must be positive")
	}
	return limits, nil
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidBundle, fmt.Sprintf(format, args...))
}
