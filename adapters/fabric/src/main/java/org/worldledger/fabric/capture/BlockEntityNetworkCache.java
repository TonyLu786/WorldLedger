package org.worldledger.fabric.capture;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;

import org.worldledger.fabric.canonical.BlockEntityValue;
import org.worldledger.fabric.canonical.NbtValue;

public final class BlockEntityNetworkCache {
	public static final int DEFAULT_MAX_ENTRIES_PER_CHUNK = 4096;
	public static final long DEFAULT_MAX_BYTES_PER_CHUNK = 16L << 20;
	public static final long DEFAULT_MAX_TOTAL_BYTES = 128L << 20;

	public record Position(int x, int y, int z) {}

	public record NetworkEntry(Position position, String type, NbtValue.CompoundTag nbt, int sourceBytes) {
		public NetworkEntry {
			Objects.requireNonNull(position, "position");
			Objects.requireNonNull(type, "type");
			Objects.requireNonNull(nbt, "nbt");
			if (sourceBytes < 0) {
				throw new IllegalArgumentException("sourceBytes must not be negative");
			}
		}
	}

	private static final class ChunkState {
		private boolean baselineKnown;
		private long sourceBytes;
		private final Map<Position, NetworkEntry> entries = new HashMap<>();
	}

	private final int maxEntriesPerChunk;
	private final long maxBytesPerChunk;
	private final long maxTotalBytes;
	private final Map<ChunkCoordinate, ChunkState> chunks = new HashMap<>();
	private long totalSourceBytes;

	public BlockEntityNetworkCache() {
		this(DEFAULT_MAX_ENTRIES_PER_CHUNK, DEFAULT_MAX_BYTES_PER_CHUNK, DEFAULT_MAX_TOTAL_BYTES);
	}

	BlockEntityNetworkCache(int maxEntriesPerChunk, long maxBytesPerChunk, long maxTotalBytes) {
		if (maxEntriesPerChunk < 1
				|| maxBytesPerChunk < 1
				|| maxTotalBytes < maxBytesPerChunk) {
			throw new IllegalArgumentException("invalid block-entity cache limits");
		}
		this.maxEntriesPerChunk = maxEntriesPerChunk;
		this.maxBytesPerChunk = maxBytesPerChunk;
		this.maxTotalBytes = maxTotalBytes;
	}

	public synchronized void replaceBaseline(ChunkCoordinate chunk, List<NetworkEntry> entries) {
		Objects.requireNonNull(chunk, "chunk");
		Objects.requireNonNull(entries, "entries");
		ChunkState replacement = new ChunkState();
		for (NetworkEntry entry : entries) {
			if (!chunk.equals(chunkFor(entry.position()))) {
				throw new IllegalArgumentException("block entity lies outside its chunk baseline");
			}
			if (replacement.entries.put(entry.position(), entry) != null) {
				throw new IllegalArgumentException("duplicate block entity position in baseline");
			}
			replacement.sourceBytes = checkedAdd(replacement.sourceBytes, entry.sourceBytes());
			validateChunkBounds(replacement);
		}
		ChunkState previous = chunks.get(chunk);
		long previousBytes = previous == null ? 0 : previous.sourceBytes;
		long proposedTotal = checkedAdd(totalSourceBytes - previousBytes, replacement.sourceBytes);
		if (proposedTotal > maxTotalBytes) {
			throw new IllegalArgumentException("block-entity cache exceeds its global byte limit");
		}
		replacement.baselineKnown = true;
		chunks.put(chunk, replacement);
		totalSourceBytes = proposedTotal;
	}

	public synchronized void markUnknown(ChunkCoordinate chunk) {
		removeChunk(chunk);
	}

	public synchronized void update(NetworkEntry entry) {
		ChunkCoordinate chunk = chunkFor(entry.position());
		ChunkState state = chunks.get(chunk);
		if (state == null || !state.baselineKnown) {
			return;
		}
		NetworkEntry previous = state.entries.get(entry.position());
		long previousBytes = previous == null ? 0 : previous.sourceBytes();
		long proposedChunkBytes = checkedAdd(state.sourceBytes - previousBytes, entry.sourceBytes());
		int proposedEntries = state.entries.size() + (previous == null ? 1 : 0);
		if (proposedEntries > maxEntriesPerChunk || proposedChunkBytes > maxBytesPerChunk) {
			throw new IllegalArgumentException("block-entity cache exceeds its per-chunk limit");
		}
		long proposedTotal = checkedAdd(totalSourceBytes - previousBytes, entry.sourceBytes());
		if (proposedTotal > maxTotalBytes) {
			throw new IllegalArgumentException("block-entity cache exceeds its global byte limit");
		}
		state.entries.put(entry.position(), entry);
		state.sourceBytes = proposedChunkBytes;
		totalSourceBytes = proposedTotal;
	}

	public synchronized void invalidateUnlessType(Position position, String currentType) {
		ChunkState state = chunks.get(chunkFor(position));
		if (state == null || !state.baselineKnown) {
			return;
		}
		NetworkEntry cached = state.entries.get(position);
		if (cached != null && !cached.type().equals(currentType)) {
			removeEntry(state, position);
		}
	}

	public synchronized void remove(Position position) {
		ChunkState state = chunks.get(chunkFor(position));
		if (state != null && state.baselineKnown) {
			removeEntry(state, position);
		}
	}

	public synchronized Optional<List<BlockEntityValue>> snapshot(ChunkCoordinate chunk) {
		ChunkState state = chunks.get(chunk);
		if (state == null || !state.baselineKnown) {
			return Optional.empty();
		}
		List<BlockEntityValue> result = new ArrayList<>(state.entries.size());
		for (NetworkEntry entry : state.entries.values()) {
			result.add(new BlockEntityValue(
					Math.floorMod(entry.position().x(), 16),
					entry.position().y(),
					Math.floorMod(entry.position().z(), 16),
					entry.type(),
					entry.nbt()));
		}
		return Optional.of(List.copyOf(result));
	}

	public synchronized void forget(ChunkCoordinate chunk) {
		removeChunk(chunk);
	}

	public synchronized void clear() {
		chunks.clear();
		totalSourceBytes = 0;
	}

	public static ChunkCoordinate chunkFor(Position position) {
		return new ChunkCoordinate(Math.floorDiv(position.x(), 16), Math.floorDiv(position.z(), 16));
	}

	private void validateChunkBounds(ChunkState state) {
		if (state.entries.size() > maxEntriesPerChunk || state.sourceBytes > maxBytesPerChunk) {
			throw new IllegalArgumentException("block-entity cache exceeds its per-chunk limit");
		}
	}

	private void removeEntry(ChunkState state, Position position) {
		NetworkEntry removed = state.entries.remove(position);
		if (removed != null) {
			state.sourceBytes -= removed.sourceBytes();
			totalSourceBytes -= removed.sourceBytes();
		}
	}

	private void removeChunk(ChunkCoordinate chunk) {
		ChunkState removed = chunks.remove(chunk);
		if (removed != null) {
			totalSourceBytes -= removed.sourceBytes;
		}
	}

	private static long checkedAdd(long left, long right) {
		try {
			return Math.addExact(left, right);
		} catch (ArithmeticException exception) {
			throw new IllegalArgumentException("block-entity cache byte count overflow", exception);
		}
	}
}
