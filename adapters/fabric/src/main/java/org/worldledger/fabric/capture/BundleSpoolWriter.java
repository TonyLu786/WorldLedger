package org.worldledger.fabric.capture;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.google.gson.Strictness;
import com.google.gson.stream.JsonReader;
import com.google.gson.stream.JsonToken;
import java.io.IOException;
import java.io.InputStream;
import java.io.StringReader;
import java.nio.ByteBuffer;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.channels.SeekableByteChannel;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.OpenOption;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.StandardOpenOption;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashSet;
import java.util.HexFormat;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.UUID;
import java.util.concurrent.locks.ReentrantLock;
import java.util.function.Supplier;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.worldledger.fabric.canonical.MinecraftJavaV1;

public final class BundleSpoolWriter {
	public static final String SCHEMA = "worldledger.capture-bundle/v1";
	static final String WRITER_LOCK_FILE = ".writer.lock";
	private static final int MAX_MANIFEST_BYTES = 1 << 20;
	private static final int MAX_COMPONENTS = 256;
	private static final long MAX_COMPONENT_BYTES = 64L << 20;
	private static final long MAX_TOTAL_BYTES = 512L << 20;
	private static final Logger LOGGER = LoggerFactory.getLogger("worldledger");
	private static final ReentrantLock JVM_SPOOL_LOCK = new ReentrantLock();

	public record RecoveryReport(int recovered, int quarantined, List<String> diagnostics) {
		public RecoveryReport {
			diagnostics = List.copyOf(diagnostics);
		}
	}

	private record ComponentFile(String relativePath, byte[] data, String sha256) {}

	private static final Gson GSON = new GsonBuilder()
			.disableHtmlEscaping()
			.setPrettyPrinting()
			.create();
	private final Path spoolDirectory;

	@FunctionalInterface
	private interface IoOperation<T> {
		T run() throws IOException;
	}

	public BundleSpoolWriter(Path spoolDirectory) {
		this.spoolDirectory = spoolDirectory.toAbsolutePath().normalize();
	}

	public Path write(CaptureJob job) throws IOException {
		return withSpoolLock(() -> writeLocked(job));
	}

	private Path writeLocked(CaptureJob job) throws IOException {
		TreeMap<String, ComponentFile> components = encodeComponents(job);
		byte[] manifest = buildManifest(job, components);
		if (manifest.length > MAX_MANIFEST_BYTES) {
			throw new IOException("capture manifest exceeds " + MAX_MANIFEST_BYTES + " bytes");
		}
		Path temporary = Files.createDirectory(spoolDirectory.resolve(".tmp-" + UUID.randomUUID()));
		boolean manifestWritten = false;
		try {
			for (ComponentFile component : components.values()) {
				Path output = resolveComponentPath(temporary, component.relativePath());
				Files.createDirectories(output.getParent());
				writeSynced(output, component.data());
			}
			writeSynced(temporary.resolve("bundle.json"), manifest);
			manifestWritten = true;
			syncDirectory(temporary);
			Path ready = readyPath(job.sessionId(), job.sequence());
			moveWithoutReplace(temporary, ready);
			syncDirectory(spoolDirectory);
			return ready;
		} catch (IOException | RuntimeException exception) {
			if (manifestWritten) {
				syncDirectoryBestEffort(temporary);
			}
			throw exception;
		}
	}

	public RecoveryReport recoverTemporaryBundles() throws IOException {
		return withSpoolLock(this::recoverTemporaryBundlesLocked);
	}

