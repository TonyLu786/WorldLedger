package org.worldledger.fabric.capture;

public record ChunkCoordinate(int x, int z) implements Comparable<ChunkCoordinate> {
	@Override
	public int compareTo(ChunkCoordinate other) {
		int xOrder = Integer.compare(x, other.x);
		return xOrder != 0 ? xOrder : Integer.compare(z, other.z);
	}
}
