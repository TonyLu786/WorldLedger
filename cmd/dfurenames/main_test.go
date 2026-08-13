package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var resourceLocation = regexp.MustCompile(`^[a-z0-9_.-]+:[a-z0-9_./-]+$`)

func committed(t *testing.T) Tables {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "profiles", "renames-26.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tables Tables
	if err := json.Unmarshal(data, &tables); err != nil {
		t.Fatal(err)
	}
	return tables
}

// These are renames with independently known history. They guard against a
// parser change that silently starts pairing the wrong string constants.
func TestCommittedRenamesMatchKnownMinecraftHistory(t *testing.T) {
	tables := committed(t)

	known := []Rename{
		{DataVersion: 1488, From: "minecraft:kelp_top", To: "minecraft:kelp"},
		{DataVersion: 1490, From: "minecraft:melon_block", To: "minecraft:melon"},
		{DataVersion: 1802, From: "minecraft:sign", To: "minecraft:oak_sign"},
		{DataVersion: 2680, From: "minecraft:grass_path", To: "minecraft:dirt_path"},
		{DataVersion: 3692, From: "minecraft:grass", To: "minecraft:short_grass"},
	}
	for _, want := range known {
		found := false
		for _, got := range tables.Blocks {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing known rename %s -> %s at data version %d", want.From, want.To, want.DataVersion)
		}
	}
}

// A rename table built by pairing adjacent string constants can silently pick up
// constants belonging to a neighbouring fixer, which shows up as values that are
// not resource locations at all.
func TestCommittedRenamesContainOnlyResourceLocations(t *testing.T) {
	tables := committed(t)

	for _, group := range [][]Rename{tables.Blocks, tables.Items} {
		for _, rename := range group {
			if !resourceLocation.MatchString(rename.From) {
				t.Errorf("rename source %q is not a resource location", rename.From)
			}
			if !resourceLocation.MatchString(rename.To) {
				t.Errorf("rename target %q is not a resource location", rename.To)
			}
			if rename.DataVersion <= 0 {
				t.Errorf("rename %s -> %s has no data version", rename.From, rename.To)
			}
			if rename.From == rename.To {
				t.Errorf("rename %s maps to itself", rename.From)
			}
		}
	}
}

func TestCommittedRenamesAreUnambiguousPerDataVersion(t *testing.T) {
	tables := committed(t)

	type key struct {
		version int32
		from    string
	}
	seen := map[key]string{}
	for _, rename := range tables.Blocks {
		identity := key{version: rename.DataVersion, from: rename.From}
		if previous, exists := seen[identity]; exists && previous != rename.To {
			t.Errorf("%s maps to both %s and %s at data version %d", rename.From, previous, rename.To, rename.DataVersion)
		}
		seen[identity] = rename.To
	}
}

// Coverage is part of the artifact. A run that quietly claims to have extracted
// everything would be more dangerous than one that names its gaps.
func TestCommittedCoverageIsReportedHonestly(t *testing.T) {
	tables := committed(t)

	if tables.Schema != Schema {
		t.Fatalf("schema = %q", tables.Schema)
	}
	if tables.Coverage.BlockFixers == 0 || tables.Coverage.BlockExtracted == 0 {
		t.Fatal("coverage counts are missing")
	}
	if tables.Coverage.BlockExtracted > tables.Coverage.BlockFixers {
		t.Fatalf("extracted %d of %d block fixers", tables.Coverage.BlockExtracted, tables.Coverage.BlockFixers)
	}
	if tables.Coverage.BlockExtracted == tables.Coverage.BlockFixers {
		t.Fatal("full coverage is not achievable: at least one fixer is built from a lambda and must be listed as unextracted")
	}
	if len(tables.Coverage.Unextracted) == 0 {
		t.Fatal("fixers were skipped but none were named")
	}
}
