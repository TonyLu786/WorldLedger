package seed

import (
	"errors"
	"fmt"
)

// SpreadType is net.minecraft.world.level.levelgen.structure.placement.RandomSpreadType.
type SpreadType string

const (
	// SpreadLinear draws one value.
	SpreadLinear SpreadType = "linear"
	// SpreadTriangular averages two draws, which concentrates placements toward
	// the middle of a region and consumes twice as much of the RNG stream.
	SpreadTriangular SpreadType = "triangular"
)

func ParseSpreadType(value string) (SpreadType, error) {
	switch SpreadType(value) {
	case SpreadLinear:
		return SpreadLinear, nil
	case SpreadTriangular:
		return SpreadTriangular, nil
	}
	return "", fmt.Errorf("unknown spread type %q; want linear or triangular", value)
}

func (s SpreadType) evaluate(random *LegacyRandomSource, bound int32) int32 {
	if s == SpreadTriangular {
		return (random.NextInt(bound) + random.NextInt(bound)) / 2
	}
	return random.NextInt(bound)
}

// Placement is the vanilla random-spread structure placement: the world is cut
// into square regions of Spacing chunks, and one candidate chunk is drawn inside
// each region.
//
// The values are per-structure-set data, not constants of the game. They live in
// the release's own worldgen data and must be supplied by the caller, because a
// server with a datapack can change them and any assumption baked in here would
// silently produce wrong answers on exactly the worlds people care about.
type Placement struct {
	Spacing    int32      `json:"spacing"`
	Separation int32      `json:"separation"`
	Salt       int32      `json:"salt"`
	SpreadType SpreadType `json:"spread_type"`
}

func (p Placement) Validate() error {
	if p.Spacing <= 0 {
		return errors.New("spacing must be positive")
	}
	if p.Separation < 0 {
		return errors.New("separation must not be negative")
	}
	if p.Separation >= p.Spacing {
		return fmt.Errorf("separation %d must be smaller than spacing %d", p.Separation, p.Spacing)
	}
	if p.SpreadType != SpreadLinear && p.SpreadType != SpreadTriangular {
		return fmt.Errorf("unknown spread type %q", p.SpreadType)
	}
	return nil
}

// ChunkPos is a chunk coordinate pair.
type ChunkPos struct {
	X int32 `json:"x"`
	Z int32 `json:"z"`
}

// RegionOf returns the placement region containing a chunk.
func (p Placement) RegionOf(chunkX, chunkZ int32) (int32, int32) {
	return floorDiv(chunkX, p.Spacing), floorDiv(chunkZ, p.Spacing)
}

// PotentialChunk returns the chunk this seed places the structure in, for the
// region containing the given chunk. It reproduces
// RandomSpreadStructurePlacement.getPotentialStructureChunk.
//
// A "potential" chunk is where the structure may start. Whether one actually
// generates there additionally depends on biome and terrain checks that this
// package does not model, so a mismatch disproves a seed but a match does not
// prove one.
func (p Placement) PotentialChunk(worldSeed int64, chunkX, chunkZ int32) (ChunkPos, error) {
	if err := p.Validate(); err != nil {
		return ChunkPos{}, err
	}
	regionX, regionZ := p.RegionOf(chunkX, chunkZ)
	return p.potentialChunkInRegion(worldSeed, regionX, regionZ), nil
}

func (p Placement) potentialChunkInRegion(worldSeed int64, regionX, regionZ int32) ChunkPos {
	random := &LegacyRandomSource{}
	random.SetLargeFeatureWithSalt(worldSeed, regionX, regionZ, p.Salt)
	bound := p.Spacing - p.Separation
	offsetX := p.SpreadType.evaluate(random, bound)
	offsetZ := p.SpreadType.evaluate(random, bound)
	return ChunkPos{
		X: regionX*p.Spacing + offsetX,
		Z: regionZ*p.Spacing + offsetZ,
	}
}

// Observation is a structure start someone actually saw, used as a constraint.
type Observation struct {
	Placement Placement `json:"placement"`
	Chunk     ChunkPos  `json:"chunk"`
}

// Matches reports whether a candidate seed would place this structure where it
// was observed.
func (o Observation) Matches(worldSeed int64) bool {
	if o.Placement.Validate() != nil {
		return false
	}
	regionX, regionZ := o.Placement.RegionOf(o.Chunk.X, o.Chunk.Z)
	return o.Placement.potentialChunkInRegion(worldSeed, regionX, regionZ) == o.Chunk
}

// Consistent reports whether every observation agrees with a candidate seed.
func Consistent(worldSeed int64, observations []Observation) bool {
	for _, observation := range observations {
		if !observation.Matches(worldSeed) {
			return false
		}
	}
	return true
}

func floorDiv(value, divisor int32) int32 {
	quotient := value / divisor
	if value%divisor != 0 && (value < 0) != (divisor < 0) {
		quotient--
	}
	return quotient
}
