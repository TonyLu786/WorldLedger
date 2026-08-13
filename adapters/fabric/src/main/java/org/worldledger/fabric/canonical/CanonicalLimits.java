package org.worldledger.fabric.canonical;

public record CanonicalLimits(
		int maxComponentBytes,
		int maxStringBytes,
		int maxNbtBytes,
		int maxNbtDepth,
		int maxCollectionItems) {
	public CanonicalLimits {
		if (maxComponentBytes <= 0
				|| maxStringBytes <= 0
				|| maxNbtBytes <= 0
				|| maxNbtDepth <= 0
				|| maxCollectionItems <= 0) {
			throw new IllegalArgumentException("canonicalization limits must be positive");
		}
		if (maxNbtBytes > maxComponentBytes) {
			throw new IllegalArgumentException("NBT byte limit exceeds component byte limit");
		}
	}

	public static CanonicalLimits defaults() {
		return new CanonicalLimits(64 << 20, 1 << 20, 1 << 20, 64, 1 << 20);
	}
}
