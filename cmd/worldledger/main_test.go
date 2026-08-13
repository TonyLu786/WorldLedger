package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/bundle"
)

func TestIngestBundleCommandDeletesOnlyAfterSuccess(t *testing.T) {
	archiveDir := filepath.Join(t.TempDir(), "archive")
	a, err := archive.Init(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := writeCLIBundle(t)

	if err := run([]string{"ingest-bundle", "--archive", archiveDir, "--delete-on-success", bundleDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundleDir); !os.IsNotExist(err) {
		t.Fatalf("successfully imported bundle was not deleted: %v", err)
	}
	report := a.Check()
	if report.Observations != 1 || report.Objects != 1 || len(report.Errors) != 0 {
		t.Fatalf("unexpected archive after CLI import: %#v", report)
	}
}

func TestIngestBundleCommandKeepsRejectedBundle(t *testing.T) {
	archiveDir := filepath.Join(t.TempDir(), "archive")
	a, err := archive.Init(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := writeCLIBundle(t)
	manifestPath := filepath.Join(bundleDir, "bundle.json")
	var manifest map[string]any
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	components := manifest["components"].(map[string]any)
	descriptor := components["mcjava.shape"].(map[string]any)
	descriptor["digest"] = "0000000000000000000000000000000000000000000000000000000000000000"
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"ingest-bundle", "--archive", archiveDir, "--delete-on-success", bundleDir}); err == nil {
		t.Fatal("expected CLI import to reject wrong digest")
	}
	if _, err := os.Stat(bundleDir); err != nil {
		t.Fatalf("rejected bundle was deleted: %v", err)
	}
	report := a.Check()
	if report.Observations != 0 || report.Objects != 0 || len(report.Errors) != 0 {
		t.Fatalf("rejected CLI import dirtied archive: %#v", report)
	}
}

func writeCLIBundle(t *testing.T) string {
	t.Helper()
	bundleDir := filepath.Join(t.TempDir(), "ready-cli")
	componentsDir := filepath.Join(bundleDir, "components")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("shape fixture")
	if err := os.WriteFile(filepath.Join(componentsDir, "shape.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	manifest := map[string]any{
		"schema":      bundle.Schema,
		"server_id":   "example.org:25565",
		"dimension":   "minecraft:overworld",
		"chunk":       map[string]any{"x": 14, "z": -8},
		"observed_at": "2026-08-09T12:00:03.123456Z",
		"protocol":    "minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1",
		"source":      map[string]any{"contributor": "alice"},
		"components": map[string]any{
			"mcjava.shape": map[string]any{
				"path":      "components/shape.bin",
				"algorithm": "sha256",
				"digest":    hex.EncodeToString(sum[:]),
				"size":      len(payload),
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "bundle.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return bundleDir
}
