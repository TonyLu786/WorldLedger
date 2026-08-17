// Command mcprofile extracts a Minecraft release profile from a client jar, and
// compares two of them.
//
// The profile records what the release can represent: its data version, the
// build range of each dimension, and its block and biome registries. Nothing is
// hand-written, so any release can be profiled by whoever holds its jar.
//
//	mcprofile --jar <client.jar> --out profiles/minecraft-java-26.2.json
//	mcprofile --from profiles/minecraft-java-1.21.11.json --to profiles/minecraft-java-26.2.json
//
// The second form is for a Minecraft upgrade. The capture fingerprint is
// committed, so a release that changes what the game reports fails the build,
// but the failure only says something moved. Comparing the two releases says
// what, and separates what the new release merely adds from what bears on
// observations already captured.
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

const usage = `usage:
  mcprofile --jar <client.jar> --out <profile.json>   extract a profile from a release
  mcprofile --from <a.json> --to <b.json>             report what changed between two`

func run() error {
	// The flag package's own error output names the binary by its path, which
	// under go run is a temporary build directory, and prints a bare list of
	// flags. Discarding it and answering here keeps the two modes visible, the
	// same way every worldledger command does.
	fs := flag.NewFlagSet("mcprofile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jarPath := fs.String("jar", "", "Minecraft client jar")
	outPath := fs.String("out", "", "profile output path")
	fromPath := fs.String("from", "", "profile to compare from")
	toPath := fs.String("to", "", "profile to compare to")
	if err := fs.Parse(os.Args[1:]); err != nil {
		// Being asked for help is not a failure, so it answers on stdout and
		// exits zero. An unknown flag is, and it names the flag before the usage
		// rather than only listing what was allowed.
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("%w\n\n%s", err, usage)
	}
	// Every input is named by a flag, so a bare argument is somebody guessing a
	// positional form. Ignoring it and reporting success would mean exiting zero
	// having done nothing they asked for.
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q\n\n%s", fs.Arg(0), usage)
	}

	switch {
	case *fromPath != "" && *toPath != "":
		if *jarPath != "" || *outPath != "" {
			return errors.New("--from/--to compares existing profiles and does not read a jar")
		}
		return compare(*fromPath, *toPath)
	case *fromPath != "" || *toPath != "":
		return errors.New("comparing needs both --from and --to")
	case *jarPath == "" && *outPath == "" && fs.NFlag() == 0:
		// Being asked how to use it is not a failure, so this goes to stdout and
		// exits zero, the same rule the worldledger command follows.
		fmt.Println(usage)
		return nil
	case *jarPath == "" || *outPath == "":
		return errors.New("extracting needs both --jar and --out\n\n" + usage)
	}

	return extract(*jarPath, *outPath)
}

func extract(jarPath, outPath string) error {
	reader, err := zip.OpenReader(jarPath)
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

	if err := profile.Save(outPath); err != nil {
		return err
	}
	fmt.Printf("%s data version %d\n", profile.Version, profile.DataVersion)
	fmt.Printf("dimensions %d  blocks %d  biomes %d\n",
		len(profile.Dimensions), len(profile.Blocks), len(profile.Biomes))
	fmt.Println("wrote", outPath)
	return nil
}

func compare(fromPath, toPath string) error {
	from, err := mcprofile.Load(fromPath)
	if err != nil {
		return err
	}
	to, err := mcprofile.Load(toPath)
	if err != nil {
		return err
	}
	printDelta(mcprofile.Compare(from, to))
	return nil
}

func printDelta(delta mcprofile.Delta) {
	direction := "going backwards"
	if delta.Forward() {
		direction = "an upgrade"
	}
	if delta.FromDataVersion == delta.ToDataVersion {
		direction = "the same data version"
	}
	fmt.Printf("%s (data version %d) to %s (data version %d), %s\n\n",
		delta.From, delta.FromDataVersion, delta.To, delta.ToDataVersion, direction)

	if delta.Empty() {
		fmt.Println("The two releases represent exactly the same things.")
		return
	}

	// What bears on data that already exists comes first, because it is the part
	// somebody has to act on.
	if delta.TouchesExistingArchives() {
		fmt.Println("Bears on observations already captured")
		printNames("  blocks the target cannot place", delta.BlocksRemoved)
		printNames("  biomes the target cannot place", delta.BiomesRemoved)
		printNames("  dimensions that are gone", delta.DimensionsRemoved)
		for _, change := range delta.DimensionsChanged {
			if change.Narrowed() {
				fmt.Printf("  build range narrowed  %s  %s -> %s\n", change.ID, change.From.Describe(), change.To.Describe())
			}
		}
		printNames("  structure sets that are gone", delta.StructureSetsRemoved)
		for _, change := range delta.StructureSetsChanged {
			fmt.Printf("  structure placement changed  %s\n", change.ID)
			printStructureSet("    from", change.From)
			printStructureSet("    to  ", change.To)
		}
		fmt.Println()
	} else {
		fmt.Println("Nothing changed that bears on observations already captured.")
		fmt.Println()
	}

	added := len(delta.BlocksAdded) + len(delta.BiomesAdded) +
		len(delta.DimensionsAdded) + len(delta.StructureSetsAdded)
	for _, change := range delta.DimensionsChanged {
		if !change.Narrowed() {
			added++
		}
	}
	// Said rather than left to inference. A report that simply stops is one the
	// reader has to guess the end of.
	if added == 0 {
		fmt.Println("The target represents nothing the source did not.")
		return
	}

	printNames("Newly representable blocks", delta.BlocksAdded)
	printNames("Newly representable biomes", delta.BiomesAdded)
	printNames("New dimensions", delta.DimensionsAdded)
	printNames("New structure sets", delta.StructureSetsAdded)
	for _, change := range delta.DimensionsChanged {
		if !change.Narrowed() {
			fmt.Printf("Build range widened  %s  %s -> %s\n", change.ID, change.From.Describe(), change.To.Describe())
		}
	}
}

func printNames(heading string, names []string) {
	if len(names) == 0 {
		return
	}
	fmt.Printf("%s (%d)\n", heading, len(names))
	for _, name := range names {
		fmt.Println("   ", name)
	}
}

func printStructureSet(label string, set mcprofile.StructureSet) {
	if set.RandomSpread {
		fmt.Printf("%s %s spacing %d separation %d salt %d\n", label, set.Type, set.Spacing, set.Separation, set.Salt)
		return
	}
	fmt.Printf("%s %s salt %d\n", label, set.Type, set.Salt)
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
