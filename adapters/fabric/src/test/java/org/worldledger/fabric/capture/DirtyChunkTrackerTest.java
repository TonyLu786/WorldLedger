package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.List;
import org.junit.jupiter.api.Test;

final class DirtyChunkTrackerTest {
	@Test
	void burstUpdatesCoalesceUntilQuiet() {
		DirtyChunkTracker tracker = new DirtyChunkTracker(5, 50);
		ChunkCoordinate chunk = new ChunkCoordinate(2, -3);
		tracker.load(chunk, 0);
		tracker.markDirty(chunk, 3);
		tracker.markDirty(chunk, 6);
		assertTrue(tracker.claimDue(10, 1).isEmpty());
		List<DirtyChunkTracker.Claim> due = tracker.claimDue(11, 1);
		assertEquals(1, due.size());
		assertEquals(chunk, due.getFirst().chunk());
		assertEquals(0, due.getFirst().firstDirtyTick());
		assertEquals(6, due.getFirst().lastDirtyTick());
	}

	@Test
	void sustainedUpdatesCannotStarveSnapshot() {
		DirtyChunkTracker tracker = new DirtyChunkTracker(5, 20);
		ChunkCoordinate chunk = new ChunkCoordinate(0, 0);
		tracker.load(chunk, 0);
		for (int tick = 1; tick <= 20; tick++) {
			tracker.markDirty(chunk, tick);
		}
		assertEquals(1, tracker.claimDue(20, 1).size());
	}

	@Test
	void failedQueueOfferCanRestoreClaim() {
		DirtyChunkTracker tracker = new DirtyChunkTracker(2, 20);
		ChunkCoordinate chunk = new ChunkCoordinate(1, 1);
		tracker.load(chunk, 0);
		DirtyChunkTracker.Claim claim = tracker.claimDue(2, 1).getFirst();
		tracker.retry(claim);
		assertEquals(1, tracker.claimDue(2, 1).size());
	}

	@Test
	void finalClaimsDrainEveryDirtyChunkInCoordinateOrder() {
		DirtyChunkTracker tracker = new DirtyChunkTracker(2, 20);
		ChunkCoordinate later = new ChunkCoordinate(4, 0);
		ChunkCoordinate earlier = new ChunkCoordinate(-2, 9);
		tracker.load(later, 3);
		tracker.load(earlier, 4);

		DirtyChunkTracker.Claim first = tracker.claimFinal(earlier);
		assertEquals(earlier, first.chunk());
		assertEquals(List.of(later), tracker.claimAll().stream().map(DirtyChunkTracker.Claim::chunk).toList());
		assertEquals(0, tracker.dirtyCount());
	}
}
