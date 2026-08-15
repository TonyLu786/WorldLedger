package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

final class CaptureTimingTest {
	@Test
	void aSessionThatDidNoWorkSaysSoRatherThanReportingZero() {
		CaptureTiming.Snapshot nothing = new CaptureTiming.Snapshot(0, 0, 0);
		assertFalse(nothing.measured());
		assertTrue(nothing.describe().contains("no ticks"), nothing.describe());
	}

	/**
	 * The maximum is what decides whether a frame was dropped; a mean over many
	 * quiet ticks hides exactly the tick worth knowing about.
	 */
	@Test
	void theMaximumIsReportedSeparatelyFromTheMean() {
		CaptureTiming.Snapshot uneven = new CaptureTiming.Snapshot(100, 100 * 100_000L, 9_000_000L);
		assertEquals(100.0, uneven.meanMicroseconds(), 0.001);
		assertEquals(9_000.0, uneven.maxMicroseconds(), 0.001);
		assertTrue(uneven.describe().contains("max"), uneven.describe());
	}

	/** A Minecraft tick is 50 ms, which is the budget the number is against. */
	@Test
	void theWorstTickIsExpressedAgainstATickBudget() {
		assertEquals(0.5, new CaptureTiming.Snapshot(1, 25_000_000L, 25_000_000L).worstTickShareOfBudget(), 0.0001);
		assertEquals(0.01, new CaptureTiming.Snapshot(1, 500_000L, 500_000L).worstTickShareOfBudget(), 0.0001);
	}

	@Test
	void negativeTimesAreRefused() {
		assertThrows(IllegalArgumentException.class, () -> new CaptureTiming.Snapshot(-1, 0, 0));
		assertThrows(IllegalArgumentException.class, () -> new CaptureTiming.Snapshot(0, -1, 0));
	}

	@Test
	void thePublishedSnapshotIsWhatIsReadBack() {
		CaptureTiming.reset();
		assertFalse(CaptureTiming.lastSession().measured());
		CaptureTiming.publish(new CaptureTiming.Snapshot(5, 5_000_000L, 2_000_000L));
		assertEquals(5, CaptureTiming.lastSession().ticks());
		CaptureTiming.reset();
	}
}
