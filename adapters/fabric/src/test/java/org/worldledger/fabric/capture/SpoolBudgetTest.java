package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Assumptions;
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
		assertEquals(0L, status.bundleBytes());
	}

	@Test
	void measurementIncludesNestedBundleComponents(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		writeBytes(spool.resolve("ready-a").resolve("components"), "blocks.bin", 400);
		writeBytes(spool.resolve("ready-a"), "bundle.json", 100);
		writeBytes(spool.resolve("ready-b").resolve("components"), "blocks.bin", 500);

		assertEquals(1000L, new SpoolBudget(spool, 1 << 20, 0).measure());
	}

	/**
	 * The writer stores a component repeated across bundles once and hard-links
	 * it from each, so this is a case where the sum is bigger than the folder.
	 *
	 * <p>Counting it per bundle is the choice, not an accident. A walk cannot
	 * tell a link from a copy on Windows, which is where most of this runs, so
	 * the alternative is a ceiling that means one thing on one filesystem and
	 * something else on another. What the change was for is the notice: it no
	 * longer tells somebody the folder holds a figure the folder does not hold.
	 */
	@Test
	void aComponentSharedBetweenBundlesCountsForEachBundleThatNamesIt(@TempDir Path root)
			throws IOException {
		Path spool = root.resolve("spool");
		Path first = spool.resolve("ready-a").resolve("components").resolve("blocks.bin");
		Files.createDirectories(first.getParent());
		Files.write(first, new byte[400]);

		Path second = spool.resolve("ready-b").resolve("components").resolve("blocks.bin");
		Files.createDirectories(second.getParent());
		try {
			Files.createLink(second, first);
		} catch (IOException | UnsupportedOperationException | SecurityException unsupported) {
			Assumptions.abort("this filesystem has no hard links, which is the whole subject");
		}

		assertEquals(800L, new SpoolBudget(spool, 1 << 20, 0).measure());
	}

	/**
	 * The notice a person acts on. It used to open with "spool holds 4.0 GiB",
	 * and somebody who then opened the folder would find a fraction of that and
	 * conclude the program was wrong about the one thing it was measuring.
	 */
	@Test
	void theNoticeDoesNotClaimTheFolderIsAsBigAsTheSum(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		writeBytes(spool.resolve("ready-a"), "bundle.json", 2000);

		String detail = new SpoolBudget(spool, 1000, 0).check().detail();
		assertTrue(detail.contains("bundles"), "what was counted is not named: " + detail);
		assertTrue(detail.contains("holds less"), "the folder is not smaller than the sum: " + detail);
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

	/**
	 * The budget bounds captures nobody has taken in yet. The other half of this
	 * project keeps a bundle after importing it, renaming it rather than deleting
	 * it so a window never destroys somebody's only copy, and its note says the
	 * new name is invisible to the adapter. It was not: everything counted, so a
	 * contributor who imported every evening still reached the ceiling, and when
	 * they did, capture stopped for good and the notice told them to import.
	 */
	@Test
	void bundlesAlreadyTakenInDoNotCountAgainstTheCeiling(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		writeBytes(spool.resolve("ready-a"), "bundle.json", 400);
		writeBytes(spool.resolve("imported-b"), "bundle.json", 4000);
		writeBytes(spool.resolve("quarantine-c"), "bundle.json", 4000);
		writeBytes(spool.resolve(".tmp-d"), "bundle.json", 100);

		assertEquals(500L, new SpoolBudget(spool, 1 << 20, 0).measure(),
				"only work still waiting to be imported may count");
	}

	/**
	 * Importing is the documented next step and it deletes bundles while the
	 * client is still running. A walk that aborts when a subtree vanishes threw
	 * out of the budget check, past the writer's full-spool handling, and lost
	 * the chunk being written.
	 */
	@Test
	void aBundleDeletedWhileTheSpoolIsBeingMeasuredIsNotAnError(@TempDir Path root) throws IOException {
		Path spool = root.resolve("spool");
		writeBytes(spool.resolve("ready-a"), "bundle.json", 400);
		Path vanishing = spool.resolve("ready-b");
		writeBytes(vanishing, "bundle.json", 400);

		// The state a walk sees when an import removes the payload between
		// listing the bundle and reading inside it.
		Files.delete(vanishing.resolve("bundle.json"));

		assertEquals(400L, new SpoolBudget(spool, 1 << 20, 0).measure(),
				"a bundle that went away mid-measure should contribute nothing, not throw");
	}

	@Test
	void aSpoolThatDisappearsEntirelyMeasuresZero(@TempDir Path root) {
		assertEquals(0L, new SpoolBudget(root.resolve("never-existed"), 1 << 20, 0).measure());
	}
}
