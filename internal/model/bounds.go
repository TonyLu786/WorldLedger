package model

import "fmt"

// ChunkBounds is an inclusive box of chunk coordinates.
//
// It lives here because more than one kind of declaration needs to name an
// area: a redaction withholds one, and a landmark gives one a name. Keeping a
// second copy per package would mean a second parser and a second set of
// off-by-one decisions about whether the bounds are inclusive.
//
// The name avoids "region", which in this codebase already means an Anvil
// region file covering 32x32 chunks. This is an arbitrary box, not that.
type ChunkBounds struct {
	MinX int32 `json:"min_x"`
	MinZ int32 `json:"min_z"`
	MaxX int32 `json:"max_x"`
	MaxZ int32 `json:"max_z"`
}

// Contains reports whether a chunk falls inside. Both bounds are inclusive, so
// a box with equal minima and maxima is one chunk rather than none.
func (b ChunkBounds) Contains(x, z int32) bool {
	return x >= b.MinX && x <= b.MaxX && z >= b.MinZ && z <= b.MaxZ
}

func (b ChunkBounds) Validate() error {
	if b.MinX > b.MaxX || b.MinZ > b.MaxZ {
		return fmt.Errorf("bounds are inverted: (%d,%d) to (%d,%d)", b.MinX, b.MinZ, b.MaxX, b.MaxZ)
	}
	return nil
}

// Chunks is how many chunks the box covers.
func (b ChunkBounds) Chunks() int {
	if b.Validate() != nil {
		return 0
	}
	return int(int64(b.MaxX-b.MinX+1) * int64(b.MaxZ-b.MinZ+1))
}

func (b ChunkBounds) String() string {
	return fmt.Sprintf("chunks (%d,%d) to (%d,%d)", b.MinX, b.MinZ, b.MaxX, b.MaxZ)
}