	private RecoveryReport recoverTemporaryBundlesLocked() throws IOException {
		List<Path> temporaryDirectories;
		try (var stream = Files.list(spoolDirectory)) {
			temporaryDirectories = stream
					.filter(path -> path.getFileName().toString().startsWith(".tmp-"))
					.sorted(Comparator.comparing(path -> path.getFileName().toString()))
					.toList();
		}
		int recovered = 0;
		int quarantined = 0;
		List<String> diagnostics = new ArrayList<>();
		for (Path temporary : temporaryDirectories) {
			try {
				JsonObject manifest = validateTemporaryBundle(temporary);
				JsonObject capture = manifest.getAsJsonObject("capture");
				String sessionId = capture.get("session_id").getAsString();
				long sequence = capture.get("sequence").getAsLong();
				Path ready = readyPath(sessionId, sequence);
				moveWithoutReplace(temporary, ready);
				recovered++;
				diagnostics.add("recovered " + temporary.getFileName() + " as " + ready.getFileName());
			} catch (Exception exception) {
				Path quarantine = uniqueQuarantinePath();
				moveWithoutReplace(temporary, quarantine);
				quarantined++;
				diagnostics.add("quarantined " + temporary.getFileName() + ": " + exception.getMessage());
			}
		}
		if (recovered > 0 || quarantined > 0) {
			syncDirectory(spoolDirectory);
		}
		return new RecoveryReport(recovered, quarantined, diagnostics);
	}

	private TreeMap<String, ComponentFile> encodeComponents(CaptureJob job) {
		TreeMap<String, ComponentFile> components = new TreeMap<>();
		putComponent(components, "mcjava.shape", "components/shape.bin", MinecraftJavaV1.encodeShape(
				job.minSectionY(), job.sectionCount()));
		for (CaptureJob.SectionSnapshot section : job.sections()) {
			section.blocks().ifPresent(blocks -> putObservedComponent(
					job,
					components,
					"mcjava.blocks." + section.sectionY(),
					"components/blocks_" + section.sectionY() + ".bin",
					() -> MinecraftJavaV1.encodeBlockSection(section.sectionY(), blocks)));
			section.biomes().ifPresent(biomes -> putObservedComponent(
					job,
					components,
					"mcjava.biomes." + section.sectionY(),
					"components/biomes_" + section.sectionY() + ".bin",
					() -> MinecraftJavaV1.encodeBiomeSection(section.sectionY(), biomes)));
		}
		job.blockEntities().ifPresent(entries -> putObservedComponent(
				job,
				components,
				"mcjava.block_entities",
				"components/block_entities.bin",
				() -> MinecraftJavaV1.encodeBlockEntities(entries)));
		if (components.size() > MAX_COMPONENTS) {
			throw new IllegalArgumentException("capture bundle exceeds component-count limit");
		}
		long totalBytes = components.values().stream()
				.mapToLong(component -> component.data().length)
				.sum();
		if (totalBytes > MAX_TOTAL_BYTES) {
			throw new IllegalArgumentException("capture bundle exceeds aggregate byte limit");
		}
		return components;
	}

	private static void putObservedComponent(
			CaptureJob job,
			Map<String, ComponentFile> components,
			String name,
			String relativePath,
			Supplier<byte[]> encoder) {
		try {
			putComponent(components, name, relativePath, encoder.get());
		} catch (IllegalArgumentException exception) {
			LOGGER.warn(
					"Omitting component {} for capture session={} sequence={}: {}",
					name,
					job.sessionId(),
					job.sequence(),
					exception.getMessage());
		}
	}

	private static void putComponent(
			Map<String, ComponentFile> components, String name, String relativePath, byte[] data) {
		if (data.length > MAX_COMPONENT_BYTES) {
			throw new IllegalArgumentException("component " + name + " exceeds the byte limit");
		}
		ComponentFile previous = components.put(name, new ComponentFile(relativePath, data, sha256(data)));
		if (previous != null) {
			throw new IllegalArgumentException("duplicate component " + name);
		}
	}

