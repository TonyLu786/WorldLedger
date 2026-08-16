package org.worldledger.fabric.gametest;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.IOException;
import java.io.Reader;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Properties;
import java.util.Set;
import java.util.stream.Stream;

import com.mojang.brigadier.ParseResults;
import net.fabricmc.fabric.api.client.command.v2.ClientCommands;
import net.fabricmc.fabric.api.client.command.v2.FabricClientCommandSource;
import net.fabricmc.fabric.api.client.gametest.v1.FabricClientGameTest;
import net.fabricmc.fabric.api.client.gametest.v1.context.ClientGameTestContext;
import net.fabricmc.fabric.api.client.gametest.v1.context.TestDedicatedServerConnection;
import net.fabricmc.fabric.api.client.gametest.v1.context.TestDedicatedServerContext;
import net.fabricmc.loader.api.FabricLoader;
import net.minecraft.client.Minecraft;
import org.worldledger.fabric.CapturePaths;
import org.worldledger.fabric.CaptureTiming;

/**
 * Drives a real client through a real multiplayer session and checks what the
 * adapter wrote.
 *
 * <p>This is the capture direction, which unit tests cannot reach: they can
 * prove that a semantic snapshot becomes correct bytes, but not that the client
 * hooks fire in the right order against a live server. The milestone this
 * belongs to is described in {@code docs/milestone-java-capture.md}.
 *
 * <p>The test fails when nothing was captured. Capture is disabled while the
 * contributor is blank, so a run with no configuration would otherwise pass
 * while proving nothing.
 */
public final class CaptureClientGameTest implements FabricClientGameTest {
	/** Long enough for the adapter's quiet window and its maximum latency bound. */
	private static final int SETTLE_TICKS = 200;
	/** The spool is written off the client thread, so publication lags the tick. */
	private static final int SPOOL_TIMEOUT_TICKS = 600;

	private static final String BUNDLE_SCHEMA = "worldledger.capture-bundle/v1";
	private static final String SHAPE_COMPONENT = "mcjava.shape";
	private static final String BLOCKS_PREFIX = "mcjava.blocks.";

	/**
	 * Server settings that make the captured world the same world on every run
	 * and on every platform.
	 *
	 * <p>The point of this test is to compare what two machines canonicalize from
	 * the same observed state. A default server generates terrain from a random
	 * seed, so two runs would differ in their content before any encoder was
	 * involved and a digest comparison between them would mean nothing. A
	 * superflat world with a fixed seed and no structures removes generated
	 * variation entirely, leaving only what the fixture commands place.
	 *
	 * <p>The view distance is pinned because it decides how many chunks the
	 * client loads, and therefore which chunks the adapter captures. The default
	 * is a client and server negotiation whose result is not guaranteed to match
	 * across machines.
	 */
	private static Properties deterministicServerProperties() {
		Properties properties = new Properties();
		properties.setProperty("level-seed", "worldledger-capture-fixture");
		properties.setProperty("level-type", "minecraft:flat");
		properties.setProperty("generate-structures", "false");
		properties.setProperty("view-distance", "5");
		properties.setProperty("simulation-distance", "5");
		properties.setProperty("spawn-monsters", "false");
		properties.setProperty("spawn-npcs", "false");
		properties.setProperty("spawn-animals", "false");
		return properties;
	}

	@Override
	public void runTest(ClientGameTestContext context) {
		CapturePaths paths = CapturePaths.fromFabricConfigDirectory(FabricLoader.getInstance().getConfigDir());
		requireCaptureConfigured(paths.configDirectory());

		Set<Path> before = readySpoolEntries(paths.spoolDirectory());

		try (TestDedicatedServerContext server = context.worldBuilder().createServer(deterministicServerProperties())) {
			try (TestDedicatedServerConnection connection = server.connect()) {
				connection.waitForChunksDownload();
				verifyClientCommands(context);
				placeFixture(server);
				connection.waitForClientboundPackets();
				// Let the dirty-chunk coalescing window close before disconnecting.
				context.waitTicks(SETTLE_TICKS);
			}
			// Leaving the connection triggers the adapter's final flush.
		}

		List<Path> published = awaitNewReadyEntries(context, paths.spoolDirectory(), before);
		awaitSpoolSettled(context, paths.spoolDirectory());
		verifyNoFailedEntries(paths.spoolDirectory());
		verifyBundles(readySpoolEntries(paths.spoolDirectory()), before);
		if (published.isEmpty()) {
			throw new AssertionError("no bundle was published");
		}
		reportClientThreadCost();
	}

