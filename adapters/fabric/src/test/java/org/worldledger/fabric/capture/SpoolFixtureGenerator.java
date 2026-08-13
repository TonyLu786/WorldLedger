package org.worldledger.fabric.capture;

import java.nio.file.Files;
import java.nio.file.Path;

public final class SpoolFixtureGenerator {
	private SpoolFixtureGenerator() {}

	public static void main(String[] arguments) throws Exception {
		if (arguments.length != 1) {
			throw new IllegalArgumentException("usage: SpoolFixtureGenerator <empty-spool-directory>");
		}
		Path spool = Path.of(arguments[0]).toAbsolutePath().normalize();
		if (Files.exists(spool)) {
			try (var entries = Files.list(spool)) {
				if (entries.findAny().isPresent()) {
					throw new IllegalStateException("fixture spool directory must be empty: " + spool);
				}
			}
		}
		Files.createDirectories(spool);
		Path ready = new BundleSpoolWriter(spool).write(CaptureTestFixtures.job(417));
		Files.deleteIfExists(spool.resolve(BundleSpoolWriter.WRITER_LOCK_FILE));
		System.out.println(ready);
	}
}