	private static byte[] buildManifest(CaptureJob job, Map<String, ComponentFile> components) {
		JsonObject root = new JsonObject();
		root.addProperty("schema", SCHEMA);
		root.addProperty("server_id", job.serverId());
		if (!job.serverAddress().isBlank()) {
			root.addProperty("server_address", job.serverAddress());
		}
		root.addProperty("dimension", job.dimension());
		JsonObject chunk = new JsonObject();
		chunk.addProperty("x", job.chunk().x());
		chunk.addProperty("z", job.chunk().z());
		root.add("chunk", chunk);
		root.addProperty("observed_at", job.observedAt().toString());
		root.addProperty("protocol", CaptureJob.PROTOCOL);
		JsonObject source = new JsonObject();
		source.addProperty("contributor", job.contributor());
		source.addProperty("agent", CaptureJob.AGENT);
		root.add("source", source);
		JsonObject capture = new JsonObject();
		capture.addProperty("session_id", job.sessionId());
		capture.addProperty("sequence", job.sequence());
		capture.addProperty("trigger", job.trigger());
		root.add("capture", capture);
		JsonObject descriptors = new JsonObject();
		for (Map.Entry<String, ComponentFile> entry : components.entrySet()) {
			JsonObject descriptor = new JsonObject();
			descriptor.addProperty("path", entry.getValue().relativePath());
			descriptor.addProperty("algorithm", "sha256");
			descriptor.addProperty("digest", entry.getValue().sha256());
			descriptor.addProperty("size", entry.getValue().data().length);
			descriptors.add(entry.getKey(), descriptor);
		}
		root.add("components", descriptors);
		return (GSON.toJson(root) + "\n").getBytes(StandardCharsets.UTF_8);
	}

	private JsonObject validateTemporaryBundle(Path temporary) throws IOException {
		if (!Files.isDirectory(temporary, LinkOption.NOFOLLOW_LINKS) || Files.isSymbolicLink(temporary)) {
			throw new IOException("temporary path is not a real directory");
		}
		Path manifestPath = temporary.resolve("bundle.json");
		if (!Files.isRegularFile(manifestPath, LinkOption.NOFOLLOW_LINKS) || Files.isSymbolicLink(manifestPath)) {
			throw new IOException("bundle.json is absent or indirect");
		}
		String manifestText = decodeUtf8(readLimited(manifestPath, MAX_MANIFEST_BYTES));
		validateJsonStructure(manifestText);
		JsonObject manifest;
		try {
			JsonElement parsed = JsonParser.parseString(manifestText);
			if (!parsed.isJsonObject()) {
				throw new IOException("bundle.json root is not an object");
			}
			manifest = parsed.getAsJsonObject();
		} catch (RuntimeException exception) {
			throw new IOException("bundle.json is invalid", exception);
		}
		requireOnlyFields(manifest, Set.of(
				"schema", "server_id", "server_address", "dimension", "chunk", "observed_at",
				"protocol", "source", "capture", "components"), "bundle");
		if (!SCHEMA.equals(requiredString(manifest, "schema"))) {
			throw new IOException("unexpected bundle schema");
		}
		requiredNonBlankString(manifest, "server_id");
		requiredNonBlankString(manifest, "dimension");
		requiredNonBlankString(manifest, "protocol");
		try {
			java.time.Instant.parse(requiredString(manifest, "observed_at"));
		} catch (java.time.format.DateTimeParseException exception) {
			throw new IOException("observed_at is invalid", exception);
		}
		if (manifest.has("server_address")) {
			requiredString(manifest, "server_address");
		}
		JsonObject chunk = requiredObject(manifest, "chunk");
		requireOnlyFields(chunk, Set.of("x", "z"), "chunk");
		requiredInt(chunk, "x");
		requiredInt(chunk, "z");
		JsonObject source = requiredObject(manifest, "source");
		requireOnlyFields(source, Set.of("contributor", "agent"), "source");
		requiredNonBlankString(source, "contributor");
		if (source.has("agent")) {
			requiredString(source, "agent");
		}
		JsonObject capture = requiredObject(manifest, "capture");
		requireOnlyFields(capture, Set.of("session_id", "sequence", "trigger"), "capture");
		long sequence = requiredNonNegativeLong(capture, "sequence");
		validateSessionAndSequence(requiredString(capture, "session_id"), sequence);
		if (capture.has("trigger")) {
			requiredString(capture, "trigger");
		}
		JsonObject components = requiredObject(manifest, "components");
		if (components.isEmpty()) {
			throw new IOException("components are absent");
		}
		if (components.size() > MAX_COMPONENTS) {
			throw new IOException("component count exceeds " + MAX_COMPONENTS);
		}
		Path realRoot = temporary.toRealPath();
		long totalBytes = 0;
		for (Map.Entry<String, JsonElement> entry : components.entrySet()) {
			if (entry.getKey().isBlank()) {
				throw new IOException("component name is blank");
			}
			JsonObject descriptor = requiredObject(entry.getValue(), "component " + entry.getKey());
			requireOnlyFields(descriptor, Set.of("path", "algorithm", "digest", "size"), "component " + entry.getKey());
			String relativePath = requiredString(descriptor, "path");
			if (!"sha256".equals(requiredString(descriptor, "algorithm"))) {
				throw new IOException("unsupported digest algorithm");
			}
			String expectedDigest = requiredString(descriptor, "digest");
			if (!validLowerSha256(expectedDigest)) {
				throw new IOException("component digest is not a lowercase SHA-256 value");
			}
			long expectedSize = requiredNonNegativeLong(descriptor, "size");
			if (expectedSize > MAX_COMPONENT_BYTES) {
				throw new IOException("component size exceeds " + MAX_COMPONENT_BYTES);
			}
			if (expectedSize > MAX_TOTAL_BYTES - totalBytes) {
				throw new IOException("aggregate component size exceeds " + MAX_TOTAL_BYTES);
			}
			totalBytes += expectedSize;
			Path component = resolveComponentPath(temporary, relativePath);
			if (!Files.isRegularFile(component, LinkOption.NOFOLLOW_LINKS) || Files.isSymbolicLink(component)) {
				throw new IOException("component is absent or indirect");
			}
			Path realComponent = component.toRealPath();
			if (!realComponent.startsWith(realRoot)) {
				throw new IOException("component resolves outside the temporary bundle");
			}
			if (!sha256(component, expectedSize).equals(expectedDigest)) {
				throw new IOException("component identity mismatch");
			}
		}
		return manifest;
	}

