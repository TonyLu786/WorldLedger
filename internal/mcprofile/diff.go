package mcprofile

import (
	"fmt"
	"sort"
)

// Comparing two release profiles is what makes a Minecraft upgrade something
// other than a surprise.
//
// The capture fingerprint is committed, so a release that changes what the game
// reports will change it and fail the build. That is the point of the gate, but
// on its own it only says something moved. It cannot say whether the archive
// core regressed or whether the game legitimately changed underneath it, and
// answering that by hand means reading two registries side by side.
//
// A delta answers it from the releases themselves. It also separates the two
// kinds of change that get conflated: what a new release can newly represent is
// harmless to an archive already captured, while what it stops representing,
// where it moves the build range, and where it changes structure placement all
// bear on data that already exists.

// Delta is what changed between two release profiles, in the direction From to
// To. It is a description rather than a judgement: nothing here decides whether
// a change is acceptable, only what it is.
type Delta struct {
	From, To                       string
	FromDataVersion, ToDataVersion int32

	BlocksAdded, BlocksRemoved []string
	BiomesAdded, BiomesRemoved []string

	DimensionsAdded, DimensionsRemoved []string
	DimensionsChanged                  []DimensionChange

	StructureSetsAdded, StructureSetsRemoved []string
	StructureSetsChanged                     []StructureSetChange
}

type DimensionChange struct {
	ID       string
	From, To Dimension
}

// Narrowed reports whether the build range lost sections at either end. That is
// the change that can put existing observations outside the world, and 1.18 is
// the precedent: it moved the overworld floor from 0 to -64.
func (c DimensionChange) Narrowed() bool {
	return c.To.MinSectionY > c.From.MinSectionY || c.To.MaxSectionY() < c.From.MaxSectionY()
}

type StructureSetChange struct {
	ID       string
	From, To StructureSet
}

// Forward reports whether To is the newer release. A delta is computed in
// whichever direction it is given, and both directions are useful: forward is
// an upgrade, backwards is the question convert has to answer.
func (d Delta) Forward() bool { return d.ToDataVersion > d.FromDataVersion }

// Empty reports whether the two profiles describe the same capabilities. Data
// versions are excluded deliberately: two releases can differ by a version
// number and represent exactly the same things, and for everything this project
// does with a profile, that is no difference at all.
func (d Delta) Empty() bool {
	return len(d.BlocksAdded) == 0 && len(d.BlocksRemoved) == 0 &&
		len(d.BiomesAdded) == 0 && len(d.BiomesRemoved) == 0 &&
		len(d.DimensionsAdded) == 0 && len(d.DimensionsRemoved) == 0 &&
		len(d.DimensionsChanged) == 0 &&
		len(d.StructureSetsAdded) == 0 && len(d.StructureSetsRemoved) == 0 &&
		len(d.StructureSetsChanged) == 0
}

// TouchesExistingArchives reports whether anything changed that bears on
// observations already captured, as opposed to only widening what a later
// capture could hold.
//
// A removed block or biome is an identifier an existing observation may name
// and the other release cannot place. A narrowed build range can put an
// existing section outside the world. A changed structure placement invalidates
// any seed research done against the other release. Additions do none of that.
func (d Delta) TouchesExistingArchives() bool {
	if len(d.BlocksRemoved) > 0 || len(d.BiomesRemoved) > 0 || len(d.DimensionsRemoved) > 0 {
		return true
	}
	for _, change := range d.DimensionsChanged {
		if change.Narrowed() {
			return true
		}
	}
	// Any structure placement change counts, including a removal. Placement is
	// the whole input to the seed search, so a different salt is a different
	// answer, not a smaller one.
	return len(d.StructureSetsChanged) > 0 || len(d.StructureSetsRemoved) > 0
}

// Compare reports what changed going from one release to the other.
func Compare(from, to Profile) Delta {
	delta := Delta{
		From:            from.Version,
		To:              to.Version,
		FromDataVersion: from.DataVersion,
		ToDataVersion:   to.DataVersion,
	}

	delta.BlocksAdded, delta.BlocksRemoved = compareNames(from.Blocks, to.Blocks)
	delta.BiomesAdded, delta.BiomesRemoved = compareNames(from.Biomes, to.Biomes)

	for id, before := range from.Dimensions {
		after, exists := to.Dimensions[id]
		switch {
		case !exists:
			delta.DimensionsRemoved = append(delta.DimensionsRemoved, id)
		case after != before:
			delta.DimensionsChanged = append(delta.DimensionsChanged, DimensionChange{ID: id, From: before, To: after})
		}
	}
	for id := range to.Dimensions {
		if _, exists := from.Dimensions[id]; !exists {
			delta.DimensionsAdded = append(delta.DimensionsAdded, id)
		}
	}

	for id, before := range from.StructureSets {
		after, exists := to.StructureSets[id]
		switch {
		case !exists:
			delta.StructureSetsRemoved = append(delta.StructureSetsRemoved, id)
		case after != before:
			delta.StructureSetsChanged = append(delta.StructureSetsChanged, StructureSetChange{ID: id, From: before, To: after})
		}
	}
	for id := range to.StructureSets {
		if _, exists := from.StructureSets[id]; !exists {
			delta.StructureSetsAdded = append(delta.StructureSetsAdded, id)
		}
	}

	sort.Strings(delta.DimensionsAdded)
	sort.Strings(delta.DimensionsRemoved)
	sort.Strings(delta.StructureSetsAdded)
	sort.Strings(delta.StructureSetsRemoved)
	sort.Slice(delta.DimensionsChanged, func(i, j int) bool {
		return delta.DimensionsChanged[i].ID < delta.DimensionsChanged[j].ID
	})
	sort.Slice(delta.StructureSetsChanged, func(i, j int) bool {
		return delta.StructureSetsChanged[i].ID < delta.StructureSetsChanged[j].ID
	})
	return delta
}

// compareNames returns what To has that From does not, and the reverse. Both
// registries are sorted in a profile, but nothing here relies on that.
func compareNames(from, to []string) (added, removed []string) {
	before := make(map[string]struct{}, len(from))
	for _, name := range from {
		before[name] = struct{}{}
	}
	after := make(map[string]struct{}, len(to))
	for _, name := range to {
		after[name] = struct{}{}
	}
	for name := range after {
		if _, exists := before[name]; !exists {
			added = append(added, name)
		}
	}
	for name := range before {
		if _, exists := after[name]; !exists {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// Describe renders a dimension's build range the way the profile means it, in
// blocks rather than in sections, because that is the number an operator reads
// off a world.
func (d Dimension) Describe() string {
	return fmt.Sprintf("y %d to %d", d.MinSectionY*16, (d.MaxSectionY()+1)*16-1)
}
