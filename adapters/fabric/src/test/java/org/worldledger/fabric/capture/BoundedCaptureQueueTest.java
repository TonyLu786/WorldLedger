package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;

final class BoundedCaptureQueueTest {
	@Test
	void queueIsBoundedAndPreservesAcceptedOrder() throws InterruptedException {
		CountDownLatch firstStarted = new CountDownLatch(1);
		CountDownLatch release = new CountDownLatch(1);
		List<Long> written = Collections.synchronizedList(new ArrayList<>());
		BoundedCaptureQueue queue = new BoundedCaptureQueue(
				1,
				job -> {
					firstStarted.countDown();
					if (!release.await(5, TimeUnit.SECONDS)) {
						throw new IllegalStateException("test sink timed out");
					}
					written.add(job.sequence());
				},
				(job, exception) -> {
					throw new AssertionError(exception);
				});
		assertTrue(queue.offer(CaptureTestFixtures.job(1)));
		assertTrue(firstStarted.await(5, TimeUnit.SECONDS));
		assertTrue(queue.offer(CaptureTestFixtures.job(2)));
		assertFalse(queue.offer(CaptureTestFixtures.job(3)));
		release.countDown();
		assertTrue(queue.closeGracefully(Duration.ofSeconds(5)));
		assertEquals(List.of(1L, 2L), written);
		assertEquals(2, queue.completed());
		assertEquals(0, queue.failed());
	}

	/**
	 * A final flush waits for the writer rather than discarding observed state.
	 * The non-waiting offer refuses immediately; the waiting one succeeds once
	 * the writer drains.
	 */
	@Test
	void waitingOfferAcceptsAJobOnceTheWriterDrains() throws InterruptedException {
		CountDownLatch firstStarted = new CountDownLatch(1);
		CountDownLatch release = new CountDownLatch(1);
		List<Long> written = Collections.synchronizedList(new ArrayList<>());
		BoundedCaptureQueue queue = new BoundedCaptureQueue(
				1,
				job -> {
					firstStarted.countDown();
					if (!release.await(5, TimeUnit.SECONDS)) {
						throw new IllegalStateException("test sink timed out");
					}
					written.add(job.sequence());
				},
				(job, exception) -> {
					throw new AssertionError(exception);
				});

		assertTrue(queue.offer(CaptureTestFixtures.job(1)));
		assertTrue(firstStarted.await(5, TimeUnit.SECONDS));
		assertTrue(queue.offer(CaptureTestFixtures.job(2)));
		// The queue is full: without waiting this state would simply be lost.
		assertFalse(queue.offer(CaptureTestFixtures.job(3)));

		Thread releaser = Thread.ofPlatform().start(() -> {
			try {
				Thread.sleep(50);
			} catch (InterruptedException exception) {
				Thread.currentThread().interrupt();
			}
			release.countDown();
		});
		assertTrue(queue.offer(CaptureTestFixtures.job(3), Duration.ofSeconds(5)));
		releaser.join();

		assertTrue(queue.closeGracefully(Duration.ofSeconds(5)));
		assertEquals(List.of(1L, 2L, 3L), written);
		assertEquals(0, queue.failed());
	}

	/** The wait is bounded, so a stalled writer cannot hang a disconnect. */
	@Test
	void waitingOfferGivesUpWhenTheWriterNeverDrains() throws InterruptedException {
		CountDownLatch firstStarted = new CountDownLatch(1);
		CountDownLatch release = new CountDownLatch(1);
		BoundedCaptureQueue queue = new BoundedCaptureQueue(
				1,
				job -> {
					firstStarted.countDown();
					if (!release.await(10, TimeUnit.SECONDS)) {
						throw new IllegalStateException("test sink timed out");
					}
				},
				(job, exception) -> {
					throw new AssertionError(exception);
				});

		assertTrue(queue.offer(CaptureTestFixtures.job(1)));
		assertTrue(firstStarted.await(5, TimeUnit.SECONDS));
		assertTrue(queue.offer(CaptureTestFixtures.job(2)));

		long started = System.nanoTime();
		assertFalse(queue.offer(CaptureTestFixtures.job(3), Duration.ofMillis(200)));
		long waited = Duration.ofNanos(System.nanoTime() - started).toMillis();
		assertTrue(waited >= 150, "should have waited for the budget, waited " + waited + "ms");
		assertTrue(waited < 5000, "should have given up promptly, waited " + waited + "ms");

		release.countDown();
		queue.closeGracefully(Duration.ofSeconds(5));
	}

	/** A zero budget behaves exactly like the non-waiting offer. */
	@Test
	void zeroBudgetDoesNotWait() throws InterruptedException {
		CountDownLatch firstStarted = new CountDownLatch(1);
		CountDownLatch release = new CountDownLatch(1);
		BoundedCaptureQueue queue = new BoundedCaptureQueue(
				1,
				job -> {
					firstStarted.countDown();
					if (!release.await(5, TimeUnit.SECONDS)) {
						throw new IllegalStateException("test sink timed out");
					}
				},
				(job, exception) -> {
					throw new AssertionError(exception);
				});

		assertTrue(queue.offer(CaptureTestFixtures.job(1)));
		assertTrue(firstStarted.await(5, TimeUnit.SECONDS));
		assertTrue(queue.offer(CaptureTestFixtures.job(2), Duration.ZERO));
		assertFalse(queue.offer(CaptureTestFixtures.job(3), Duration.ZERO));

		release.countDown();
		queue.closeGracefully(Duration.ofSeconds(5));
	}
}