	/**
	 * Checks that /worldledger is registered and that running it does something.
	 *
	 * <p>The command is the only way a player can ask whether capture is working,
	 * and until this ran nothing had ever executed it: the classes compiled, the
	 * strings had unit tests, and registration threw no exception, none of which
	 * is the same as a command a player can type.
	 *
	 * <p>Both halves are checked because either can fail alone. A tree with the
	 * wrong shape parses nothing; a tree with the right shape can still have a
	 * handler that throws on its first line. Sending it the way a player does,
	 * through the client's own command path, is what proves it was claimed by the
	 * client rather than passed to a server that has never heard of it.
	 */
	private static void verifyClientCommands(ClientGameTestContext context) {
		for (String path : new String[] {"", "status", "spool", "reload"}) {
			String command = path.isEmpty() ? "worldledger" : "worldledger " + path;
			ParseResults<FabricClientCommandSource> parsed = context.computeOnClient(client ->
					ClientCommands.getActiveDispatcher().parse(command, clientCommandSource(client)));
			if (!parsed.getExceptions().isEmpty()) {
				throw new AssertionError("/" + command + " does not parse: " + parsed.getExceptions());
			}
			if (parsed.getContext().build(command).getCommand() == null) {
				throw new AssertionError("/" + command + " parses but runs nothing");
			}
		}

		// Through the client's own path, which is what a player's keystrokes reach.
		// A throw here fails the test; the feedback itself is asserted by the unit
		// tests over CaptureStatus, which is where the wording lives.
		context.runOnClient(client -> client.getConnection().sendCommand("worldledger status"));
		context.waitTicks(2);
	}

	private static FabricClientCommandSource clientCommandSource(Minecraft client) {
		// Minecraft's own client suggestion source is what Fabric hands to client
		// commands, and it implements the Fabric interface by mixin.
		return (FabricClientCommandSource) client.getConnection().getSuggestionsProvider();
	}

	/**
	 * Prints what capture cost the client thread, so every run leaves the number
	 * in the build log rather than leaving the question open.
	 *
	 * <p>The assertion is deliberately far above anything a healthy run
	 * approaches. A shared build machine can stall a thread for reasons that
	 * have nothing to do with this code, so a tight bound here would fail for
	 * the wrong reasons; what it catches is capture doing something structurally
	 * wrong on the thread that draws frames.
	 */
	private static void reportClientThreadCost() {
		CaptureTiming.Snapshot timing = CaptureTiming.lastSession();
		System.out.println("[worldledger] client-thread capture cost: " + timing.describe());
		if (!timing.measured()) {
			throw new AssertionError("the session captured chunks but recorded no client-thread work");
		}
		if (timing.maxMicroseconds() > 500_000.0) {
			throw new AssertionError(
					"one tick spent " + timing.maxMicroseconds()
							+ " us in capture, which is ten whole ticks; something is being done on the "
							+ "client thread that should not be");
		}
	}

	/**
	 * Waits for the writer to finish whatever it is mid-way through.
	 *
	 * <p>A {@code .tmp-} directory is the crash-safe staging area for a bundle
	 * being written, so its presence during a write is correct behaviour rather
	 * than a fault. Only one that outlives the writer indicates a problem, which
	 * is what the following check is for.
	 */
	private static void awaitSpoolSettled(ClientGameTestContext context, Path spool) {
		for (int tick = 0; tick < SPOOL_TIMEOUT_TICKS; tick++) {
			if (partialEntries(spool).isEmpty()) {
				return;
			}
			context.waitTick();
		}
	}

	private static List<String> partialEntries(Path spool) {
		if (!Files.isDirectory(spool)) {
			return List.of();
		}
		try (Stream<Path> entries = Files.list(spool)) {
			return entries.map(path -> path.getFileName().toString())
					.filter(name -> name.startsWith("quarantine-") || name.startsWith(".tmp-"))
					.toList();
		} catch (IOException exception) {
			throw new UncheckedIOException(exception);
		}
	}

	private static void placeFixture(TestDedicatedServerContext server) {
		server.runCommand("forceload add 0 0");
		server.runCommand("fill 0 64 0 15 64 15 minecraft:stone");
		server.runCommand("setblock 2 65 1 minecraft:oak_log[axis=x]");
		server.runCommand("setblock 4 65 1 minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=false]");
		server.runCommand("fillbiome 0 64 0 15 79 15 minecraft:desert");
	}

	private static void requireCaptureConfigured(Path configDirectory) {
		Path file = configDirectory.resolve("capture.properties");
		if (!Files.isRegularFile(file)) {
			throw new AssertionError("capture.properties is absent at " + file
					+ "; the run configuration must write it before the client starts, "
					+ "otherwise capture is disabled and this test would prove nothing");
		}
		Properties properties = new Properties();
		try (Reader reader = Files.newBufferedReader(file, StandardCharsets.UTF_8)) {
			properties.load(reader);
		} catch (IOException exception) {
			throw new UncheckedIOException(exception);
		}
		if (properties.getProperty("contributor", "").trim().isEmpty()) {
			throw new AssertionError("contributor is blank in " + file + "; capture is disabled");
		}
	}

	private static Set<Path> readySpoolEntries(Path spool) {
		if (!Files.isDirectory(spool)) {
			return Set.of();
		}
		try (Stream<Path> entries = Files.list(spool)) {
			Set<Path> out = new HashSet<>();
			entries.filter(Files::isDirectory)
					.filter(path -> path.getFileName().toString().startsWith("ready-"))
					.forEach(out::add);
			return out;
		} catch (IOException exception) {
			throw new UncheckedIOException(exception);
		}
	}