	private Path readyPath(String sessionId, long sequence) {
		validateSessionAndSequence(sessionId, sequence);
		return spoolDirectory.resolve(
				"ready-" + sessionId + "-" + String.format(Locale.ROOT, "%020d", sequence));
	}

	private Path uniqueQuarantinePath() {
		return spoolDirectory.resolve("quarantine-" + UUID.randomUUID());
	}

	private static void validateSessionAndSequence(String sessionId, long sequence) {
		if (!UUID.fromString(sessionId).toString().equals(sessionId) || sequence < 0) {
			throw new IllegalArgumentException("invalid capture session or sequence");
		}
	}

	private static Path resolveComponentPath(Path root, String relativePath) throws IOException {
		if (relativePath.isBlank()
				|| relativePath.indexOf('\\') >= 0
				|| relativePath.indexOf(':') >= 0
				|| relativePath.startsWith("/")) {
			throw new IOException("invalid component path");
		}
		for (String segment : relativePath.split("/", -1)) {
			if (segment.isEmpty() || segment.equals(".") || segment.equals("..")) {
				throw new IOException("component path is not normalized");
			}
		}
		Path relative = Path.of(relativePath);
		if (relative.isAbsolute() || !relative.normalize().equals(relative)) {
			throw new IOException("component path escapes the bundle");
		}
		Path resolved = root.resolve(relative).normalize();
		if (!resolved.startsWith(root.normalize()) || resolved.equals(root.resolve("bundle.json"))) {
			throw new IOException("component path escapes the bundle");
		}
		return resolved;
	}

	private static String requiredString(JsonObject object, String name) throws IOException {
		JsonElement value = object.get(name);
		if (value == null || !value.isJsonPrimitive() || !value.getAsJsonPrimitive().isString()) {
			throw new IOException(name + " is absent or not a string");
		}
		return value.getAsString();
	}

	private static String requiredNonBlankString(JsonObject object, String name) throws IOException {
		String value = requiredString(object, name);
		if (value.isBlank()) {
			throw new IOException(name + " is blank");
		}
		return value;
	}

