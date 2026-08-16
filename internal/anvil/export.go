package anvil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/model"
)

const (
	componentShape         = "mcjava.shape"
	componentBlockEntities = "mcjava.block_entities"
	componentBlocksPrefix  = "mcjava.blocks."
	componentBiomesPrefix  = "mcjava.biomes."
)

// ObjectSource reads canonical component bytes. It is satisfied by cas.Store.
type ObjectSource interface {
	Open(ref model.BlobRef) (*os.File, error)
}

type ChunkSource struct {
	Chunk       model.ChunkRef
	Observation model.Observation
}

// PreparedChunk is decoded canonical state, ready to write. Loading is separate
// from writing so a caller can rewrite the state for another release in
// between.
type PreparedChunk struct {
	Chunk      model.ChunkRef
	Components ChunkComponents
}

func Prepare(source ObjectSource, chunks []ChunkSource) ([]PreparedChunk, error) {
	prepared := make([]PreparedChunk, 0, len(chunks))
	for _, entry := range chunks {
		components, err := LoadComponents(source, entry.Observation)
		if err != nil {
			return nil, fmt.Errorf("chunk (%d,%d): %w", entry.Chunk.X, entry.Chunk.Z, err)
		}
		prepared = append(prepared, PreparedChunk{Chunk: entry.Chunk, Components: components})
	}
	return prepared, nil
}

type ExportRequest struct {
	WorldDir    string
	Dimension   string
	DataVersion int32
	Overwrite   bool
}

type ExportReport struct {
	RegionFiles []string `json:"region_files"`
	Chunks      int      `json:"chunks"`
}

// Export writes region files into an existing world directory. It deliberately
// does not create the world: level.dat carries the data version, generator, and
// build height, and fabricating those is how an export ends up silently
// upgraded or misaligned against the client that reads it.
func Export(chunks []PreparedChunk, request ExportRequest) (ExportReport, error) {
	regionDir, err := DimensionDirectory(request.WorldDir, request.Dimension)
	if err != nil {
		return ExportReport{}, err
	}
	if err := requireExistingWorld(request.WorldDir); err != nil {
		return ExportReport{}, err
	}

	regions := map[[2]int32]*Region{}
	for _, entry := range chunks {
		chunk, err := BuildChunk(entry.Chunk.X, entry.Chunk.Z, request.DataVersion, entry.Components)
		if err != nil {
			return ExportReport{}, err
		}
		regionX, regionZ := RegionOf(entry.Chunk.X, entry.Chunk.Z)
		key := [2]int32{regionX, regionZ}
		region, exists := regions[key]
		if !exists {
			region = NewRegion(regionX, regionZ)
			regions[key] = region
		}
		if err := region.AddChunk(entry.Chunk.X, entry.Chunk.Z, chunk); err != nil {
			return ExportReport{}, err
		}
	}

	keys := make([][2]int32, 0, len(regions))
	for key := range regions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] == keys[j][0] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})

	// Every target is checked before anything is written, so a refusal cannot
	// leave a half-exported dimension behind.
	paths := make([]string, 0, len(keys))
	for _, key := range keys {
		path := filepath.Join(regionDir, RegionFileName(key[0], key[1]))
		if !request.Overwrite {
			if _, err := os.Stat(path); err == nil {
				return ExportReport{}, fmt.Errorf("%s already exists; pass --overwrite to replace it", path)
			} else if !os.IsNotExist(err) {
				return ExportReport{}, err
			}
		}
		paths = append(paths, path)
	}

	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return ExportReport{}, err
	}
	report := ExportReport{RegionFiles: paths, Chunks: len(chunks)}
	for index, key := range keys {
		if err := writeFileAtomic(paths[index], regions[key].Bytes()); err != nil {
			return ExportReport{}, err
		}
	}
	return report, nil
}

// DimensionDirectory resolves the region directory for a dimension inside an
// existing world.
//
// Java Edition has used two layouts. The current one puts every dimension under
// dimensions/<namespace>/<path>/region, including the three vanilla ones.
// Older releases put the overworld directly in region/ and the nether and end in
// DIM-1/ and DIM1/. Rather than guess from a version number, the world is asked:
// it was created by the client that will read it, so whichever layout it already
// has is the correct one. A world with neither gets the current layout.
func DimensionDirectory(worldDir, dimension string) (string, error) {
	namespace, path, err := splitDimension(dimension)
	if err != nil {
		return "", err
	}

	segments := append([]string{worldDir, "dimensions", namespace}, strings.Split(path, "/")...)
	current := filepath.Join(segments...)
	if isDirectory(current) {
		return filepath.Join(current, "region"), nil
	}
	if legacy, ok := legacyRegionDirectory(worldDir, namespace, path); ok && isDirectory(legacy) {
		return legacy, nil
	}
	return filepath.Join(current, "region"), nil
}

// legacyRegionDirectory reports where a pre-dimensions release kept a vanilla
// dimension's regions. It returns the region directory itself: the legacy
// overworld lived at the top of the world, so testing its parent would match
// every world in existence. Modded dimensions never had a legacy location.
func legacyRegionDirectory(worldDir, namespace, path string) (string, bool) {
	if namespace != "minecraft" {
		return "", false
	}
	switch path {
	case "overworld":
		return filepath.Join(worldDir, "region"), true
	case "the_nether":
		return filepath.Join(worldDir, "DIM-1", "region"), true
	case "the_end":
		return filepath.Join(worldDir, "DIM1", "region"), true
	}
	return "", false
}

