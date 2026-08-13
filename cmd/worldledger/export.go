package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/anvil"
	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/epoch"
	"github.com/worldledger/worldledger-mc/internal/translate"
)

func snapshotAt(a archive.Archive, server, dimension, moment string) (epoch.Snapshot, error) {
	at := time.Now().UTC()
	if moment != "" {
		parsed, err := time.Parse(time.RFC3339Nano, moment)
		if err != nil {
			return epoch.Snapshot{}, fmt.Errorf("--at must be an RFC3339 timestamp: %w", err)
		}
		at = parsed.UTC()
	}
	gathered, err := a.DimensionObservations(server, dimension)
	if err != nil {
		return epoch.Snapshot{}, err
	}
	inputs := make([]epoch.ChunkInput, 0, len(gathered))
	for _, entry := range gathered {
		inputs = append(inputs, epoch.ChunkInput{Chunk: entry.Chunk, Observations: entry.Observations})
	}
	return epoch.BuildSnapshot(server, dimension, at, inputs), nil
}

func cmdCoverage(args []string) error {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	dimension := fs.String("dimension", "minecraft:overworld", "dimension id")
	moment := fs.String("at", "", "RFC3339 reconstruction time (default now)")
	asJSON := fs.Bool("json", false, "emit the full per-chunk snapshot as JSON")
	mapPath := fs.String("map", "", "write a PNG coverage map, one pixel per chunk")
	scale := fs.Int("map-scale", 4, "pixels per chunk in the map")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" {
		return errors.New("usage: worldledger coverage --archive DIR --server ID --dimension DIM [--at TIME] [--json] [--map FILE]")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	snapshot, err := snapshotAt(a, *server, *dimension, *moment)
	if err != nil {
		return err
	}

	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(snapshot)
	}

	summary := snapshot.Summary
	fmt.Printf("server        %s\n", snapshot.Server)
	fmt.Printf("dimension     %s\n", snapshot.Dimension)
	fmt.Printf("at            %s\n", snapshot.At.Format(time.RFC3339Nano))
	fmt.Printf("policy        %s\n\n", snapshot.Policy)
	fmt.Printf("chunks        %d\n", summary.Chunks)
	fmt.Printf("corroborated  %d\n", summary.Corroborated)
	fmt.Printf("single-source %d\n", summary.SingleSource)
	fmt.Printf("superseded    %d  (changed over time; later state used)\n", summary.Superseded)
	fmt.Printf("conflict      %d  (disagreement too close in time to be a change)\n", summary.Conflict)
	fmt.Printf("unknown       %d\n", summary.Unknown)

	if summary.Conflict > 0 {
		fmt.Println("\nchunks where contributors disagree about the same moment:")
		for _, selection := range snapshot.Selections {
			if selection.Status != epoch.StatusConflict {
				continue
			}
			fmt.Printf("  (%d,%d) used %s from %v; %d other state(s) preserved\n",
				selection.Chunk.X, selection.Chunk.Z,
				selection.Selected.StateDigest[:12], selection.Contributors, len(selection.Rejected))
		}
	}

	if *mapPath != "" {
		if err := snapshot.RenderPNG(*mapPath, *scale); err != nil {
			return err
		}
		minX, minZ, maxX, maxZ, _ := snapshot.Bounds()
		fmt.Printf("\nwrote %s covering chunks x %d..%d, z %d..%d\n", *mapPath, minX, maxX, minZ, maxZ)
		fmt.Println("  green corroborated   blue single-source   amber superseded   red conflict")
		fmt.Println("  background is chunks with nothing observed at that time, not chunks of air")
	}
	return nil
}

type worldRequest struct {
	archivePath string
	server      string
	dimension   string
	moment      string
	into        string
	overwrite   bool
}