	private static JsonObject requiredObject(JsonObject object, String name) throws IOException {
		return requiredObject(object.get(name), name);
	}

	private static JsonObject requiredObject(JsonElement element, String label) throws IOException {
		if (element == null || !element.isJsonObject()) {
			throw new IOException(label + " is absent or not an object");
		}
		return element.getAsJsonObject();
	}

	private static void requireOnlyFields(JsonObject object, Set<String> allowed, String label) throws IOException {
		for (String name : object.keySet()) {
			if (!allowed.contains(name)) {
				throw new IOException(label + " contains unknown field " + name);
			}
		}
	}

	private static int requiredInt(JsonObject object, String name) throws IOException {
		long value = requiredNonNegativeOrSignedLong(object, name);
		if (value < Integer.MIN_VALUE || value > Integer.MAX_VALUE) {
			throw new IOException(name + " does not fit a signed 32-bit integer");
		}
		return (int) value;
	}

	private static long requiredNonNegativeLong(JsonObject object, String name) throws IOException {
		long value = requiredNonNegativeOrSignedLong(object, name);
		if (value < 0) {
			throw new IOException(name + " is negative");
		}
		return value;
	}

	private static long requiredNonNegativeOrSignedLong(JsonObject object, String name) throws IOException {
		JsonElement element = object.get(name);
		if (element == null
				|| !element.isJsonPrimitive()
				|| !element.getAsJsonPrimitive().isNumber()) {
			throw new IOException(name + " is absent or not an integer");
		}
		String raw = element.getAsString();
		if (!isSignedDecimalInteger(raw)) {
			throw new IOException(name + " is not an integer");
		}
		try {
			return Long.parseLong(raw);
		} catch (NumberFormatException exception) {
			throw new IOException(name + " does not fit a signed 64-bit integer", exception);
		}
	}

	private static boolean isSignedDecimalInteger(String value) {
		int start = value.startsWith("-") ? 1 : 0;
		if (start == value.length()) {
			return false;
		}
		if (value.length() - start > 1 && value.charAt(start) == '0') {
			return false;
		}
		for (int index = start; index < value.length(); index++) {
			if (value.charAt(index) < '0' || value.charAt(index) > '9') {
				return false;
			}
		}
		return true;
	}

	private <T> T withSpoolLock(IoOperation<T> operation) throws IOException {
		JVM_SPOOL_LOCK.lock();
		try {
			Files.createDirectories(spoolDirectory);
			Path lockPath = spoolDirectory.resolve(WRITER_LOCK_FILE);
			try (FileChannel lockChannel = FileChannel.open(
						lockPath, StandardOpenOption.CREATE, StandardOpenOption.WRITE);
					FileLock fileLock = lockChannel.lock()) {
				if (!fileLock.isValid()) {
					throw new IOException("spool writer lock is invalid");
				}
				return operation.run();
			}
		} finally {
			JVM_SPOOL_LOCK.unlock();
		}
	}

	private static byte[] readLimited(Path path, int maximum) throws IOException {
		try (InputStream input = Files.newInputStream(
				path, StandardOpenOption.READ, LinkOption.NOFOLLOW_LINKS)) {
			byte[] data = input.readNBytes(maximum + 1);
			if (data.length > maximum) {
				throw new IOException("file exceeds " + maximum + " bytes");
			}
			return data;
		}
	}

	private static String decodeUtf8(byte[] data) throws IOException {
		try {
			return StandardCharsets.UTF_8.newDecoder()
					.onMalformedInput(CodingErrorAction.REPORT)
					.onUnmappableCharacter(CodingErrorAction.REPORT)
					.decode(ByteBuffer.wrap(data))
					.toString();
		} catch (CharacterCodingException exception) {
			throw new IOException("bundle.json is not valid UTF-8", exception);
		}
	}

