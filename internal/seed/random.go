// Package seed implements the deterministic parts of Minecraft world
// generation that a world seed can be reasoned about from.
//
// Scope: this package models structure placement, which runs on the legacy
// 48-bit linear congruential generator and is exactly invertible in principle.
// It does not model biome or terrain generation, which in current releases run
// on a different generator and would require a bit-exact reimplementation of the
// noise pipeline. Recovering a full 64-bit world seed needs that second stage,
// so this package alone does not recover seeds; see docs/seed-recovery.md.
//
// Every constant here was read out of the pinned 26.2 client jar rather than
// recalled, and the tests pin them.
package seed

// Constants of the legacy generator, from
// net.minecraft.world.level.levelgen.LegacyRandomSource. They are the values
// java.util.Random has always used.
const (
	multiplier = 0x5DEECE66D // 25214903917
	increment  = 0xB         // 11
	mask       = (1 << 48) - 1
)

// LegacyRandomSource is Minecraft's LegacyRandomSource, which is bit-compatible
// with java.util.Random. Structure placement depends on the exact sequence, so
// this reproduces Java's algorithms rather than an equivalent-looking one.
type LegacyRandomSource struct {
	state uint64
}

func NewLegacyRandomSource(value int64) *LegacyRandomSource {
	random := &LegacyRandomSource{}
	random.SetSeed(value)
	return random
}

func (r *LegacyRandomSource) SetSeed(value int64) {
	r.state = (uint64(value) ^ multiplier) & mask
}

// State exposes the scrambled internal state. Seed search works on this value,
// not on the seed the user typed.
func (r *LegacyRandomSource) State() uint64 {
	return r.state
}

func (r *LegacyRandomSource) next(bits uint) int32 {
	r.state = (r.state*multiplier + increment) & mask
	return int32(r.state >> (48 - bits))
}

// NextInt reproduces java.util.Random.nextInt(int), including the power-of-two
// shortcut and the rejection loop that keeps the distribution uniform. The
// rejection loop consumes a variable number of values, which is why a caller
// cannot assume a fixed number of RNG draws.
func (r *LegacyRandomSource) NextInt(bound int32) int32 {
	if bound <= 0 {
		panic("bound must be positive")
	}
	value := r.next(31)
	limit := bound - 1
	if bound&limit == 0 {
		return int32((int64(bound) * int64(value)) >> 31)
	}
	for {
		next := value % bound
		// The Java loop condition detects the overflow window that would bias
		// the result; it is reproduced here in the same form.
		if value-next+limit >= 0 {
			return next
		}
		value = r.next(31)
	}
}

// Salts used when deriving a positional seed, from
// net.minecraft.world.level.levelgen.WorldgenRandom.setLargeFeatureWithSalt.
const (
	regionXSalt = 341873128712
	regionZSalt = 132897987541
)

// SetLargeFeatureWithSalt seeds the generator the way structure placement does.
//
//	setSeed(regionX*341873128712 + regionZ*132897987541 + worldSeed + salt)
func (r *LegacyRandomSource) SetLargeFeatureWithSalt(worldSeed int64, regionX, regionZ, salt int32) {
	r.SetSeed(int64(regionX)*regionXSalt + int64(regionZ)*regionZSalt + worldSeed + int64(salt))
}