func (r worldRequest) plan() (archive.Archive, epoch.Snapshot, []anvil.PreparedChunk, error) {
	a, err := archive.Open(r.archivePath)
	if err != nil {
		return archive.Archive{}, epoch.Snapshot{}, nil, err
	}
	snapshot, err := snapshotAt(a, r.server, r.dimension, r.moment)
	if err != nil {
		return archive.Archive{}, epoch.Snapshot{}, nil, err
	}

	sources := make([]anvil.ChunkSource, 0, len(snapshot.Selections))
	for _, selection := range snapshot.Selections {
		if !selection.Known() {
			continue
		}
		sources = append(sources, anvil.ChunkSource{Chunk: selection.Chunk, Observation: *selection.Selected})
	}
	if len(sources) == 0 {
		return archive.Archive{}, epoch.Snapshot{}, nil, fmt.Errorf("no chunk has an observation at or before %s", snapshot.At.Format(time.RFC3339Nano))
	}

	prepared, err := anvil.Prepare(a.CAS, sources)
	if err != nil {
		return archive.Archive{}, epoch.Snapshot{}, nil, err
	}
	return a, snapshot, prepared, nil
}

func (r worldRequest) write(snapshot epoch.Snapshot, prepared []anvil.PreparedChunk, dataVersion int32) error {
	report, err := anvil.Export(prepared, anvil.ExportRequest{
		WorldDir:    r.into,
		Dimension:   snapshot.Dimension,
		DataVersion: dataVersion,
		Overwrite:   r.overwrite,
	})
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d chunks into %d region file(s)\n", report.Chunks, len(report.RegionFiles))
	for _, path := range report.RegionFiles {
		fmt.Printf("  %s\n", path)
	}
	fmt.Printf("\ncorroborated %d  single-source %d  superseded %d  conflict %d\n",
		snapshot.Summary.Corroborated, snapshot.Summary.SingleSource,
		snapshot.Summary.Superseded, snapshot.Summary.Conflict)
	if snapshot.Summary.Unknown > 0 {
		fmt.Printf("%d chunk(s) had no observation at that time and were left unwritten rather than filled with air\n", snapshot.Summary.Unknown)
	}
	if snapshot.Summary.Conflict > 0 {
		fmt.Printf("%d chunk(s) were resolved by falling back to the most recent state; every rejected state remains in the archive\n", snapshot.Summary.Conflict)
	}
	return nil
}

func bindWorldFlags(fs *flag.FlagSet, request *worldRequest) {
	fs.StringVar(&request.archivePath, "archive", "", "archive directory")
	fs.StringVar(&request.server, "server", "", "stable server id")
	fs.StringVar(&request.dimension, "dimension", "minecraft:overworld", "dimension id")
	fs.StringVar(&request.moment, "at", "", "RFC3339 reconstruction time (default now)")
	fs.StringVar(&request.into, "into", "", "existing Minecraft world directory")
	fs.BoolVar(&request.overwrite, "overwrite", false, "replace existing region files")
}

// cmdExport writes the observed state unchanged. It never approximates, so the
// result is only readable by a release that already understands everything the
// capture saw.
func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var request worldRequest
	bindWorldFlags(fs, &request)
	dataVersion := fs.Int("data-version", anvil.DataVersion26_2, "data version to stamp, matching the captured release")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if request.archivePath == "" || request.server == "" || request.into == "" {
		return errors.New("usage: worldledger export --archive DIR --server ID --dimension DIM --into WORLD_DIR [--at TIME] [--overwrite]")
	}

	a, snapshot, prepared, err := request.plan()
	if err != nil {
		return err
	}
	if err := requirePolicy(a, request.server); err != nil {
		return err
	}
	printCompatibilityNotice(snapshot, prepared, int32(*dataVersion))
	return request.write(snapshot, prepared, int32(*dataVersion))
}

