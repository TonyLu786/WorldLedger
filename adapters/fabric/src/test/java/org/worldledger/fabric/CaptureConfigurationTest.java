package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

final class CaptureConfigurationTest {
	@TempDir
	Path temporaryDirectory;

	@Test
	void defaultFileIsDisabledUntilContributorIsExplicit() throws Exception {
		CaptureConfiguration configuration = CaptureConfiguration.loadOrCreate(temporaryDirectory);
		assertFalse(configuration.enabled());
		String contents = Files.readString(temporaryDirectory.resolve("capture.properties"), StandardCharsets.UTF_8);
		assertTrue(contents.contains("contributor=\n"));
		assertFalse(contents.contains("Sun "));
	}

	@Test
	void configuredBoundsAreParsedAndValidated() throws Exception {
		Files.writeString(
				temporaryDirectory.resolve("capture.properties"),
				"contributor=alice\nserver_id=archive.example\ncoalesce_ticks=4\nqueue_capacity=3\nmax_snapshots_per_tick=2\n",
				StandardCharsets.UTF_8);
		CaptureConfiguration configuration = CaptureConfiguration.loadOrCreate(temporaryDirectory);
		assertTrue(configuration.enabled());
		assertEquals("alice", configuration.contributor());
		assertEquals("archive.example", configuration.serverId());
		assertEquals(4, configuration.coalesceTicks());
		assertEquals(3, configuration.queueCapacity());
		assertEquals(2, configuration.maxSnapshotsPerTick());

		Files.writeString(
				temporaryDirectory.resolve("capture.properties"),
				"contributor=alice\nqueue_capacity=0\n",
				StandardCharsets.UTF_8);
		assertThrows(
				IllegalArgumentException.class,
				() -> CaptureConfiguration.loadOrCreate(temporaryDirectory));
	}

	@Test
	void localConfigurationInputIsSizeBounded() throws Exception {
		Path path = temporaryDirectory.resolve("capture.properties");
		Files.write(path, new byte[(64 << 10) + 1]);
		assertThrows(IOException.class, () -> CaptureConfiguration.loadOrCreate(temporaryDirectory));

		Files.writeString(path, "contributor=" + "a".repeat(257) + "\n", StandardCharsets.UTF_8);
		assertThrows(
				IllegalArgumentException.class,
				() -> CaptureConfiguration.loadOrCreate(temporaryDirectory));
	}
}