	private static List<Path> awaitNewReadyEntries(ClientGameTestContext context, Path spool, Set<Path> before) {
		for (int tick = 0; tick < SPOOL_TIMEOUT_TICKS; tick++) {
			List<Path> published = new ArrayList<>(readySpoolEntries(spool));
			published.removeAll(before);
			if (!published.isEmpty()) {
				return published;
			}
			context.waitTick();
		}
		throw new AssertionError("no ready capture bundle appeared in " + spool
				+ " after a multiplayer session that applied block and biome changes");
	}

	private static void verifyNoFailedEntries(Path spool) {
		List<String> failures = partialEntries(spool);
		if (!failures.isEmpty()) {
			throw new AssertionError(
					"the spool still held incomplete or quarantined entries after the writer drained: " + failures);
		}
	}

	private static void verifyBundles(Set<Path> all, Set<Path> before) {
		List<Path> published = new ArrayList<>(all);
		published.removeAll(before);
		boolean sawForcedChunk = false;

		for (Path bundle : published) {
			JsonObject manifest = readManifest(bundle.resolve("bundle.json"));
			expect(manifest, "schema", BUNDLE_SCHEMA);
			expect(manifest, "dimension", "minecraft:overworld");

			JsonObject chunk = manifest.getAsJsonObject("chunk");
			if (chunk == null) {
				throw new AssertionError(bundle + ": manifest declares no chunk");
			}
			if (chunk.get("x").getAsInt() == 0 && chunk.get("z").getAsInt() == 0) {
				sawForcedChunk = true;
			}

			JsonObject components = manifest.getAsJsonObject("components");
			if (components == null || components.isEmpty()) {
				throw new AssertionError(bundle + ": manifest declares no components");
			}

			boolean sawShape = false;
			boolean sawBlocks = false;
			for (String name : components.keySet()) {
				if (SHAPE_COMPONENT.equals(name)) {
					sawShape = true;
				} else if (name.startsWith(BLOCKS_PREFIX)) {
					sawBlocks = true;
				}
				verifyComponent(bundle, name, components.getAsJsonObject(name));
			}
			if (!sawShape) {
				throw new AssertionError(bundle + ": no " + SHAPE_COMPONENT + " component");
			}
			if (!sawBlocks) {
				throw new AssertionError(bundle + ": no " + BLOCKS_PREFIX + "* component");
			}
		}

		if (!sawForcedChunk) {
			throw new AssertionError("no bundle covered the force-loaded fixture chunk (0,0)");
		}
	}

	private static void verifyComponent(Path bundle, String name, JsonObject descriptor) {
		if (descriptor == null) {
			throw new AssertionError(bundle + ": component " + name + " has no descriptor");
		}
		String relative = descriptor.get("path").getAsString();
		if (relative.startsWith("/") || relative.contains("..") || relative.contains("\\")) {
			throw new AssertionError(bundle + ": component " + name + " has an unsafe path " + relative);
		}
		Path payload = bundle.resolve(relative);
		if (!Files.isRegularFile(payload)) {
			throw new AssertionError(bundle + ": component " + name + " is missing at " + payload);
		}

		byte[] bytes;
		try {
			bytes = Files.readAllBytes(payload);
		} catch (IOException exception) {
			throw new UncheckedIOException(exception);
		}
		long declaredSize = descriptor.get("size").getAsLong();
		if (declaredSize != bytes.length) {
			throw new AssertionError(bundle + ": component " + name + " is " + bytes.length
					+ " bytes but the manifest declares " + declaredSize);
		}
		String algorithm = descriptor.get("algorithm").getAsString();
		if (!"sha256".equals(algorithm)) {
			throw new AssertionError(bundle + ": component " + name + " uses algorithm " + algorithm);
		}
		String declaredDigest = descriptor.get("digest").getAsString();
		if (!sha256(bytes).equals(declaredDigest)) {
			throw new AssertionError(bundle + ": component " + name + " does not match its declared digest");
		}
	}

	private static JsonObject readManifest(Path path) {
		if (!Files.isRegularFile(path)) {
			throw new AssertionError("missing bundle manifest at " + path);
		}
		try (Reader reader = Files.newBufferedReader(path, StandardCharsets.UTF_8)) {
			JsonElement parsed = JsonParser.parseReader(reader);
			if (!parsed.isJsonObject()) {
				throw new AssertionError(path + ": manifest is not a JSON object");
			}
			return parsed.getAsJsonObject();
		} catch (IOException exception) {
			throw new UncheckedIOException(exception);
		}
	}

	private static void expect(JsonObject manifest, String field, String expected) {
		JsonElement value = manifest.get(field);
		if (value == null || !expected.equals(value.getAsString())) {
			throw new AssertionError("manifest " + field + " is " + value + "; expected " + expected);
		}
	}

	private static String sha256(byte[] data) {
		MessageDigest digest;
		try {
			digest = MessageDigest.getInstance("SHA-256");
		} catch (NoSuchAlgorithmException exception) {
			throw new IllegalStateException("SHA-256 is required", exception);
		}
		StringBuilder out = new StringBuilder(64);
		for (byte value : digest.digest(data)) {
			out.append(String.format(Locale.ROOT, "%02x", value));
		}
		return out.toString();
	}
}
