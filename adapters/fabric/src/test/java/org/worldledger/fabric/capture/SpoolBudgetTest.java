package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

final class SpoolBudgetTest {
	private static void writeBytes(Path directory, String name, int count) throws IOException {
		Files.createDirectories(directory);
		Files.write(directory.resolve(name), new byte[count]);
	}

	@Test
	void anEmptySpoolIsWithinBudget(@TempDir Path root) {
		SpoolBudget budget = new SpoolBudget(root.resolve("spool"), 1024, 0);
		SpoolBudget.Status status = budget.check();
		assertTrue(status.allowsCapture());
		assertEquals(0L, status.spoolBytes());
	}

	@Test
	void measurementIncludesNestedBundleComponents(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		writeBytes(spool.resolve("ready-a").resolve("components"), "blocks.bin", 400);
		writeBytes(spool.resolve("ready-a"), "bundle.json", 100);
		writeBytes(spool.resolve("ready-b").resolve("components"), "blocks.bin", 500);

		assertEquals(1000L, new SpoolBudget(spool, 1 << 20, 0).measure());
	}

	@Test
	void captureStopsWhenTheBudgetIsReached(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		writeBytes(spool.resolve("ready-a"), "bundle.json", 900);

		SpoolBudget.Status under = new SpoolBudget(spool, 1000, 0).check();
		assertTrue(under.allowsCapture());

		writeBytes(spool.resolve("ready-b"), "bundle.json", 200);
		SpoolBudget.Status over = new SpoolBudget(spool, 1000, 0).check();
		assertFalse(over.allowsCapture());
		assertEquals(SpoolBudget.State.SPOOL_FULL, over.state());
		assertTrue(over.detail().contains("import"), "the operator should be told how to resume: " + over.detail());
	}

	/**
	 * Reaching the budget must not delete anything. A spooled bundle is an
	 * observation that was already taken and already published crash-safely;
	 * discarding it to make room for one not yet taken would trade recorded
	 * evidence for hypothetical evidence.
	 */
	@Test
	void reachingTheBudgetPreservesWhatIsAlreadySpooled(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		Path bundle = spool.resolve("ready-a");
		writeBytes(bundle, "bundle.json", 2000);
		byte[] before = Files.readAllBytes(bundle.resolve("bundle.json"));

		SpoolBudget budget = new SpoolBudget(spool, 1000, 0);
		assertFalse(budget.check().allowsCapture());

		assertTrue(Files.exists(bundle.resolve("bundle.json")), "the spooled bundle was removed");
		assertEquals(before.length, Files.readAllBytes(bundle.resolve("bundle.json")).length);
	}

	@Test
	void aFullFilesystemStopsCaptureEvenBelowTheByteCeiling(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		writeBytes(spool.resolve("ready-a"), "bundle.json", 10);

		// A free-space requirement larger than any real disk forces the check.
		SpoolBudget budget = new SpoolBudget(spool, Long.MAX_VALUE, Long.MAX_VALUE);
		SpoolBudget.Status status = budget.check();
		assertFalse(status.allowsCapture());
		assertEquals(SpoolBudget.State.DISK_LOW, status.state());
	}

	@Test
	void defaultsLeaveHeadroomForALongSession() {
		// Roughly 214 KiB per full-height chunk, so the default ceiling holds
		// well over ten thousand chunk snapshots.
		assertTrue(SpoolBudget.DEFAULT_MAX_BYTES / (214L * 1024) > 10_000);
		assertTrue(SpoolBudget.DEFAULT_MIN_FREE_BYTES > 0);
	}

	@Test
	void sizesAreReportedInUnitsAPersonCanRead() {
		assertEquals("512 B", SpoolBudget.readable(512));
		assertNotEquals("0 B", SpoolBudget.readable(4L * 1024 * 1024 * 1024));
		assertTrue(SpoolBudget.readable(4L * 1024 * 1024 * 1024).contains("GiB"));
	}

	@Test
	void invalidBudgetsAreRejected(@TempDir Path root) {
		try {
			new SpoolBudget(root, 0, 0);
			throw new AssertionError("a zero ceiling was accepted");
		} catch (IllegalArgumentException expected) {
			assertTrue(expected.getMessage().contains("maxBytes"));
		}
		try {
			new SpoolBudget(root, 1, -1);
			throw new AssertionError("a negative free-space requirement was accepted");
		} catch (IllegalArgumentException expected) {
			assertTrue(expected.getMessage().contains("minFreeBytes"));
		}
	}

	@Test
	void measurementToleratesAMissingSpool(@TempDir Path root) {
		assertEquals(0L, SpoolBudget.withDefaults(root.resolve("absent")).measure());
		assertEquals(
				"ready".getBytes(StandardCharsets.UTF_8).length,
				"ready".length(),
				"guard against a platform default charset changing the fixture");
	}
}