	private static void validateJsonStructure(String json) throws IOException {
		try (JsonReader reader = new JsonReader(new StringReader(json))) {
			reader.setStrictness(Strictness.STRICT);
			consumeJsonValue(reader, 0);
			if (reader.peek() != JsonToken.END_DOCUMENT) {
				throw new IOException("bundle.json has trailing content");
			}
		}
	}

	private static void consumeJsonValue(JsonReader reader, int depth) throws IOException {
		if (depth > 64) {
			throw new IOException("JSON nesting exceeds 64");
		}
		switch (reader.peek()) {
			case BEGIN_OBJECT -> {
				reader.beginObject();
				Set<String> names = new HashSet<>();
				while (reader.hasNext()) {
					String name = reader.nextName();
					if (!names.add(name)) {
						throw new IOException("duplicate JSON field " + name);
					}
					consumeJsonValue(reader, depth + 1);
				}
				reader.endObject();
			}
			case BEGIN_ARRAY -> {
				reader.beginArray();
				while (reader.hasNext()) {
					consumeJsonValue(reader, depth + 1);
				}
				reader.endArray();
			}
			case STRING -> reader.nextString();
			case NUMBER -> reader.nextString();
			case BOOLEAN -> reader.nextBoolean();
			case NULL -> reader.nextNull();
			default -> throw new IOException("unexpected JSON token " + reader.peek());
		}
	}

	private static boolean validLowerSha256(String value) {
		if (value.length() != 64) {
			return false;
		}
		for (int index = 0; index < value.length(); index++) {
			char character = value.charAt(index);
			if (!((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f'))) {
				return false;
			}
		}
		return true;
	}

	private static String sha256(Path path, long expectedSize) throws IOException {
		MessageDigest digest = newSha256();
		Set<OpenOption> options = Set.of(StandardOpenOption.READ, LinkOption.NOFOLLOW_LINKS);
		long size = 0;
		try (SeekableByteChannel channel = Files.newByteChannel(path, options)) {
			ByteBuffer buffer = ByteBuffer.allocate(8192);
			while (channel.read(buffer) >= 0) {
				buffer.flip();
				size += buffer.remaining();
				if (size > expectedSize) {
					throw new IOException("component size exceeds its descriptor");
				}
				digest.update(buffer);
				buffer.clear();
			}
		}
		if (size != expectedSize) {
			throw new IOException("component size differs from its descriptor");
		}
		return HexFormat.of().formatHex(digest.digest());
	}

	private static void writeSynced(Path path, byte[] data) throws IOException {
		try (FileChannel channel = FileChannel.open(
				path, StandardOpenOption.CREATE_NEW, StandardOpenOption.WRITE)) {
			ByteBuffer buffer = ByteBuffer.wrap(data);
			while (buffer.hasRemaining()) {
				channel.write(buffer);
			}
			channel.force(true);
		}
	}

	private static void moveWithoutReplace(Path source, Path target) throws IOException {
		if (Files.exists(target, LinkOption.NOFOLLOW_LINKS)) {
			throw new FileAlreadyExistsException(target.toString());
		}
		try {
			Files.move(source, target, StandardCopyOption.ATOMIC_MOVE);
		} catch (AtomicMoveNotSupportedException exception) {
			Files.move(source, target);
		}
	}

	private static void syncDirectory(Path directory) throws IOException {
		if (System.getProperty("os.name", "").startsWith("Windows")) {
			return;
		}
		try (FileChannel channel = FileChannel.open(directory, StandardOpenOption.READ)) {
			channel.force(true);
		}
	}

	private static void syncDirectoryBestEffort(Path directory) {
		try {
			syncDirectory(directory);
		} catch (IOException ignored) {
			// The complete temporary bundle remains recoverable by its manifest.
		}
	}

	private static String sha256(byte[] data) {
		return HexFormat.of().formatHex(newSha256().digest(data));
	}

	private static MessageDigest newSha256() {
		try {
			return MessageDigest.getInstance("SHA-256");
		} catch (NoSuchAlgorithmException exception) {
			throw new IllegalStateException("SHA-256 is unavailable", exception);
		}
	}
}
