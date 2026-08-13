package org.worldledger.fabric.capture;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Objects;
import java.util.stream.Stream;

/**
 * Watches how much disk the spool is using and says when to stop.
 *
 * <p>The spool grows until someone imports it. A capture bundle for a
 * full-height overworld chunk is around 214 KiB, so a session that explores
 * steadily produces tens of megabytes and an unattended client can fill a disk.
 * Individual bundles were already bounded; the total was not.
 *
 * <p>When the budget is reached this stops accepting new work rather than
 * deleting old work. Discarding a spooled bundle would destroy an observation
 * that was already taken and already survived a crash-safe publish, in order to
 * make room for one that has not been taken yet. Refusing to capture more loses
 * only what was never recorded, and it is visible: the session reports that it
 * stopped and why.
 */
public final class SpoolBudget {
	/**
	 * Default ceiling for the spool. Large enough for a long session at roughly
	 * 214 KiB per chunk, small enough that an unattended client cannot quietly
	 * consume a disk.
	 */
	public static final long DEFAULT_MAX_BYTES = 4L * 1024 * 1024 * 1024;

	/** Keep this much of the filesystem free regardless of the byte ceiling. */
	public static final long DEFAULT_MIN_FREE_BYTES = 2L * 1024 * 1024 * 1024;

	public enum State {
		/** Capture may continue. */
		OK,
		/** The configured spool ceiling was reached. */
		SPOOL_FULL,
		/** The filesystem is close to full, whatever the spool is using. */
		DISK_LOW
	}

	public record Status(State state, long spoolBytes, long usableBytes, String detail) {
		public Status {
			Objects.requireNonNull(state, "state");
			Objects.requireNonNull(detail, "detail");
		}

		public boolean allowsCapture() {
			return state == State.OK;
		}
	}

	private final Path spoolDirectory;
	private final long maxBytes;
	private final long minFreeBytes;

	public SpoolBudget(Path spoolDirectory, long maxBytes, long minFreeBytes) {
		this.spoolDirectory = Objects.requireNonNull(spoolDirectory, "spoolDirectory");
		if (maxBytes < 1) {
			throw new IllegalArgumentException("maxBytes must be positive");
		}
		if (minFreeBytes < 0) {
			throw new IllegalArgumentException("minFreeBytes must not be negative");
		}
		this.maxBytes = maxBytes;
		this.minFreeBytes = minFreeBytes;
	}

	public static SpoolBudget withDefaults(Path spoolDirectory) {
		return new SpoolBudget(spoolDirectory, DEFAULT_MAX_BYTES, DEFAULT_MIN_FREE_BYTES);
	}

	/** Measures the spool and reports whether capture may continue. */
	public Status check() {
		long used = measure();
		long usable = usableSpace();

		if (used >= maxBytes) {
			return new Status(State.SPOOL_FULL, used, usable, String.format(
					"spool holds %s and the budget is %s; import and clear it to resume capture",
					readable(used), readable(maxBytes)));
		}
		if (minFreeBytes > 0 && usable >= 0 && usable < minFreeBytes) {
			return new Status(State.DISK_LOW, used, usable, String.format(
					"only %s free on the spool's filesystem and %s is required",
					readable(usable), readable(minFreeBytes)));
		}
		return new Status(State.OK, used, usable, "within budget");
	}

	/** Total bytes currently held by the spool, including partial entries. */
	public long measure() {
		if (!Files.isDirectory(spoolDirectory)) {
			return 0L;
		}
		try (Stream<Path> entries = Files.walk(spoolDirectory)) {
			return entries.filter(Files::isRegularFile).mapToLong(SpoolBudget::sizeOf).sum();
		} catch (IOException exception) {
			throw new UncheckedIOException(exception);
		}
	}

	private long usableSpace() {
		try {
			Path existing = spoolDirectory;
			while (existing != null && !Files.exists(existing)) {
				existing = existing.getParent();
			}
			if (existing == null) {
				return -1L;
			}
			return Files.getFileStore(existing).getUsableSpace();
		} catch (IOException exception) {
			// An unreadable file store must not stop capture; the byte ceiling
			// still applies.
			return -1L;
		}
	}

	private static long sizeOf(Path path) {
		try {
			return Files.size(path);
		} catch (IOException exception) {
			return 0L;
		}
	}

	static String readable(long bytes) {
		if (bytes < 1024) {
			return bytes + " B";
		}
		String units = "KMGTPE";
		int exponent = 0;
		double value = bytes / 1024.0;
		while (value >= 1024 && exponent < units.length() - 1) {
			value /= 1024;
			exponent++;
		}
		return String.format("%.1f %ciB", value, units.charAt(exponent));
	}
}
