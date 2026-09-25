package main

import (
	"fmt"
	"math"
)

// A chunk coordinate and a data version are both int32 on disk, and the flags
// that supply them were parsed as int and converted without a word.
//
// On a 64-bit machine that conversion wraps silently. `--x 4294967296` stored
// an observation at chunk 0,0 and derived its identity from the truncated
// coordinate, so the record said one thing and its name said another; the same
// flags on inspect and verify then answered about a different chunk than the
// one that was asked for. `--data-version 4294967296` printed "written at data
// version 0" and stamped that into region files a Minecraft client reads, and
// `--data-version -1` was simply accepted.
//
// None of these is a likely typo. All of them are a number that was refused by
// nothing, which is the part worth fixing: a value out of range is something a
// person can correct, and a value that wrapped is something they cannot even
// see.

func requireInt32(flag string, value int) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("--%s is %d, which does not fit in the 32 bits this is stored in (%d to %d)",
			flag, value, math.MinInt32, math.MaxInt32)
	}
	return int32(value), nil
}

// requireDataVersion additionally refuses a negative one.
//
// A data version is a count that has only ever gone up. A negative one cannot
// have come from any Minecraft release, and stamping it into a world produces a
// file whose own version field says something impossible.
func requireDataVersion(value int) (int32, error) {
	stamped, err := requireInt32("data-version", value)
	if err != nil {
		return 0, err
	}
	if stamped < 0 {
		return 0, fmt.Errorf("--data-version is %d; a data version is a count and no release has a negative one", value)
	}
	return stamped, nil
}

// requireChunk checks a pair together, so somebody who got both wrong is told
// about both.
func requireChunk(x, z int) (int32, int32, error) {
	chunkX, xErr := requireInt32("x", x)
	chunkZ, zErr := requireInt32("z", z)
	switch {
	case xErr != nil && zErr != nil:
		return 0, 0, fmt.Errorf("%v; and %v", xErr, zErr)
	case xErr != nil:
		return 0, 0, xErr
	case zErr != nil:
		return 0, 0, zErr
	}
	return chunkX, chunkZ, nil
}
