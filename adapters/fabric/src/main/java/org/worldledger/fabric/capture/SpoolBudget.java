package org.worldledger.fabric.capture;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.FileVisitResult;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.nio.file.SimpleFileVisitor;
import java.nio.file.attribute.BasicFileAttributes;
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
 *
 * <p>The ceiling counts what the bundles come to, which is not what the folder
 * occupies. A component repeated across bundles is written once and hard-linked
 * from each of them, so the disk holds one copy where this counts several, and
 * on a session that revisits the same terrain it can be counting many. That is
 * deliberate rather than an oversight to be corrected: the declared total is
 * the same number on every filesystem, it is what an import will have to read,
 * and it can be arrived at without asking a filesystem questions that Windows
 * does not answer. Keeping the disk itself safe is what minFreeBytes is for,
 * and that one is a reading.
 *
 * <p>What does not follow is telling somebody their spool holds four gigabytes
 * when the folder they are about to open holds a fraction of it, so the notice
 * says what was counted.
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

	/**
	 * @param bundleBytes what the spool's bundles come to, counting a shared
	 *     component once per bundle that names it
	 * @param usableBytes what the filesystem says is free, or -1 if it would
	 *     not say
	 */
	public record Status(State state, long bundleBytes, long usableBytes, String detail) {
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
					"the spooled bundles come to %s against a budget of %s; import and clear the"
							+ " spool to resume capture. Bundles share their repeated components,"
							+ " so the folder itself holds less than that",
					readable(used), readable(maxBytes)));
		}
		if (minFreeBytes > 0 && usable >= 0 && usable < minFreeBytes) {
			return new Status(State.DISK_LOW, used, usable, String.format(
					"only %s free on the spool's filesystem and %s is required",
					readable(usable), readable(minFreeBytes)));
		}
		return new Status(State.OK, used, usable, "within budget");
	}

	/**
	 * What the spool's files add up to, including partial entries.
	 *
	 * <p>A sum of declared sizes, so a component hard-linked into several
	 * bundles is counted once for each of them. See the note on this class for
	 * why that is the number the ceiling is set against.
	 */
	public long measure() {
		if (!Files.isDirectory(spoolDirectory)) {
			return 0L;
		}
		long total = 0L;
		try (Stream<Path> top = Files.list(spoolDirectory)) {
			for (Path entry : (Iterable<Path>) top::iterator) {
				if (!countsTowardBudget(entry.getFileName().toString())) {
					continue;
				}
				total += sizeOfTree(entry);
			}
		} catch (NoSuchFileException vanished) {
			// The spool itself went while this was reading it, which is what an
			// import doing its job looks like. Nothing left to count.
			return total;
		} catch (IOException exception) {
			throw new UncheckedIOException(exception);
		}
		return total;
	}

	/**
	 * Whether an entry in the spool is work this budget is meant to bound.
	 *
	 * <p>The budget exists to stop an unattended client filling a disk with
	 * captures nobody has taken in yet. It used to add up every file under the
	 * spool, and the other half of this project renames a bundle to
	 * {@code imported-} rather than deleting it, on the reasoning that a window
	 * must not destroy somebody's only copy on the strength of one button. That
	 * reasoning is right and its note says the new name "is invisible to" the
	 * adapter, which was the part that was not true.
	 *
	 * <p>So a contributor who imports every evening accumulated toward the
	 * ceiling anyway, and when it tripped, capture stopped for good and the
	 * notice told them to import and clear the spool: exactly what they had been
	 * doing. Quarantined bundles counted too, and nothing on either side of the
	 * boundary ever removes those.
	 */
	private static boolean countsTowardBudget(String name) {
		return name.startsWith("ready-") || name.startsWith(".tmp-");
	}

	/**
	 * Adds up one bundle, tolerating its disappearance.
	 *
	 * <p>An import deletes bundles while the client is still running, which is
	 * the documented next step and not a race anybody should have to avoid.
	 * {@link Files#walk} aborts the whole traversal when a subtree vanishes
	 * mid-walk, and the resulting UncheckedIOException escaped the budget check,
	 * left the writer's failure handler with something that was not a full
	 * spool, and lost the chunk that was being written.
	 */
	private static long sizeOfTree(Path root) throws IOException {
		long[] total = {0L};
		Files.walkFileTree(root, new SimpleFileVisitor<Path>() {
			@Override
			public FileVisitResult visitFile(Path file, BasicFileAttributes attributes) {
				if (attributes.isRegularFile()) {
					total[0] += attributes.size();
				}
				return FileVisitResult.CONTINUE;
			}

			@Override
			public FileVisitResult visitFileFailed(Path file, IOException failure) {
				// Gone between being listed and being read. It is not in the
				// spool, so it is not in the total.
				return FileVisitResult.CONTINUE;
			}
		});
		return total[0];
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
