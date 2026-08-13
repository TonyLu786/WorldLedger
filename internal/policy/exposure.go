package policy

import (
	"fmt"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// Exposure is a coarse band describing how much a server's accumulated
// observations would help someone reconstruct its generation parameters.
//
// These bands are prompts for a human decision, not measurements of how likely
// an attack is to succeed. Nothing here simulates a recovery attempt. Treating
// a low band as safety would be exactly the fabricated certainty this project
// exists to avoid.
type Exposure string

const (
	ExposureMinimal     Exposure = "minimal"
	ExposureModerate    Exposure = "moderate"
	ExposureSubstantial Exposure = "substantial"
)

// Assessment describes accumulated coverage for one server.
type Assessment struct {
	Server string `json:"server"`
	Chunks int    `json:"chunks"`
	// Regions is how many 32x32 chunk region files the coverage touches.
	Regions int `json:"regions"`
	// LargestCluster is the biggest edge-connected run of observed chunks.
	// Contiguity matters more than raw count: scattered chunks rarely contain a
	// whole structure, and a structure someone can locate is what constrains a
	// seed.
	LargestCluster int      `json:"largest_cluster"`
	DensestRegion  float64  `json:"densest_region_fill"`
	MinX           int32    `json:"min_x"`
	MinZ           int32    `json:"min_z"`
	MaxX           int32    `json:"max_x"`
	MaxZ           int32    `json:"max_z"`
	Exposure       Exposure `json:"exposure"`
	Reason         string   `json:"reason"`
}

// A vanilla structure set commonly spaces candidates about 32 chunks apart, so
// a contiguous area of roughly one region is the scale at which whole structures
// start being captured.
const (
	clusterMinimalCeiling  = 64
	clusterModerateCeiling = 32 * 32
	regionsOfConcern       = 4
)

// Assess measures accumulated coverage. It takes the chunks an archive actually
// holds for a server, across dimensions, because the exposure comes from the
// merged archive rather than from any one contribution.
func Assess(server string, chunks []model.ChunkRef) Assessment {
	assessment := Assessment{Server: model.NormalizeToken(server), Chunks: len(chunks)}
	if len(chunks) == 0 {
		assessment.Exposure = ExposureMinimal
		assessment.Reason = "no observations"
		return assessment
	}

	present := make(map[chunkKey]bool, len(chunks))
	regions := map[chunkKey]int{}
	assessment.MinX, assessment.MaxX = chunks[0].X, chunks[0].X
	assessment.MinZ, assessment.MaxZ = chunks[0].Z, chunks[0].Z

	for _, chunk := range chunks {
		present[chunkKey{chunk.X, chunk.Z}] = true
		regions[chunkKey{chunk.X >> 5, chunk.Z >> 5}]++
		if chunk.X < assessment.MinX {
			assessment.MinX = chunk.X
		}
		if chunk.X > assessment.MaxX {
			assessment.MaxX = chunk.X
		}
		if chunk.Z < assessment.MinZ {
			assessment.MinZ = chunk.Z
		}
		if chunk.Z > assessment.MaxZ {
			assessment.MaxZ = chunk.Z
		}
	}

	assessment.Regions = len(regions)
	for _, count := range regions {
		if fill := float64(count) / float64(32*32); fill > assessment.DensestRegion {
			assessment.DensestRegion = fill
		}
	}
	assessment.LargestCluster = largestCluster(present)

	switch {
	case assessment.LargestCluster >= clusterModerateCeiling:
		assessment.Exposure = ExposureSubstantial
		assessment.Reason = fmt.Sprintf("a contiguous area of %d chunks is large enough to contain whole structures", assessment.LargestCluster)
	case assessment.Regions >= regionsOfConcern && assessment.DensestRegion >= 0.25:
		assessment.Exposure = ExposureSubstantial
		assessment.Reason = fmt.Sprintf("%d regions with up to %.0f%% coverage span several structure placements", assessment.Regions, assessment.DensestRegion*100)
	case assessment.LargestCluster >= clusterMinimalCeiling:
		assessment.Exposure = ExposureModerate
		assessment.Reason = fmt.Sprintf("a contiguous area of %d chunks may contain a structure", assessment.LargestCluster)
	default:
		assessment.Exposure = ExposureMinimal
		assessment.Reason = fmt.Sprintf("coverage is scattered; the largest contiguous area is %d chunks", assessment.LargestCluster)
	}
	return assessment
}

type chunkKey struct{ x, z int32 }

// largestCluster finds the biggest edge-connected component, iteratively so that
// a large archive cannot exhaust the stack.
func largestCluster(present map[chunkKey]bool) int {
	seen := make(map[chunkKey]bool, len(present))
	largest := 0

	for start := range present {
		if seen[start] {
			continue
		}
		size := 0
		stack := []chunkKey{start}
		seen[start] = true
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			neighbours := []chunkKey{
				{current.x + 1, current.z},
				{current.x - 1, current.z},
				{current.x, current.z + 1},
				{current.x, current.z - 1},
			}
			for _, neighbour := range neighbours {
				if present[neighbour] && !seen[neighbour] {
					seen[neighbour] = true
					stack = append(stack, neighbour)
				}
			}
		}
		if size > largest {
			largest = size
		}
	}
	return largest
}