func splitDimension(dimension string) (string, string, error) {
	normalized := model.NormalizeToken(dimension)
	namespace, path, found := strings.Cut(normalized, ":")
	if !found || strings.Contains(path, ":") {
		return "", "", fmt.Errorf("dimension %q must be a namespaced resource location", dimension)
	}
	if namespace == "" || path == "" {
		return "", "", fmt.Errorf("dimension %q is not a valid resource location", dimension)
	}
	for _, segment := range append(strings.Split(path, "/"), namespace) {
		if segment == "" || segment == "." || segment == ".." {
			return "", "", fmt.Errorf("dimension %q contains an unsafe path segment", dimension)
		}
	}
	return namespace, path, nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// requireExistingWorld refuses to write into anything that is not already a
// world.
//
// Export deliberately never generates level.dat: a world's seed, generator,
// version and game rules are server state that was never observed, and writing
// a plausible one would be inventing exactly the kind of data this project
// refuses to invent. The consequence is that a target world has to exist first,
// which is the last step of the path and the easiest place to be stranded, so
// the message says what to do rather than naming an internal file.
func requireExistingWorld(worldDir string) error {
	if worldDir == "" {
		return fmt.Errorf("a target world directory is required")
	}
	info, err := os.Stat(filepath.Join(worldDir, "level.dat"))
	if os.IsNotExist(err) {
		return fmt.Errorf(
			"%s is not a Minecraft world yet, and export writes into one rather than creating it.\n\n"+
				"WorldLedger never invents a world's seed, generator or game rules, because nobody\n"+
				"observed them. So make the empty world first, then export into it:\n\n"+
				"  1. In Minecraft: Singleplayer, Create New World, name it, Create.\n"+
				"  2. Leave the world and quit to the title screen.\n"+
				"  3. Point --into at that world's folder, which is\n"+
				"     .minecraft/saves/<the name you chose>\n\n"+
				"Observed chunks are written over the empty terrain; anything nobody saw is\n"+
				"left untouched rather than filled in.",
			worldDir)
	}
	if err != nil {
		return fmt.Errorf("cannot read the world at %s: %w", worldDir, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s contains a level.dat directory rather than a level.dat file", worldDir)
	}
	return nil
}

func LoadComponents(source ObjectSource, observation model.Observation) (ChunkComponents, error) {
	components := ChunkComponents{
		Blocks: map[int32]mcjava.BlockSection{},
		Biomes: map[int32]mcjava.BiomeSection{},
	}
	names := make([]string, 0, len(observation.Components))
	for name := range observation.Components {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ref := observation.Components[name]
		data, err := readObject(source, ref)
		if err != nil {
			return ChunkComponents{}, fmt.Errorf("component %q: %w", name, err)
		}
		switch {
		case name == componentShape:
			shape, err := mcjava.DecodeShape(data)
			if err != nil {
				return ChunkComponents{}, fmt.Errorf("component %q: %w", name, err)
			}
			components.Shape = shape
		case name == componentBlockEntities:
			entries, err := mcjava.DecodeBlockEntities(data)
			if err != nil {
				return ChunkComponents{}, fmt.Errorf("component %q: %w", name, err)
			}
			components.BlockEntities = entries
			components.HasBlockEntities = true
		case strings.HasPrefix(name, componentBlocksPrefix):
			sectionY, err := parseSectionY(name, componentBlocksPrefix)
			if err != nil {
				return ChunkComponents{}, err
			}
			section, err := mcjava.DecodeBlockSection(data)
			if err != nil {
				return ChunkComponents{}, fmt.Errorf("component %q: %w", name, err)
			}
			if section.SectionY != sectionY {
				return ChunkComponents{}, fmt.Errorf("component %q encodes section %d", name, section.SectionY)
			}
			components.Blocks[sectionY] = section
		case strings.HasPrefix(name, componentBiomesPrefix):
			sectionY, err := parseSectionY(name, componentBiomesPrefix)
			if err != nil {
				return ChunkComponents{}, err
			}
			section, err := mcjava.DecodeBiomeSection(data)
			if err != nil {
				return ChunkComponents{}, fmt.Errorf("component %q: %w", name, err)
			}
			if section.SectionY != sectionY {
				return ChunkComponents{}, fmt.Errorf("component %q encodes section %d", name, section.SectionY)
			}
			components.Biomes[sectionY] = section
		}
		// A component this version does not understand is left alone rather
		// than guessed at; it belongs to a later schema.
	}
	return components, nil
}

func parseSectionY(name, prefix string) (int32, error) {
	suffix := strings.TrimPrefix(name, prefix)
	value, err := strconv.ParseInt(suffix, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("component %q has an invalid section coordinate", name)
	}
	if suffix != strconv.FormatInt(value, 10) {
		return 0, fmt.Errorf("component %q has a non-canonical section coordinate", name)
	}
	return int32(value), nil
}

func readObject(source ObjectSource, ref model.BlobRef) ([]byte, error) {
	file, err := source.Open(ref)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != ref.Size {
		return nil, fmt.Errorf("object %s is %d bytes; the observation records %d", ref.Digest, len(data), ref.Size)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ref.Digest {
		return nil, fmt.Errorf("object %s does not match its digest", ref.Digest)
	}
	return data, nil
}

func writeFileAtomic(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