// cmdConvert writes a downgraded copy into a separate world. It is deliberately
// a different command from export: it approximates, and that must be something
// the operator asked for rather than something an export quietly did.
func cmdConvert(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var request worldRequest
	bindWorldFlags(fs, &request)
	targetProfile := fs.String("target-profile", "", "release profile to convert into, from cmd/mcprofile")
	rulesPath := fs.String("rules", "", "rename and substitution rules")
	policyName := fs.String("on-unrepresentable", string(translate.PolicySkipChunk), "report, skip-chunk, or fill")
	filler := fs.String("filler", translate.DefaultFiller, "block written when no rule covers a state")
	fillerBiome := fs.String("filler-biome", translate.DefaultBiomeFiller, "biome written when no rule covers one")
	keepBlockEntities := fs.Bool("keep-block-entities", false, "carry block entity data across releases unchanged")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if request.archivePath == "" || request.server == "" || request.into == "" || *targetProfile == "" {
		return errors.New("usage: worldledger convert --archive DIR --server ID --dimension DIM --into WORLD_DIR --target-profile FILE [--rules FILE] [--on-unrepresentable POLICY]")
	}

	a, snapshot, prepared, err := request.plan()
	if err != nil {
		return err
	}
	if err := requirePolicy(a, request.server); err != nil {
		return err
	}
	fmt.Printf("converting into a separate world at %s\n", request.into)
	fmt.Println("the faithful export is untouched; this copy is an approximation")
	fmt.Println()

	prepared, dataVersion, err := translateForTarget(prepared, snapshot.Dimension, translationOptions{
		profilePath:       *targetProfile,
		rulesPath:         *rulesPath,
		policyName:        *policyName,
		filler:            *filler,
		fillerBiome:       *fillerBiome,
		keepBlockEntities: *keepBlockEntities,
	})
	if err != nil {
		return err
	}
	if len(prepared) == 0 {
		return errors.New("conversion left no chunk that the target release can represent")
	}
	return request.write(snapshot, prepared, dataVersion)
}

// printCompatibilityNotice states up front what will read the result. Minecraft
// upgrades an older world forward on its own, so a newer client is safe; there
// is no path backwards, which is what convert exists for.
func printCompatibilityNotice(snapshot epoch.Snapshot, prepared []anvil.PreparedChunk, dataVersion int32) {
	releases := observedReleases(snapshot)
	fmt.Printf("This export is written at data version %d", dataVersion)
	if len(releases) > 0 {
		fmt.Printf(", captured from Minecraft %s", strings.Join(releases, ", "))
	}
	fmt.Println(".")
	fmt.Println()
	fmt.Println("  Open it with that release or newer. A newer release upgrades the world by itself.")
	fmt.Println("  An older release cannot read it at all. Use `worldledger convert` to write a")
	fmt.Println("  downgraded copy into a separate world.")

	if namespaces := modNamespaces(prepared); len(namespaces) > 0 {
		fmt.Println()
		fmt.Printf("  This export contains state from %d non-vanilla namespace(s). The client that\n", len(namespaces))
		fmt.Println("  opens it needs the matching mods installed, or those blocks will not load:")
		for _, namespace := range namespaces {
			fmt.Printf("    %s\n", namespace)
		}
	}
	fmt.Println()
}

func observedReleases(snapshot epoch.Snapshot) []string {
	seen := map[string]struct{}{}
	for _, selection := range snapshot.Selections {
		if selection.Selected == nil {
			continue
		}
		protocol := selection.Selected.Protocol
		if !strings.HasPrefix(protocol, "minecraft-java/") {
			continue
		}
		release := strings.TrimPrefix(protocol, "minecraft-java/")
		if separator := strings.IndexByte(release, ';'); separator >= 0 {
			release = release[:separator]
		}
		if release != "" {
			seen[release] = struct{}{}
		}
	}
	return sortedSet(seen)
}

// modNamespaces reports every namespace other than minecraft that appears in the
// exported state.
func modNamespaces(prepared []anvil.PreparedChunk) []string {
	seen := map[string]struct{}{}
	record := func(value string) {
		namespace, _, found := strings.Cut(value, ":")
		if found && namespace != "minecraft" {
			seen[namespace] = struct{}{}
		}
	}
	for _, entry := range prepared {
		for _, section := range entry.Components.Blocks {
			for _, state := range section.Palette {
				name := state
				if bracket := strings.IndexByte(name, '['); bracket >= 0 {
					name = name[:bracket]
				}
				record(name)
			}
		}
		for _, section := range entry.Components.Biomes {
			for _, biome := range section.Palette {
				record(biome)
			}
		}
		for _, blockEntity := range entry.Components.BlockEntities {
			record(blockEntity.Type)
		}
	}
	return sortedSet(seen)
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
