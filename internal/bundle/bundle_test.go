package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/model"
)

func TestImportValidBundleIsIdempotent(t *testing.T) {
	a := newArchive(t)
	payloads := map[string][]byte{
		"mcjava.shape":     []byte("shape fixture"),
		"mcjava.blocks.-4": []byte("block fixture"),
	}
	bundleDir, _ := makeBundle(t, "ready-session-1", payloads)

	first, err := Import(a, bundleDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Components != len(payloads) || first.ObservationID == "" || first.StateDigest == "" || first.Deleted {
		t.Fatalf("unexpected first result: %#v", first)
	}
	if _, err := os.Stat(bundleDir); err != nil {
		t.Fatalf("source bundle should remain after a normal import: %v", err)
	}

	second, err := Import(a, bundleDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("duplicate import returned a different result: first=%#v second=%#v", first, second)
	}

	observations, err := a.Observations(model.ChunkRef{
		ServerID:  "example.org:25565",
		Dimension: "minecraft:overworld",
		X:         14,
		Z:         -8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("duplicate import created %d observations", len(observations))
	}
	if observations[0].ID != first.ObservationID || observations[0].StateDigest != first.StateDigest {
		t.Fatalf("stored observation does not match import result: %#v", observations[0])
	}
	for name, want := range payloads {
		ref, ok := observations[0].Components[name]
		if !ok {
			t.Fatalf("missing component %q", name)
		}
		f, err := a.CAS.Open(ref)
		if err != nil {
			t.Fatal(err)
		}
		got, readErr := io.ReadAll(f)
		closeErr := f.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if string(got) != string(want) {
			t.Fatalf("component %q changed: have %q want %q", name, got, want)
		}
	}
	assertArchiveClean(t, a, 1, len(payloads))
}

func TestRejectedBundlesLeaveArchiveClean(t *testing.T) {
	tests := []struct {
		name     string
		payloads map[string][]byte
		mutate   func(t *testing.T, bundleDir string, manifest map[string]any)
		options  Options
	}{
		{
			name: "wrong size",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["size"] = float64(999)
			},
		},
		{
			name: "wrong digest",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["digest"] = strings.Repeat("0", 64)
			},
			options: Options{DeleteOnSuccess: true},
		},
		{
			name: "later component has wrong digest",
			payloads: map[string][]byte{
				"mcjava.blocks.0": []byte("blocks fixture"),
				"mcjava.shape":    []byte("shape fixture"),
			},
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["digest"] = strings.Repeat("0", 64)
			},
		},
		{
			name: "uppercase digest",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				d := descriptor(manifest, "mcjava.shape")
				d["digest"] = strings.ToUpper(d["digest"].(string))
			},
		},
		{
			name: "missing file",
			mutate: func(t *testing.T, bundleDir string, manifest map[string]any) {
				if err := os.Remove(filepath.Join(bundleDir, filepath.FromSlash(descriptor(manifest, "mcjava.shape")["path"].(string)))); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "absolute path",
			mutate: func(t *testing.T, bundleDir string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["path"] = filepath.Join(bundleDir, "components", "component-0.bin")
			},
		},
		{
			name: "volume relative path",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["path"] = "C:component.bin"
			},
		},
		{
			name: "parent traversal",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["path"] = "../outside.bin"
			},
		},
		{
			name: "embedded traversal",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["path"] = "components/../outside.bin"
			},
		},
		{
			name: "backslash path",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["path"] = `components\component-0.bin`
			},
		},
		{
			name: "manifest as component",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["path"] = "bundle.json"
			},
		},
		{
			name: "unsupported algorithm",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				descriptor(manifest, "mcjava.shape")["algorithm"] = "sha512"
			},
		},
		{
			name: "unsupported schema",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				manifest["schema"] = "worldledger.capture-bundle/v2"
			},
		},
		{
			name: "missing chunk coordinate",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				delete(manifest["chunk"].(map[string]any), "x")
			},
		},
		{
			name: "missing descriptor size",
			mutate: func(t *testing.T, _ string, manifest map[string]any) {
				delete(descriptor(manifest, "mcjava.shape"), "size")
			},
		},
		{
			name:    "component too large",
			mutate:  func(t *testing.T, _ string, _ map[string]any) {},
			options: Options{Limits: Limits{MaxComponentBytes: 4}},
		},
		{
			name: "aggregate too large",
			payloads: map[string][]byte{
				"mcjava.shape":    []byte("shape fixture"),
				"mcjava.blocks.0": []byte("blocks fixture"),
			},
			mutate:  func(t *testing.T, _ string, _ map[string]any) {},
			options: Options{Limits: Limits{MaxTotalBytes: 8}},
		},
		{
			name: "too many components",
			payloads: map[string][]byte{
				"mcjava.shape":    []byte("shape fixture"),
				"mcjava.blocks.0": []byte("blocks fixture"),
			},
			mutate:  func(t *testing.T, _ string, _ map[string]any) {},
			options: Options{Limits: Limits{MaxComponents: 1}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := newArchive(t)
			payloads := test.payloads
			if payloads == nil {
				payloads = map[string][]byte{"mcjava.shape": []byte("shape fixture")}
			}
			bundleDir, manifest := makeBundle(t, "ready-hostile", payloads)
			test.mutate(t, bundleDir, manifest)
			writeManifest(t, bundleDir, manifest)

			_, err := Import(a, bundleDir, test.options)
			if err == nil {
				t.Fatal("expected bundle rejection")
			}
			if !errors.Is(err, ErrInvalidBundle) {
				t.Fatalf("expected ErrInvalidBundle, got %v", err)
			}
			if _, err := os.Stat(bundleDir); err != nil {
				t.Fatalf("rejected source bundle was modified or removed: %v", err)
			}
			assertArchiveClean(t, a, 0, 0)
		})
	}
}

