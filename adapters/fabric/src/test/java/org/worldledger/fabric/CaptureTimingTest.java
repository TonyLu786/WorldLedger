package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

final class CaptureTimingTest {

	// One slow tick as a session warms up is paid once. The same figure every few
	// seconds is a stutter. A maximum is identical in both cases, so the position
	// of the worst tick and the count of slow ones have to survive to the report.

	@Test
	void aSingleSlowTickAtTheStartIsDistinguishableFromARecurringOne() {
		CaptureTiming.Snapshot warmup = new CaptureTiming.Snapshot(100, 100 * 200_000L, 8_000_000L, 0, 1);
		CaptureTiming.Snapshot stutter = new CaptureTiming.Snapshot(100, 100 * 200_000L, 8_000_000L, 61, 14);

		assertEquals(warmup.maxNanos(), stutter.maxNanos());
		assertEquals(warmup.meanMicroseconds(), stutter.meanMicroseconds(), 0.0001);

		assertTrue(warmup.describe().contains("worst was tick 1 of 100"), warmup.describe());
		assertTrue(warmup.describe().contains("1 tick(s) over 5 ms"), warmup.describe());
		assertTrue(stutter.describe().contains("worst was tick 62 of 100"), stutter.describe());
		assertTrue(stutter.describe().contains("14 tick(s) over 5 ms"), stutter.describe());
	}

	@Test
	void aSessionMeasuredBeforeThesePositionsExistedDoesNotClaimOne() {
		CaptureTiming.Snapshot old = new CaptureTiming.Snapshot(10, 10 * 100_000L, 500_000L);
		assertEquals(-1, old.worstTickIndex());
		assertFalse(old.describe().contains("worst was tick"), old.describe());
	}

	@Test
	void countsThatContradictTheSessionAreRefused() {
		// More slow ticks than ticks, or a worst tick after the session ended,
		// mean the counters were not kept together and the report would mislead.
		assertThrows(IllegalArgumentException.class,
				() -> new CaptureTiming.Snapshot(5, 1_000_000L, 500_000L, 0, 6));
		assertThrows(IllegalArgumentException.class,
				() -> new CaptureTiming.Snapshot(5, 1_000_000L, 500_000L, 5, 1));
		assertThrows(IllegalArgumentException.class,
				() -> new CaptureTiming.Snapshot(5, 1_000_000L, 500_000L, 0, -1));
	}

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
