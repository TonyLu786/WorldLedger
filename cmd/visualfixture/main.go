// Command visualfixture writes a capture bundle whose contents can be checked
// by looking at them in game.
//
//	visualfixture --out ./spool/ready-visual-check
//
// Automated tests prove that canonical bytes round trip. They cannot prove that
// the exported world places blocks where the observation said they were: an axis
// swap, an inverted height, or a palette off-by-one all survive a byte-for-byte
// comparison against an equally wrong expectation. This fixture is arranged so
// each of those mistakes looks different from the outside.
//
// Chunk (0,0), overworld, so the pattern sits at world x 0..15, z 0..15.
//
//	y  67  three oak logs, axis x / y / z, at x = 4, 6, 8 and z = 4
//	y  66  one diamond block at the centre, x 8 z 8
//	y  65  a red line running along +X at z=0, a blue line along +Z at x=0,
//	       an emerald block where they meet at the origin, and a gold block in
//	       the far corner at x 15 z 15
//	y  64  a solid stone floor
//	y -64  a second stone floor with a gold block at the origin, which only
//	       appears if negative section coordinates survive the export
//
// Biomes split down the middle: plains for x 0..7, desert for x 8..15.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/mcprofile"
)

const (
	floorSectionY = 4  // world y 64..79
	deepSectionY  = -4 // world y -64..-49
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	const usage = "usage: visualfixture --out <bundle-dir> [--profile FILE]"
	// The flag package would otherwise answer with the binary's path, which under
	// go run is a temporary build directory, and a bare list of flags.
	fs := flag.NewFlagSet("visualfixture", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "bundle directory to create")
	profilePath := fs.String("profile", filepath.Join("profiles", "minecraft-java-26.2.json"), "release profile used to check every block exists")
	contributor := fs.String("contributor", "visual-check", "contributor id")
	server := fs.String("server", "worldledger-visual-check", "server id")
	// Placing the pattern far from spawn keeps it in a region file an existing
	// world does not have yet, so exporting into that world adds a file instead
	// of replacing one that already holds generated terrain.
	chunkX := fs.Int("chunk-x", 0, "chunk x coordinate")
	chunkZ := fs.Int("chunk-z", 0, "chunk z coordinate")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("%w\n\n%s", err, usage)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q\n\n%s", fs.Arg(0), usage)
	}
	if *out == "" {
		return errors.New(usage)
	}

	profile, err := mcprofile.Load(*profilePath)
	if err != nil {
		return err
	}

	components := map[string][]byte{}

	shape, err := mcjava.EncodeShape(-4, 24)
	if err != nil {
		return err
	}
	components["mcjava.shape"] = shape

	floor, err := encodeSection(profile, floorSectionY, buildFloorSection)
	if err != nil {
		return err
	}
	components[fmt.Sprintf("mcjava.blocks.%d", floorSectionY)] = floor

	deep, err := encodeSection(profile, deepSectionY, buildDeepSection)
	if err != nil {
		return err
	}
	components[fmt.Sprintf("mcjava.blocks.%d", deepSectionY)] = deep

	biomes, err := encodeBiomes(profile, floorSectionY)
	if err != nil {
		return err
	}
	components[fmt.Sprintf("mcjava.biomes.%d", floorSectionY)] = biomes

	return writeBundle(*out, *server, *contributor, int32(*chunkX), int32(*chunkZ), components)
}

// index is the canonical position order: x varies fastest, then z, then y.
func index(x, y, z int) int {
	return (y << 8) | (z << 4) | x
}