func TestMalformedManifestsAreRejected(t *testing.T) {
	tests := map[string][]byte{
		"truncated":         []byte(`{"schema":`),
		"trailing value":    []byte(`{} {}`),
		"duplicate key":     []byte(`{"schema":"worldledger.capture-bundle/v1","schema":"worldledger.capture-bundle/v1"}`),
		"unknown field":     []byte(`{"schema":"worldledger.capture-bundle/v1","unknown":true}`),
		"invalid utf8":      {0xff, 0xfe, 0xfd},
		"excessive nesting": []byte(strings.Repeat("[", 66) + strings.Repeat("]", 66)),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			a := newArchive(t)
			bundleDir, _ := makeBundle(t, "ready-malformed", map[string][]byte{"mcjava.shape": []byte("shape fixture")})
			if err := os.WriteFile(filepath.Join(bundleDir, "bundle.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Import(a, bundleDir, Options{})
			if err == nil || !errors.Is(err, ErrInvalidBundle) {
				t.Fatalf("expected invalid bundle error, got %v", err)
			}
			assertArchiveClean(t, a, 0, 0)
		})
	}
}

func TestManifestLimitIsEnforced(t *testing.T) {
	a := newArchive(t)
	bundleDir, _ := makeBundle(t, "ready-large-manifest", map[string][]byte{"mcjava.shape": []byte("shape fixture")})
	_, err := Import(a, bundleDir, Options{Limits: Limits{MaxManifestBytes: 32}})
	if err == nil || !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected manifest limit rejection, got %v", err)
	}
	assertArchiveClean(t, a, 0, 0)
}

func TestWindowsAlternateDataStreamPathIsRejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NTFS alternate data streams are Windows-specific")
	}
	if err := validateRelativePOSIXPath("components/shape.bin:stream"); err == nil {
		t.Fatal("expected alternate data stream path to be rejected")
	}
}

func TestTemporaryBundleDirectoryIsIgnored(t *testing.T) {
	a := newArchive(t)
	bundleDir, _ := makeBundle(t, ".tmp-session", map[string][]byte{"mcjava.shape": []byte("shape fixture")})
	_, err := Import(a, bundleDir, Options{})
	if err == nil || !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected temporary bundle rejection, got %v", err)
	}
	assertArchiveClean(t, a, 0, 0)
}

func TestSymlinkEscapeIsRejectedWhereSupported(t *testing.T) {
	a := newArchive(t)
	payload := []byte("shape fixture")
	bundleDir, manifest := makeBundle(t, "ready-symlink", map[string][]byte{"mcjava.shape": payload})
	componentPath := filepath.Join(bundleDir, filepath.FromSlash(descriptor(manifest, "mcjava.shape")["path"].(string)))
	if err := os.Remove(componentPath); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, componentPath); err != nil {
		t.Skipf("symlinks are not available on this platform: %v", err)
	}

	_, err := Import(a, bundleDir, Options{})
	if err == nil || !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	assertArchiveClean(t, a, 0, 0)
}

func TestHardLinkToManifestIsRejectedWhereSupported(t *testing.T) {
	a := newArchive(t)
	bundleDir, manifest := makeBundle(t, "ready-hardlink", map[string][]byte{"mcjava.shape": []byte("shape fixture")})
	componentPath := filepath.Join(bundleDir, filepath.FromSlash(descriptor(manifest, "mcjava.shape")["path"].(string)))
	if err := os.Remove(componentPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(bundleDir, "bundle.json"), componentPath); err != nil {
		t.Skipf("hard links are not available on this filesystem: %v", err)
	}

	_, err := Import(a, bundleDir, Options{})
	if err == nil || !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected manifest hard-link rejection, got %v", err)
	}
	assertArchiveClean(t, a, 0, 0)
}

