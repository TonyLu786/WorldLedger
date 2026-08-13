package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.Reader;
import java.nio.charset.StandardCharsets;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.List;
import java.util.Comparator;
import java.util.Optional;
import java.util.concurrent.Executors;
import org.worldledger.fabric.canonical.BlockEntityValue;
import org.worldledger.fabric.canonical.NbtValue;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

final class BundleSpoolWriterTest {
	@TempDir
	Path temporaryDirectory;

	@Test
	void committedEndToEndBundleMatchesCurrentProducerBytes() throws Exception {
		Path actualSpool = temporaryDirectory.resolve("actual-spool");
		Path actualReady = new BundleSpoolWriter(actualSpool).write(CaptureTestFixtures.job(417));
		Path committedSpool = Path.of(System.getProperty("worldledger.e2eFixtureDir"));
		Path committedReady;
		try (var entries = Files.list(committedSpool)) {
			List<Path> readyDirectories = entries
					.filter(path -> path.getFileName().toString().startsWith("ready-"))
					.toList();
			assertEquals(1, readyDirectories.size());
			committedReady = readyDirectories.getFirst();
		}
		assertEquals(committedReady.getFileName().toString(), actualReady.getFileName().toString());
		List<Path> committedFiles;
		try (var files = Files.walk(committedReady)) {
			committedFiles = files.filter(Files::isRegularFile)
					.sorted(Comparator.comparing(path -> committedReady.relativize(path).toString()))
					.toList();
		}
		List<Path> actualFiles;
		try (var files = Files.walk(actualReady)) {
			actualFiles = files.filter(Files::isRegularFile)
					.sorted(Comparator.comparing(path -> actualReady.relativize(path).toString()))
					.toList();
		}
		assertEquals(
				committedFiles.stream().map(path -> committedReady.relativize(path).toString()).toList(),
				actualFiles.stream().map(path -> actualReady.relativize(path).toString()).toList());
		for (int index = 0; index < committedFiles.size(); index++) {
			assertArrayEquals(
					Files.readAllBytes(committedFiles.get(index)),
					Files.readAllBytes(actualFiles.get(index)),
					committedReady.relativize(committedFiles.get(index)).toString());
		}
	}

	@Test
	void readyBundleContainsVerifiedMultiComponentManifest() throws Exception {
		BundleSpoolWriter writer = new BundleSpoolWriter(temporaryDirectory.resolve("spool"));
		Path ready = writer.write(CaptureTestFixtures.job(7));
		assertTrue(Files.isDirectory(ready));
		assertFalse(ready.getFileName().toString().startsWith(".tmp-"));
		JsonObject manifest;
		try (Reader reader = Files.newBufferedReader(ready.resolve("bundle.json"), StandardCharsets.UTF_8)) {
			manifest = JsonParser.parseReader(reader).getAsJsonObject();
		}
		assertEquals(BundleSpoolWriter.SCHEMA, manifest.get("schema").getAsString());
		assertEquals(7, manifest.getAsJsonObject("capture").get("sequence").getAsLong());
		assertEquals(4, manifest.getAsJsonObject("components").size());
		for (var entry : manifest.getAsJsonObject("components").entrySet()) {
			String componentPath = entry.getValue().getAsJsonObject().get("path").getAsString();
			assertTrue(Files.isRegularFile(ready.resolve(componentPath)));
		}
	}

	@Test
	void startupRecoversCompleteTemporaryBundleAndQuarantinesIncompleteOne() throws Exception {
		Path spool = temporaryDirectory.resolve("spool");
		BundleSpoolWriter writer = new BundleSpoolWriter(spool);
		Path ready = writer.write(CaptureTestFixtures.job(9));
		Path completedTemporary = spool.resolve(".tmp-complete");
		Files.move(ready, completedTemporary, StandardCopyOption.ATOMIC_MOVE);
		Path incomplete = Files.createDirectory(spool.resolve(".tmp-incomplete"));
		Files.writeString(incomplete.resolve("partial.bin"), "partial", StandardCharsets.UTF_8);

		BundleSpoolWriter.RecoveryReport report = writer.recoverTemporaryBundles();
		assertEquals(1, report.recovered());
		assertEquals(1, report.quarantined());
		assertEquals(2, report.diagnostics().size());
		try (var entries = Files.list(spool)) {
			List<String> names = entries.map(path -> path.getFileName().toString()).toList();
			assertTrue(names.stream().anyMatch(name -> name.startsWith("ready-")));
			assertTrue(names.stream().anyMatch(name -> name.startsWith("quarantine-")));
			assertTrue(names.stream().noneMatch(name -> name.startsWith(".tmp-")));
		}
	}

