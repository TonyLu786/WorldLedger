package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Comparator;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

/**
 * A full-height chunk is twenty-four sections and most of them are uniform, so
 * different chunks produce byte-identical section components in quantity. A
 * real capture held fifty distinct blobs across two thousand component files.
 *
 * <p>The bundle format is unchanged by the sharing: what these assert first is
 * that every bundle still contains every component it declares, with the right
 * bytes. The saving is checked second, and only where the file system offers
 * it.
 */
final class SpoolComponentSharingTest {
	@TempDir
	Path temporaryDirectory;

	private Path spool() {
		return temporaryDirectory.resolve("spool");
	}

	/** Two bundles whose components are identical, which is the case that repeats. */
	private Path[] writeTwoIdenticalBundles() throws IOException {
		BundleSpoolWriter writer = new BundleSpoolWriter(spool());
		return new Path[] {
			writer.write(CaptureTestFixtures.job(1)), writer.write(CaptureTestFixtures.job(2)),
		};
	}

	private static Path anyComponent(Path bundle) throws IOException {
		try (Stream<Path> entries = Files.walk(bundle.resolve("components"))) {
			return entries.filter(Files::isRegularFile).sorted().findFirst().orElseThrow();
		}
	}

	/** Whether this file system can share a file between two names at all. */
	private boolean supportsHardLinks() {
		try {
			Path original = temporaryDirectory.resolve("probe");
			Files.writeString(original, "probe");
			Files.createLink(temporaryDirectory.resolve("probe-link"), original);
			return true;
		} catch (IOException | UnsupportedOperationException | SecurityException unsupported) {
			return false;
		}
	}

	@Test
	void everyBundleStillHoldsItsOwnComponentsWithTheDeclaredBytes() throws Exception {
		Path[] bundles = writeTwoIdenticalBundles();

		for (Path bundle : bundles) {
			assertTrue(Files.isRegularFile(bundle.resolve("bundle.json")), bundle + " has no manifest");
			assertTrue(Files.isDirectory(bundle.resolve("components")), bundle + " has no components");
		}
		assertArrayEquals(
				Files.readAllBytes(anyComponent(bundles[0])),
				Files.readAllBytes(anyComponent(bundles[1])),
				"the two bundles should declare the same bytes for this test to mean anything");
	}

	@Test
	void identicalComponentsAreStoredOnce() throws Exception {
		Path[] bundles = writeTwoIdenticalBundles();
		if (!supportsHardLinks()) {
			return;
		}
		assertTrue(
				Files.isSameFile(anyComponent(bundles[0]), anyComponent(bundles[1])),
				"identical components should share one file on a file system that allows it");
	}

	/**
	 * The property that makes sharing safe. Hard links are peers rather than a
	 * copy and a reference, so importing one bundle and deleting it must not
	 * empty another bundle that happened to be written second.
	 */
	@Test
	void deletingOneBundleLeavesTheOtherReadable() throws Exception {
		Path[] bundles = writeTwoIdenticalBundles();
		byte[] expected = Files.readAllBytes(anyComponent(bundles[1]));

		try (Stream<Path> entries = Files.walk(bundles[0])) {
			entries.sorted(Comparator.reverseOrder()).forEach(path -> {
				try {
					Files.delete(path);
				} catch (IOException exception) {
					throw new RuntimeException(exception);
				}
			});
		}
		assertTrue(Files.notExists(bundles[0]), "the first bundle should be gone");
		assertArrayEquals(
				expected,
				Files.readAllBytes(anyComponent(bundles[1])),
				"deleting an imported bundle must not disturb one that shares its bytes");
	}

	/**
	 * A remembered file that has since been deleted must not stop the next
	 * bundle being written. The bytes get written again, which is what would
	 * have happened without sharing at all.
	 */
	@Test
	void aBundleIsStillWrittenAfterTheSharedCopyDisappears() throws Exception {
		BundleSpoolWriter writer = new BundleSpoolWriter(spool());
		Path first = writer.write(CaptureTestFixtures.job(1));
		byte[] expected = Files.readAllBytes(anyComponent(first));

		try (Stream<Path> entries = Files.walk(first)) {
			entries.sorted(Comparator.reverseOrder()).forEach(path -> {
				try {
					Files.delete(path);
				} catch (IOException exception) {
					throw new RuntimeException(exception);
				}
			});
		}

		Path second = writer.write(CaptureTestFixtures.job(2));
		assertArrayEquals(
				expected,
				Files.readAllBytes(anyComponent(second)),
				"a bundle written after the shared copy was removed must still carry its bytes");
	}
}
