package org.worldledger.fabric;

import java.io.IOException;
import java.io.InputStream;
import java.io.Reader;
import java.io.StringReader;
import java.nio.ByteBuffer;
import java.nio.channels.FileChannel;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.nio.file.FileAlreadyExistsException;
import java.util.Properties;
import java.util.UUID;

public record CaptureConfiguration(
		String contributor,
		String serverId,
		int coalesceTicks,
		int queueCapacity,
		int maxSnapshotsPerTick) {
	private static final String FILE_NAME = "capture.properties";
	private static final int MAX_CONFIG_BYTES = 64 << 10;

	/**
	 * A live 26.2 session retried 87 chunks against a queue of 8, because
	 * joining a server releases a whole view distance at once while the writer
	 * canonicalises one chunk at a time. Retries are safe but they postpone
	 * coverage, and a session short enough can end before the backlog clears.
	 *
	 * <p>A queue entry holds one canonical chunk snapshot, measured at roughly
	 * 214 KiB for a full-height overworld chunk, so this bounds in-flight
	 * capture memory at about 7 MiB. That is the trade being made: a few
	 * megabytes of a client's heap against coverage that would otherwise arrive
	 * late or not at all.
	 */
	private static final int DEFAULT_QUEUE_CAPACITY = 32;

	private static final byte[] DEFAULT_FILE = ("# WorldLedger capture adapter\n"
			+ "# Set contributor before joining a multiplayer server. Blank disables capture.\n"
			+ "contributor=\n"
			+ "# Optional stable archive id. Blank uses the normalized server address.\n"
			+ "server_id=\n"
			+ "coalesce_ticks=10\n"
			+ "# Chunk snapshots held in memory awaiting the writer. Each entry is\n"
			+ "# roughly 214 KiB for a full-height overworld chunk.\n"
			+ "queue_capacity=" + DEFAULT_QUEUE_CAPACITY + "\n"
			+ "max_snapshots_per_tick=1\n")
			.getBytes(StandardCharsets.UTF_8);

	public CaptureConfiguration {
		contributor = contributor.trim();
		serverId = serverId.trim();
		if (contributor.getBytes(StandardCharsets.UTF_8).length > 256) {
			throw new IllegalArgumentException("contributor exceeds 256 UTF-8 bytes");
		}
		if (serverId.getBytes(StandardCharsets.UTF_8).length > 512) {
			throw new IllegalArgumentException("server_id exceeds 512 UTF-8 bytes");
		}
		if (coalesceTicks < 1 || coalesceTicks > 1200) {
			throw new IllegalArgumentException("coalesce_ticks must be in 1..1200");
		}
		if (queueCapacity < 1 || queueCapacity > 64) {
			throw new IllegalArgumentException("queue_capacity must be in 1..64");
		}
		if (maxSnapshotsPerTick < 1 || maxSnapshotsPerTick > 8) {
			throw new IllegalArgumentException("max_snapshots_per_tick must be in 1..8");
		}
	}

	public boolean enabled() {
		return !contributor.isEmpty();
	}

	public static CaptureConfiguration loadOrCreate(Path configDirectory) throws IOException {
		Path path = configDirectory.resolve(FILE_NAME);
		if (Files.notExists(path)) {
			writeDefault(path);
		}
		byte[] data;
		try (InputStream input = Files.newInputStream(path)) {
			data = input.readNBytes(MAX_CONFIG_BYTES + 1);
		}
		if (data.length > MAX_CONFIG_BYTES) {
			throw new IOException("capture.properties exceeds " + MAX_CONFIG_BYTES + " bytes");
		}
		Properties properties = new Properties();
		try (Reader reader = new StringReader(new String(data, StandardCharsets.UTF_8))) {
			properties.load(reader);
		}
		return new CaptureConfiguration(
				properties.getProperty("contributor", ""),
				properties.getProperty("server_id", ""),
				parseInt(properties, "coalesce_ticks", 10),
				parseInt(properties, "queue_capacity", DEFAULT_QUEUE_CAPACITY),
				parseInt(properties, "max_snapshots_per_tick", 1));
	}

	private static int parseInt(Properties properties, String name, int defaultValue) {
		String raw = properties.getProperty(name);
		if (raw == null || raw.isBlank()) {
			return defaultValue;
		}
		try {
			return Integer.parseInt(raw.trim());
		} catch (NumberFormatException exception) {
			throw new IllegalArgumentException(name + " must be an integer", exception);
		}
	}

	private static void writeDefault(Path path) throws IOException {
		Files.createDirectories(path.getParent());
		Path temporary = path.getParent().resolve("." + FILE_NAME + ".tmp-" + UUID.randomUUID());
		try (FileChannel channel = FileChannel.open(
				temporary, StandardOpenOption.CREATE_NEW, StandardOpenOption.WRITE)) {
			ByteBuffer buffer = ByteBuffer.wrap(DEFAULT_FILE);
			while (buffer.hasRemaining()) {
				channel.write(buffer);
			}
			channel.force(true);
		}
		try {
			// The default move refuses an existing target. This avoids overwriting a
			// contributor value written concurrently while the default was staged.
			Files.move(temporary, path);
		} catch (FileAlreadyExistsException exception) {
			// Another process completed the same initialization first.
		} finally {
			Files.deleteIfExists(temporary);
		}
	}
}
