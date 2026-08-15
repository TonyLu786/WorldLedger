package org.worldledger.fabric;

import java.util.concurrent.atomic.AtomicReference;

/**
 * What capture cost the client thread, for the session that just ended.
 *
 * <p>The question a player asks before installing a capture mod is whether it
 * will make the game stutter, and the honest answer has been that nobody
 * measured. The adapter does its encoding, hashing and writing on a background
 * thread, so the part that can cost a frame is narrow: the per-tick work that
 * copies a chunk's state before handing it over. That is what this measures.
 *
 * <p>A Minecraft tick is 50 ms. A mean and a maximum in microseconds against
 * that number is the whole claim, and reporting the maximum matters more than
 * the mean: an average hides the one tick that dropped a frame.
 *
 * <p>The last session is held statically because the two things that want it
 * live on opposite sides of a boundary neither should cross for this. The
 * coordinator is client-only and the game test is a separate source set; a
 * measurement is not worth an interface between them.
 */
public final class CaptureTiming {
	/** A finished session's client-thread cost. All times are nanoseconds. */
	public record Snapshot(long ticks, long totalNanos, long maxNanos) {
		public Snapshot {
			if (ticks < 0 || totalNanos < 0 || maxNanos < 0) {
				throw new IllegalArgumentException("capture timing cannot be negative");
			}
		}

		public boolean measured() {
			return ticks > 0;
		}

		public double meanMicroseconds() {
			return ticks == 0 ? 0.0 : (double) totalNanos / ticks / 1000.0;
		}

		public double maxMicroseconds() {
			return maxNanos / 1000.0;
		}

		/**
		 * The share of one tick the worst tick used. A Minecraft tick is 50 ms,
		 * and a frame is shorter still, so this is the number that decides
		 * whether capture is noticeable.
		 */
		public double worstTickShareOfBudget() {
			return maxNanos / 50_000_000.0;
		}

		public String describe() {
			if (!measured()) {
				return "no ticks measured";
			}
			return String.format(
					java.util.Locale.ROOT,
					"%d ticks, mean %.1f us, max %.1f us (%.3f%% of a 50 ms tick)",
					ticks,
					meanMicroseconds(),
					maxMicroseconds(),
					worstTickShareOfBudget() * 100.0);
		}
	}

	private static final Snapshot NOTHING = new Snapshot(0, 0, 0);
	private static final AtomicReference<Snapshot> LAST = new AtomicReference<>(NOTHING);

	private CaptureTiming() {}

	static void publish(Snapshot snapshot) {
		LAST.set(snapshot);
	}

	/** The cost of the most recently finished capture session. */
	public static Snapshot lastSession() {
		return LAST.get();
	}

	static void reset() {
		LAST.set(NOTHING);
	}
}