	@Test
	void readyBundleIsNeverOverwritten() throws Exception {
		Path spool = temporaryDirectory.resolve("spool");
		BundleSpoolWriter writer = new BundleSpoolWriter(spool);
		Path ready = writer.write(CaptureTestFixtures.job(11));
		byte[] originalManifest = Files.readAllBytes(ready.resolve("bundle.json"));

		assertThrows(FileAlreadyExistsException.class, () -> writer.write(CaptureTestFixtures.job(11)));
		assertArrayEquals(originalManifest, Files.readAllBytes(ready.resolve("bundle.json")));
	}

	@Test
	void separateWriterInstancesSerializeThroughTheSpoolLock() throws Exception {
		Path spool = temporaryDirectory.resolve("spool");
		try (var executor = Executors.newFixedThreadPool(2)) {
			var first = executor.submit(() -> new BundleSpoolWriter(spool).write(CaptureTestFixtures.job(12)));
			var second = executor.submit(() -> new BundleSpoolWriter(spool).write(CaptureTestFixtures.job(13)));
			assertTrue(Files.isDirectory(first.get()));
			assertTrue(Files.isDirectory(second.get()));
		}
	}

	@Test
	void recoveryQuarantinesDuplicateManifestFields() throws Exception {
		Path spool = temporaryDirectory.resolve("spool");
		BundleSpoolWriter writer = new BundleSpoolWriter(spool);
		Path ready = writer.write(CaptureTestFixtures.job(14));
		Path temporary = spool.resolve(".tmp-duplicate-json");
		Files.move(ready, temporary, StandardCopyOption.ATOMIC_MOVE);
		Path manifestPath = temporary.resolve("bundle.json");
		String manifest = Files.readString(manifestPath, StandardCharsets.UTF_8);
		String duplicate = manifest.replaceFirst(
				"\\{", "{\n  \"schema\": \"worldledger.capture-bundle/v1\",");
		Files.writeString(manifestPath, duplicate, StandardCharsets.UTF_8);

		BundleSpoolWriter.RecoveryReport report = writer.recoverTemporaryBundles();
		assertEquals(0, report.recovered());
		assertEquals(1, report.quarantined());
		assertTrue(report.diagnostics().getFirst().contains("duplicate JSON field schema"));
	}

	@Test
	void invalidOptionalComponentIsOmittedWithoutLosingTerrain() throws Exception {
		CaptureJob base = CaptureTestFixtures.job(15);
		BlockEntityValue invalid = new BlockEntityValue(
				0, 64, 0, "INVALID", new NbtValue.CompoundTag(List.of()));
		CaptureJob job = new CaptureJob(
				base.serverId(),
				base.serverAddress(),
				base.dimension(),
				base.chunk(),
				base.observedAt(),
				base.contributor(),
				base.sessionId(),
				base.sequence(),
				base.trigger(),
				base.minSectionY(),
				base.sectionCount(),
				base.sections(),
				Optional.of(List.of(invalid)));

		Path ready = new BundleSpoolWriter(temporaryDirectory.resolve("spool")).write(job);
		JsonObject manifest;
		try (Reader reader = Files.newBufferedReader(ready.resolve("bundle.json"), StandardCharsets.UTF_8)) {
			manifest = JsonParser.parseReader(reader).getAsJsonObject();
		}
		JsonObject components = manifest.getAsJsonObject("components");
		assertFalse(components.has("mcjava.block_entities"));
		assertTrue(components.has("mcjava.shape"));
		assertTrue(components.has("mcjava.blocks.-4"));
	}
}
