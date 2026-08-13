package org.worldledger.fabric.capture;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

public final class DirtyChunkTracker {
	public record Claim(ChunkCoordinate chunk, long firstDirtyTick, long lastDirtyTick) {}

	private record DirtyWindow(long firstTick, long lastTick) {
		DirtyWindow updated(long tick) {
			return new DirtyWindow(firstTick, Math.max(lastTick, tick));
		}

		DirtyWindow merge(DirtyWindow other) {
			return new DirtyWindow(Math.min(firstTick, other.firstTick), Math.max(lastTick, other.lastTick));
		}
	}

	private final long coalesceTicks;
	private final long maxLatencyTicks;
	private final Set<ChunkCoordinate> loaded = new HashSet<>();
	private final Map<ChunkCoordinate, DirtyWindow> dirty = new HashMap<>();

	public DirtyChunkTracker(long coalesceTicks, long maxLatencyTicks) {
		if (coalesceTicks < 1 || maxLatencyTicks < coalesceTicks) {
			throw new IllegalArgumentException("invalid dirty coalescing window");
		}
		this.coalesceTicks = coalesceTicks;
		this.maxLatencyTicks = maxLatencyTicks;
	}

	public synchronized void load(ChunkCoordinate chunk, long tick) {
		loaded.add(chunk);
		markLoadedDirty(chunk, tick);
	}

	public synchronized void markDirty(ChunkCoordinate chunk, long tick) {
		if (loaded.contains(chunk)) {
			markLoadedDirty(chunk, tick);
		}
	}

	public synchronized List<Claim> claimDue(long tick, int maximum) {
		if (maximum < 1) {
			throw new IllegalArgumentException("maximum must be positive");
		}
		List<Map.Entry<ChunkCoordinate, DirtyWindow>> due = dirty.entrySet().stream()
				.filter(entry -> isDue(entry.getValue(), tick))
				.sorted(Comparator.<Map.Entry<ChunkCoordinate, DirtyWindow>>comparingLong(
							entry -> entry.getValue().firstTick())
						.thenComparing(Map.Entry::getKey))
				.limit(maximum)
				.toList();
		List<Claim> result = new ArrayList<>(due.size());
		for (Map.Entry<ChunkCoordinate, DirtyWindow> entry : due) {
			dirty.remove(entry.getKey());
			result.add(toClaim(entry.getKey(), entry.getValue()));
		}
		return result;
	}

	public synchronized Claim claimFinal(ChunkCoordinate chunk) {
		DirtyWindow window = dirty.remove(chunk);
		return window == null ? null : toClaim(chunk, window);
	}

	public synchronized List<Claim> claimAll() {
		List<Map.Entry<ChunkCoordinate, DirtyWindow>> entries = dirty.entrySet().stream()
				.sorted(Map.Entry.comparingByKey())
				.toList();
		List<Claim> result = new ArrayList<>(entries.size());
		for (Map.Entry<ChunkCoordinate, DirtyWindow> entry : entries) {
			result.add(toClaim(entry.getKey(), entry.getValue()));
		}
		dirty.clear();
		return result;
	}

	public synchronized void retry(Claim claim) {
		if (!loaded.contains(claim.chunk())) {
			return;
		}
		DirtyWindow restored = new DirtyWindow(claim.firstDirtyTick(), claim.lastDirtyTick());
		dirty.merge(claim.chunk(), restored, DirtyWindow::merge);
	}

	public synchronized void forget(ChunkCoordinate chunk) {
		loaded.remove(chunk);
		dirty.remove(chunk);
	}

	public synchronized void clear() {
		loaded.clear();
		dirty.clear();
	}

	public synchronized int dirtyCount() {
		return dirty.size();
	}

	private void markLoadedDirty(ChunkCoordinate chunk, long tick) {
		dirty.compute(chunk, (ignored, current) ->
				current == null ? new DirtyWindow(tick, tick) : current.updated(tick));
	}

	private boolean isDue(DirtyWindow window, long tick) {
		return tick - window.lastTick() >= coalesceTicks || tick - window.firstTick() >= maxLatencyTicks;
	}

	private static Claim toClaim(ChunkCoordinate chunk, DirtyWindow window) {
		return new Claim(chunk, window.firstTick(), window.lastTick());
	}
}