func buildFloorSection(states []mcjava.BlockState) {
	fill(states, "minecraft:air")

	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			states[index(x, 0, z)] = mcjava.BlockState{Name: "minecraft:stone"}
		}
	}
	// A red line along +X and a blue line along +Z. If the two horizontal axes
	// are ever swapped, the colours swap with them.
	for x := 0; x < 16; x++ {
		states[index(x, 1, 0)] = mcjava.BlockState{Name: "minecraft:red_concrete"}
	}
	for z := 0; z < 16; z++ {
		states[index(0, 1, z)] = mcjava.BlockState{Name: "minecraft:blue_concrete"}
	}
	states[index(0, 1, 0)] = mcjava.BlockState{Name: "minecraft:emerald_block"}
	states[index(15, 1, 15)] = mcjava.BlockState{Name: "minecraft:gold_block"}
	states[index(8, 2, 8)] = mcjava.BlockState{Name: "minecraft:diamond_block"}

	// Properties have to survive the export; a log with the wrong axis is
	// obvious from any angle.
	for offset, axis := range []string{"x", "y", "z"} {
		states[index(4+offset*2, 3, 4)] = mcjava.BlockState{
			Name:       "minecraft:oak_log",
			Properties: []mcjava.Property{{Name: "axis", Value: axis}},
		}
	}
}

func buildDeepSection(states []mcjava.BlockState) {
	fill(states, "minecraft:air")
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			states[index(x, 0, z)] = mcjava.BlockState{Name: "minecraft:stone"}
		}
	}
	states[index(0, 1, 0)] = mcjava.BlockState{Name: "minecraft:gold_block"}
}

func fill(states []mcjava.BlockState, name string) {
	for position := range states {
		states[position] = mcjava.BlockState{Name: name}
	}
}

func encodeSection(profile mcprofile.Profile, sectionY int32, build func([]mcjava.BlockState)) ([]byte, error) {
	states := make([]mcjava.BlockState, mcjava.BlockCount)
	build(states)
	for _, state := range states {
		if err := profile.CheckBlockState(state); err != nil {
			return nil, fmt.Errorf("section %d: %w", sectionY, err)
		}
	}
	return mcjava.EncodeBlockSection(sectionY, states)
}

func encodeBiomes(profile mcprofile.Profile, sectionY int32) ([]byte, error) {
	biomes := make([]string, mcjava.BiomeCount)
	for y := 0; y < 4; y++ {
		for z := 0; z < 4; z++ {
			for x := 0; x < 4; x++ {
				biome := "minecraft:plains"
				if x >= 2 {
					biome = "minecraft:desert"
				}
				biomes[(y<<4)|(z<<2)|x] = biome
			}
		}
	}
	for _, biome := range biomes {
		if !profile.HasBiome(biome) {
			return nil, fmt.Errorf("biome %s does not exist in %s", biome, profile.Version)
		}
	}
	return mcjava.EncodeBiomeSection(sectionY, biomes)
}

func writeBundle(dir, server, contributor string, chunkX, chunkZ int32, components map[string][]byte) error {
	if err := os.MkdirAll(filepath.Join(dir, "components"), 0o755); err != nil {
		return err
	}

	descriptors := map[string]map[string]any{}
	for name, payload := range components {
		file := sanitize(name) + ".bin"
		if err := os.WriteFile(filepath.Join(dir, "components", file), payload, 0o644); err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		descriptors[name] = map[string]any{
			"path":      "components/" + file,
			"algorithm": "sha256",
			"digest":    hex.EncodeToString(digest[:]),
			"size":      len(payload),
		}
	}

	manifest := map[string]any{
		"schema":      "worldledger.capture-bundle/v1",
		"server_id":   server,
		"dimension":   "minecraft:overworld",
		"chunk":       map[string]int32{"x": chunkX, "z": chunkZ},
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"protocol":    "minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1",
		"source": map[string]string{
			"contributor": contributor,
			"agent":       "visualfixture/0.1.0-dev",
		},
		"components": descriptors,
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), append(encoded, '\n'), 0o644); err != nil {
		return err
	}

	fmt.Printf("wrote %s with %d components\n", dir, len(components))
	for name := range descriptors {
		fmt.Printf("  %s\n", name)
	}
	return nil
}

func sanitize(name string) string {
	out := []rune(name)
	for index, r := range out {
		if r == '.' || r == ':' || r == '/' {
			out[index] = '_'
		}
	}
	return string(out)
}
