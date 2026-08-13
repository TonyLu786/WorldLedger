package mcjava_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcjava/fixture"
)

type goldenIdentity struct {
	file   string
	size   int
	sha256 string
}

var goldenIdentities = map[string]goldenIdentity{
	"biomes-mixed-negative":      {"biomes-mixed-negative.bin", 263, "a7bcadfdbd655e031ddbf243cecc92c35a6bc073d7c1b58f0b8d4b4395c2dcc4"},
	"block-entities-empty":       {"block-entities-empty.bin", 52, "8966ca92974146f8d58c2c3337de38c3b8f38fdda4c6d45595c37311b8727a77"},
	"block-entities-nbt-special": {"block-entities-nbt-special.bin", 511, "f876a04a4cee18d17353c6bda41e37033c0dc3d072a9e9ba7f57235653d760a9"},
	"blocks-all-air-negative":    {"blocks-all-air-negative.bin", 8262, "ed0cd3cbaa8a1165700c575cfaffe8cf3f58bee5602a3ba5ab3c0db78ea7f49f"},
	"blocks-high-palette":        {"blocks-high-palette.bin", 98357, "6c7392f64d1ad9a6db478dc57123ff7c03589af6ab612078093ea00f1ea85af7"},
	"blocks-property-order":      {"blocks-property-order.bin", 8328, "9a3dfa6ceea8ddc986f6e9e3381cf6db217365f52cf5f6822966b4716768d8f5"},
	"shape-negative":             {"shape-negative.bin", 53, "a2fbcf1cd1560c698239c9a93aba9aa24995954ed7f00acf3bbb92528337a5db"},
}

type committedManifest struct {
	Schema   string                   `json:"schema"`
	Source   string                   `json:"source"`
	Fixtures []committedManifestEntry `json:"fixtures"`
}

type committedManifestEntry struct {
	Name      string `json:"name"`
	Component string `json:"component"`
	File      string `json:"file"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
}

func TestCommittedGoldenFixtures(t *testing.T) {
	directory := filepath.Join("..", "..", "testdata", "mcjava-v1")
	set, err := fixture.Load(filepath.Join(directory, "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Fixtures) != len(goldenIdentities) {
		t.Fatalf("fixture count = %d; hard-coded identity count = %d", len(set.Fixtures), len(goldenIdentities))
	}

	manifestData, err := os.ReadFile(filepath.Join(directory, "outputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest committedManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "worldledger.minecraft.java.chunk-fixture-outputs/v1" || manifest.Source != "fixtures.json" {
		t.Fatalf("unexpected output manifest header: %#v", manifest)
	}
	manifestByName := make(map[string]committedManifestEntry, len(manifest.Fixtures))
	for _, entry := range manifest.Fixtures {
		if _, exists := manifestByName[entry.Name]; exists {
			t.Fatalf("duplicate output manifest entry %q", entry.Name)
		}
		manifestByName[entry.Name] = entry
	}

	seen := make(map[string]struct{}, len(set.Fixtures))
	for _, item := range set.Fixtures {
		item := item
		t.Run(item.Name, func(t *testing.T) {
			expected, exists := goldenIdentities[item.Name]
			if !exists {
				t.Fatalf("fixture has no hard-coded identity")
			}
			seen[item.Name] = struct{}{}
			if item.Output != expected.file {
				t.Fatalf("output = %q; want %q", item.Output, expected.file)
			}

			first, err := fixture.Build(item)
			if err != nil {
				t.Fatal(err)
			}
			second, err := fixture.Build(item)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Fatal("two consecutive builds differ")
			}

			committed, err := os.ReadFile(filepath.Join(directory, expected.file))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, committed) {
				t.Fatal("reference output differs from committed golden bytes")
			}
			digest := sha256.Sum256(committed)
			gotDigest := hex.EncodeToString(digest[:])
			if len(committed) != expected.size || gotDigest != expected.sha256 {
				t.Fatalf("committed identity = (%d, %s); want (%d, %s)", len(committed), gotDigest, expected.size, expected.sha256)
			}

			entry, exists := manifestByName[item.Name]
			if !exists {
				t.Fatal("fixture is absent from outputs.json")
			}
			if entry.Component != item.Component || entry.File != expected.file || entry.Size != expected.size || entry.SHA256 != expected.sha256 {
				t.Fatalf("outputs.json entry does not match hard-coded identity: %#v", entry)
			}
		})
	}
	for name := range goldenIdentities {
		if _, exists := seen[name]; !exists {
			t.Errorf("hard-coded identity %q has no fixture", name)
		}
	}
	if len(manifestByName) != len(goldenIdentities) {
		t.Fatalf("outputs.json count = %d; want %d", len(manifestByName), len(goldenIdentities))
	}
}
