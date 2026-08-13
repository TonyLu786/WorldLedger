// Command mcprofile extracts a Minecraft release profile from a client jar.
//
// The profile records what the release can represent: its data version, the
// build range of each dimension, and its block and biome registries. Nothing is
// hand-written, so any release can be profiled by whoever holds its jar.
//
//	mcprofile --jar <client.jar> --out profiles/minecraft-java-26.2.json
//
// Block identifiers come from the names of the blockstate definition files.
// Their contents are not read: those files enumerate the properties that change
// a rendered model and omit the rest, so they cannot describe a block's states.
package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/mcprofile"
)

const (
	blockStatePrefix   = "assets/minecraft/blockstates/"
	biomePrefix        = "data/minecraft/worldgen/biome/"
	dimensionPrefix    = "data/minecraft/dimension_type/"
	structureSetPrefix = "data/minecraft/worldgen/structure_set/"
	versionEntry       = "version.json"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	jarPath := flag.String("jar", "", "Minecraft client jar")
	outPath := flag.String("out", "", "profile output path")
	flag.Parse()
	if *jarPath == "" || *outPath == "" {
		return errors.New("usage: mcprofile --jar <client.jar> --out <profile.json>")
	}

	reader, err := zip.OpenReader(*jarPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	profile := mcprofile.Profile{
		Schema:        mcprofile.Schema,
		Dimensions:    map[string]mcprofile.Dimension{},
		StructureSets: map[string]mcprofile.StructureSet{},
	}

	version, dataVersion, err := readVersion(&reader.Reader)
	if err != nil {
		return err
	}
	profile.Version = version
	profile.DataVersion = dataVersion

	blocks := map[string]struct{}{}
	biomes := map[string]struct{}{}
	for _, file := range reader.File {
		switch {
		case strings.HasPrefix(file.Name, blockStatePrefix) && strings.HasSuffix(file.Name, ".json"):
			relative := strings.TrimPrefix(file.Name, blockStatePrefix)
			if strings.Contains(relative, "/") {
				continue
			}
			blocks["minecraft:"+strings.TrimSuffix(relative, ".json")] = struct{}{}
		case strings.HasPrefix(file.Name, biomePrefix) && strings.HasSuffix(file.Name, ".json"):
			relative := strings.TrimPrefix(file.Name, biomePrefix)
			if strings.Contains(relative, "/") {
				continue
			}
			biomes["minecraft:"+strings.TrimSuffix(relative, ".json")] = struct{}{}
		case strings.HasPrefix(file.Name, dimensionPrefix) && strings.HasSuffix(file.Name, ".json"):
			relative := strings.TrimPrefix(file.Name, dimensionPrefix)
			if strings.Contains(relative, "/") {
				continue
			}
			dimension, err := readDimension(file)
			if err != nil {
				return fmt.Errorf("%s: %w", file.Name, err)
			}
			profile.Dimensions["minecraft:"+strings.TrimSuffix(relative, ".json")] = dimension
		case strings.HasPrefix(file.Name, structureSetPrefix) && strings.HasSuffix(file.Name, ".json"):
			relative := strings.TrimPrefix(file.Name, structureSetPrefix)
			if strings.Contains(relative, "/") {
				continue
			}
			set, err := readStructureSet(file)
			if err != nil {
				return fmt.Errorf("%s: %w", file.Name, err)
			}
			profile.StructureSets["minecraft:"+strings.TrimSuffix(relative, ".json")] = set
		}
	}

	profile.Blocks = sortedKeys(blocks)
	profile.Biomes = sortedKeys(biomes)

	if err := profile.Save(*outPath); err != nil {
		return err
	}
	fmt.Printf("%s data version %d\n", profile.Version, profile.DataVersion)
	fmt.Printf("dimensions %d  blocks %d  biomes %d\n",
		len(profile.Dimensions), len(profile.Blocks), len(profile.Biomes))
	fmt.Println("wrote", *outPath)
	return nil
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func readVersion(reader *zip.Reader) (string, int32, error) {
	file, err := reader.Open(versionEntry)
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", versionEntry, err)
	}
	defer file.Close()
	var payload struct {
		ID           string `json:"id"`
		WorldVersion int32  `json:"world_version"`
	}
	if err := json.NewDecoder(file).Decode(&payload); err != nil {
		return "", 0, fmt.Errorf("%s: %w", versionEntry, err)
	}
	if payload.ID == "" || payload.WorldVersion <= 0 {
		return "", 0, fmt.Errorf("%s does not declare a version", versionEntry)
	}
	return payload.ID, payload.WorldVersion, nil
}

func readStructureSet(file *zip.File) (mcprofile.StructureSet, error) {
	var payload struct {
		Placement struct {
			Type       string `json:"type"`
			Spacing    int32  `json:"spacing"`
			Separation int32  `json:"separation"`
			Salt       int32  `json:"salt"`
			SpreadType string `json:"spread_type"`
		} `json:"placement"`
	}
	if err := decodeJSON(file, &payload); err != nil {
		return mcprofile.StructureSet{}, err
	}
	if payload.Placement.Type == "" {
		return mcprofile.StructureSet{}, errors.New("structure set declares no placement type")
	}

	set := mcprofile.StructureSet{
		Type:       payload.Placement.Type,
		Salt:       payload.Placement.Salt,
		SpreadType: payload.Placement.SpreadType,
	}
	// Only random spread is modelled. Concentric rings, which strongholds use,
	// is a different algorithm, so it is recorded without parameters rather than
	// being made to look usable.
	if payload.Placement.Type == "minecraft:random_spread" {
		set.RandomSpread = true
		set.Spacing = payload.Placement.Spacing
		set.Separation = payload.Placement.Separation
	}
	return set, nil
}

func readDimension(file *zip.File) (mcprofile.Dimension, error) {
	var payload struct {
		MinY   *int32 `json:"min_y"`
		Height *int32 `json:"height"`
	}
	if err := decodeJSON(file, &payload); err != nil {
		return mcprofile.Dimension{}, err
	}
	if payload.MinY == nil || payload.Height == nil {
		return mcprofile.Dimension{}, errors.New("dimension type declares no build range")
	}
	if *payload.MinY%16 != 0 || *payload.Height <= 0 || *payload.Height%16 != 0 {
		return mcprofile.Dimension{}, fmt.Errorf("build range min_y=%d height=%d is not section aligned", *payload.MinY, *payload.Height)
	}
	return mcprofile.Dimension{
		MinSectionY:  *payload.MinY / 16,
		SectionCount: uint32(*payload.Height / 16),
	}, nil
}

func decodeJSON(file *zip.File, target any) error {
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
