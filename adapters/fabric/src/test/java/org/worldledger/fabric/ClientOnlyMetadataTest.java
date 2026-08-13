package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.Reader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Test;

final class ClientOnlyMetadataTest {
	@Test
	void fabricMetadataCannotLoadOnServer() throws Exception {
		Path metadataPath = Path.of("src", "main", "resources", "fabric.mod.json");
		JsonObject metadata;
		try (Reader reader = Files.newBufferedReader(metadataPath, StandardCharsets.UTF_8)) {
			metadata = JsonParser.parseReader(reader).getAsJsonObject();
		}
		assertEquals("client", metadata.get("environment").getAsString());
		JsonObject entrypoints = metadata.getAsJsonObject("entrypoints");
		assertTrue(entrypoints.has("client"));
		assertFalse(entrypoints.has("main"));
		assertFalse(entrypoints.has("server"));
		assertEquals(1, metadata.getAsJsonArray("mixins").size());
	}
}
