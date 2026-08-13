package org.worldledger.fabric;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Objects;

public record CapturePaths(Path configDirectory, Path spoolDirectory) {
	public CapturePaths {
		Objects.requireNonNull(configDirectory, "configDirectory");
		Objects.requireNonNull(spoolDirectory, "spoolDirectory");
	}

	public static CapturePaths fromFabricConfigDirectory(Path fabricConfigDirectory) {
		Path config = Objects.requireNonNull(fabricConfigDirectory, "fabricConfigDirectory")
				.resolve("worldledger")
				.toAbsolutePath()
				.normalize();
		return new CapturePaths(config, config.resolve("spool"));
	}

	public void ensureDirectories() throws IOException {
		Files.createDirectories(spoolDirectory);
	}
}
