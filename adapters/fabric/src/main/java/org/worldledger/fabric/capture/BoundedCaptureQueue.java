package org.worldledger.fabric.capture;

import java.time.Duration;
import java.util.Objects;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BiConsumer;

public final class BoundedCaptureQueue implements AutoCloseable {
	@FunctionalInterface
	public interface JobSink {
		void accept(CaptureJob job) throws Exception;
	}

	private final ArrayBlockingQueue<CaptureJob> queue;
	private final JobSink sink;
	private final BiConsumer<CaptureJob, Exception> failureHandler;
	private final AtomicBoolean accepting = new AtomicBoolean(true);
	private final AtomicBoolean running = new AtomicBoolean(true);
	private final AtomicBoolean active = new AtomicBoolean(false);
	private final AtomicLong completed = new AtomicLong();
	private final AtomicLong failed = new AtomicLong();
	private final Thread worker;

	public BoundedCaptureQueue(
			int capacity, JobSink sink, BiConsumer<CaptureJob, Exception> failureHandler) {
		if (capacity < 1) {
			throw new IllegalArgumentException("queue capacity must be positive");
		}
		this.queue = new ArrayBlockingQueue<>(capacity);
		this.sink = Objects.requireNonNull(sink, "sink");
		this.failureHandler = Objects.requireNonNull(failureHandler, "failureHandler");
		this.worker = Thread.ofPlatform()
				.name("worldledger-capture-writer")
				.daemon(true)
				.unstarted(this::runWorker);
		this.worker.start();
	}

	public boolean offer(CaptureJob job) {
		return accepting.get() && queue.offer(Objects.requireNonNull(job, "job"));
	}

	/**
	 * Offers a job, waiting up to the given time for the writer to make room.
	 *
	 * <p>The non-waiting {@link #offer(CaptureJob)} exists to keep game threads
	 * free during play: a chunk that cannot be enqueued stays dirty and is
	 * retried. That trade stops making sense once a session is ending, because
	 * there is no further gameplay to protect and a refused job is observed
	 * state that is simply lost. This variant lets a final flush wait for the
	 * writer instead, while keeping both bounds that matter: the queue never
	 * grows, and the caller always gets control back within the timeout.
	 *
	 * @return true if the job was accepted before the timeout elapsed
	 */
	public boolean offer(CaptureJob job, Duration timeout) throws InterruptedException {
		Objects.requireNonNull(job, "job");
		Objects.requireNonNull(timeout, "timeout");
		if (!accepting.get()) {
			return false;
		}
		if (timeout.isNegative() || timeout.isZero()) {
			return queue.offer(job);
		}
		return queue.offer(job, timeout.toNanos(), TimeUnit.NANOSECONDS);
	}

	public int pending() {
		return queue.size() + (active.get() ? 1 : 0);
	}

	public long completed() {
		return completed.get();
	}

	public long failed() {
		return failed.get();
	}

	public boolean closeGracefully(Duration timeout) throws InterruptedException {
		accepting.set(false);
		long deadline = System.nanoTime() + timeout.toNanos();
		while ((!queue.isEmpty() || active.get()) && System.nanoTime() < deadline) {
			Thread.sleep(10);
		}
		running.set(false);
		worker.interrupt();
		long remainingNanos = Math.max(0, deadline - System.nanoTime());
		worker.join(Math.max(1, TimeUnit.NANOSECONDS.toMillis(remainingNanos)));
		return queue.isEmpty() && !active.get() && !worker.isAlive();
	}

	@Override
	public void close() {
		try {
			closeGracefully(Duration.ofSeconds(5));
		} catch (InterruptedException exception) {
			Thread.currentThread().interrupt();
		}
	}

	private void runWorker() {
		while (running.get() || !queue.isEmpty()) {
			CaptureJob job;
			try {
				job = queue.poll(250, TimeUnit.MILLISECONDS);
			} catch (InterruptedException exception) {
				if (!running.get()) {
					continue;
				}
				Thread.currentThread().interrupt();
				return;
			}
			if (job == null) {
				continue;
			}
			active.set(true);
			try {
				sink.accept(job);
				completed.incrementAndGet();
			} catch (Exception exception) {
				failed.incrementAndGet();
				failureHandler.accept(job, exception);
			} finally {
				active.set(false);
			}
		}
	}
}