func TestDeleteOnSuccessRunsOnlyAfterCommit(t *testing.T) {
	a := newArchive(t)
	bundleDir, _ := makeBundle(t, "ready-delete", map[string][]byte{"mcjava.shape": []byte("shape fixture")})
	result, err := Import(a, bundleDir, Options{DeleteOnSuccess: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Deleted {
		t.Fatalf("successful cleanup was not reported: %#v", result)
	}
	if _, err := os.Stat(bundleDir); !os.IsNotExist(err) {
		t.Fatalf("bundle still exists after delete-on-success: %v", err)
	}
	assertArchiveClean(t, a, 1, 1)
}

func TestDeleteOnSuccessRefusesBundleContainingArchive(t *testing.T) {
	root := t.TempDir()
	bundleDir := filepath.Join(root, "ready-dangerous")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, manifest := makeBundleAt(t, bundleDir, map[string][]byte{"mcjava.shape": []byte("shape fixture")})
	writeManifest(t, bundleDir, manifest)
	a, err := archive.Init(filepath.Join(bundleDir, "archive"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = Import(a, bundleDir, Options{DeleteOnSuccess: true})
	if err == nil || !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected dangerous cleanup target rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.Root, "VERSION")); err != nil {
		t.Fatalf("archive was touched by rejected cleanup: %v", err)
	}
	assertArchiveClean(t, a, 0, 0)
}

func TestDeleteOnSuccessRefusesBundleInsideArchive(t *testing.T) {
	root := t.TempDir()
	a, err := archive.Init(filepath.Join(root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(a.Root, "incoming", "ready-test")
	makeBundleAt(t, bundleDir, map[string][]byte{"mcjava.shape": []byte("shape fixture")})

	if _, err := Import(a, bundleDir, Options{DeleteOnSuccess: true}); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("Import() error = %v; want ErrInvalidBundle", err)
	}
	if _, err := os.Stat(bundleDir); err != nil {
		t.Fatalf("overlapping bundle was modified: %v", err)
	}
	assertArchiveClean(t, a, 0, 0)
}

func newArchive(t *testing.T) archive.Archive {
	t.Helper()
	a, err := archive.Init(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func makeBundle(t *testing.T, directoryName string, payloads map[string][]byte) (string, map[string]any) {
	t.Helper()
	return makeBundleAt(t, filepath.Join(t.TempDir(), directoryName), payloads)
}

func makeBundleAt(t *testing.T, bundleDir string, payloads map[string][]byte) (string, map[string]any) {
	t.Helper()
	componentsDir := filepath.Join(bundleDir, "components")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(payloads))
	for name := range payloads {
		names = append(names, name)
	}
	sort.Strings(names)
	descriptors := make(map[string]any, len(names))
	for index, name := range names {
		payload := payloads[name]
		filename := "component-" + string(rune('0'+index)) + ".bin"
		if err := os.WriteFile(filepath.Join(componentsDir, filename), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(payload)
		descriptors[name] = map[string]any{
			"path":      "components/" + filename,
			"algorithm": "sha256",
			"digest":    hex.EncodeToString(sum[:]),
			"size":      int64(len(payload)),
		}
	}
	manifest := map[string]any{
		"schema":         Schema,
		"server_id":      "Example.ORG:25565",
		"server_address": "example.org:25565",
		"dimension":      "minecraft:overworld",
		"chunk": map[string]any{
			"x": int32(14),
			"z": int32(-8),
		},
		"observed_at": "2026-08-09T12:00:03.123456Z",
		"protocol":    "minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1",
		"source": map[string]any{
			"contributor": "alice",
			"agent":       "worldledger-fabric/0.1.0-dev",
		},
		"capture": map[string]any{
			"session_id": "5dfe3db2-208e-4cd8-8d11-1d83fa4f951b",
			"sequence":   uint64(417),
			"trigger":    "dirty-flush",
		},
		"components": descriptors,
	}
	writeManifest(t, bundleDir, manifest)
	return bundleDir, manifest
}

func writeManifest(t *testing.T, bundleDir string, manifest map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(bundleDir, "bundle.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func descriptor(manifest map[string]any, name string) map[string]any {
	return manifest["components"].(map[string]any)[name].(map[string]any)
}

func assertArchiveClean(t *testing.T, a archive.Archive, observations, objects int) {
	t.Helper()
	report := a.Check()
	if len(report.Errors) != 0 {
		t.Fatalf("archive is not fsck-clean: %#v", report)
	}
	if report.Observations != observations || report.Objects != objects {
		t.Fatalf("unexpected archive contents: %#v", report)
	}
}
