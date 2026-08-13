package org.worldledger.fabric.canonical;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.IOException;
import java.io.Reader;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import org.junit.jupiter.api.Test;

final class CanonicalGoldenTest {
	private record Identity(String file, int size, String sha256) {}

	private static final Map<String, Identity> IDENTITIES = Map.ofEntries(
			Map.entry(
					"biomes-mixed-negative",
					new Identity(
							"biomes-mixed-negative.bin",
							263,
							"a7bcadfdbd655e031ddbf243cecc92c35a6bc073d7c1b58f0b8d4b4395c2dcc4")),
			Map.entry(
					"block-entities-empty",
					new Identity(
							"block-entities-empty.bin",
							52,
							"8966ca92974146f8d58c2c3337de38c3b8f38fdda4c6d45595c37311b8727a77")),
			Map.entry(
					"block-entities-nbt-special",
					new Identity(
							"block-entities-nbt-special.bin",
							511,
							"f876a04a4cee18d17353c6bda41e37033c0dc3d072a9e9ba7f57235653d760a9")),
			Map.entry(
					"blocks-all-air-negative",
					new Identity(
							"blocks-all-air-negative.bin",
							8262,
							"ed0cd3cbaa8a1165700c575cfaffe8cf3f58bee5602a3ba5ab3c0db78ea7f49f")),
			Map.entry(
					"blocks-high-palette",
					new Identity(
							"blocks-high-palette.bin",
							98357,
							"6c7392f64d1ad9a6db478dc57123ff7c03589af6ab612078093ea00f1ea85af7")),
			Map.entry(
					"blocks-property-order",
					new Identity(
							"blocks-property-order.bin",
							8328,
							"9a3dfa6ceea8ddc986f6e9e3381cf6db217365f52cf5f6822966b4716768d8f5")),
			Map.entry(
					"shape-negative",
					new Identity(
							"shape-negative.bin",
							53,
							"a2fbcf1cd1560c698239c9a93aba9aa24995954ed7f00acf3bbb92528337a5db")));

	@Test
	void javaEncoderMatchesEveryCommittedFixture() throws IOException, NoSuchAlgorithmException {
		Path fixtureDirectory = Path.of(System.getProperty("worldledger.fixtureDir"));
		JsonObject root;
		try (Reader reader = Files.newBufferedReader(fixtureDirectory.resolve("fixtures.json"))) {
			root = JsonParser.parseReader(reader).getAsJsonObject();
		}
		assertEquals("worldledger.minecraft.java.chunk-fixtures/v1", root.get("schema").getAsString());
		JsonArray fixtures = root.getAsJsonArray("fixtures");
		assertEquals(IDENTITIES.size(), fixtures.size());

		for (JsonElement element : fixtures) {
			JsonObject fixture = element.getAsJsonObject();
			String name = fixture.get("name").getAsString();
			Identity identity = IDENTITIES.get(name);
			if (identity == null) {
				throw new AssertionError("fixture has no hard-coded Java identity: " + name);
			}
			assertEquals(identity.file(), fixture.get("output").getAsString(), name);
			byte[] first = buildFixture(fixture);
			byte[] second = buildFixture(fixture);
			assertArrayEquals(first, second, name + " was not deterministic");
			byte[] committed = Files.readAllBytes(fixtureDirectory.resolve(identity.file()));
			assertArrayEquals(committed, first, name + " differs from the Go reference bytes");
			assertEquals(identity.size(), committed.length, name);
			assertEquals(identity.sha256(), HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(committed)), name);
		}
	}

	@Test
	void canonicalStateAndBoundsAreEnforced() {
		BlockStateValue stairs = new BlockStateValue(
				"minecraft:oak_stairs",
				List.of(
						new StateProperty("waterlogged", "false"),
						new StateProperty("shape", "straight"),
						new StateProperty("half", "bottom"),
						new StateProperty("facing", "north")));
		assertEquals(
				"minecraft:oak_stairs[facing=north,half=bottom,shape=straight,waterlogged=false]",
				MinecraftJavaV1.canonicalBlockState(stairs));
		assertThrows(
				IllegalArgumentException.class,
				() -> MinecraftJavaV1.canonicalBlockState(
						new BlockStateValue("minecraft:stone", List.of(new StateProperty("bad\u00a0name", "x")))));
		assertThrows(IllegalArgumentException.class, () -> MinecraftJavaV1.encodeShape(0, 0));
		assertThrows(
				IllegalArgumentException.class,
				() -> MinecraftJavaV1.encodeNbt(new NbtValue.StringTag("\ud800")));
		NbtValue.CompoundTag empty = new NbtValue.CompoundTag(List.of());
		BlockEntityValue duplicate = new BlockEntityValue(1, 2, 3, "minecraft:sign", empty);
		assertThrows(
				IllegalArgumentException.class,
				() -> MinecraftJavaV1.encodeBlockEntities(List.of(duplicate, duplicate)));
	}

	private static byte[] buildFixture(JsonObject fixture) {
		return switch (fixture.get("component").getAsString()) {
			case "shape" -> {
				JsonObject shape = fixture.getAsJsonObject("shape");
				yield MinecraftJavaV1.encodeShape(
						shape.get("min_section_y").getAsInt(), shape.get("section_count").getAsLong());
			}
			case "block_section" -> {
				JsonObject section = fixture.getAsJsonObject("block_section");
				yield MinecraftJavaV1.encodeBlockSection(
						section.get("section_y").getAsInt(), buildStates(section.getAsJsonObject("states")));
			}
			case "biome_section" -> {
				JsonObject section = fixture.getAsJsonObject("biome_section");
				yield MinecraftJavaV1.encodeBiomeSection(
						section.get("section_y").getAsInt(), buildResources(section.getAsJsonObject("biomes")));
			}
			case "block_entities" -> MinecraftJavaV1.encodeBlockEntities(
					buildBlockEntities(fixture.getAsJsonObject("block_entities")));
			default -> throw new IllegalArgumentException("unknown fixture component");
		};
	}

	private static List<BlockStateValue> buildStates(JsonObject spec) {
		return switch (spec.get("kind").getAsString()) {
			case "constant" -> {
				BlockStateValue state = parseState(spec.getAsJsonObject("state"));
				yield java.util.Collections.nCopies(MinecraftJavaV1.BLOCK_COUNT, state);
			}
			case "indexed_resources" -> {
				String namespace = spec.get("namespace").getAsString();
				String prefix = spec.get("path_prefix").getAsString();
				int width = spec.get("width").getAsInt();
				assertEquals(MinecraftJavaV1.BLOCK_COUNT, spec.get("count").getAsInt());
				List<BlockStateValue> states = new ArrayList<>(MinecraftJavaV1.BLOCK_COUNT);
				for (int index = 0; index < MinecraftJavaV1.BLOCK_COUNT; index++) {
					states.add(BlockStateValue.simple(
							namespace + ":" + prefix + String.format(Locale.ROOT, "%0" + width + "d", index)));
				}
				yield states;
			}
			default -> throw new IllegalArgumentException("unknown state generator");
		};
	}

	private static BlockStateValue parseState(JsonObject spec) {
		List<StateProperty> properties = new ArrayList<>();
		for (JsonElement element : arrayOrEmpty(spec, "properties")) {
			JsonObject property = element.getAsJsonObject();
			properties.add(new StateProperty(property.get("name").getAsString(), property.get("value").getAsString()));
		}
		return new BlockStateValue(spec.get("name").getAsString(), properties);
	}

	private static List<String> buildResources(JsonObject spec) {
		assertEquals("cycle", spec.get("kind").getAsString());
		JsonArray cycle = spec.getAsJsonArray("values");
		List<String> result = new ArrayList<>(MinecraftJavaV1.BIOME_COUNT);
		for (int index = 0; index < MinecraftJavaV1.BIOME_COUNT; index++) {
			result.add(cycle.get(index % cycle.size()).getAsString());
		}
		return result;
	}

	private static List<BlockEntityValue> buildBlockEntities(JsonObject spec) {
		List<BlockEntityValue> result = new ArrayList<>();
		for (JsonElement element : spec.getAsJsonArray("entries")) {
			JsonObject entry = element.getAsJsonObject();
			NbtValue nbt = parseNbt(entry.getAsJsonObject("nbt"));
			if (!(nbt instanceof NbtValue.CompoundTag compound)) {
				throw new IllegalArgumentException("fixture block entity root is not a compound");
			}
			result.add(new BlockEntityValue(
					entry.get("local_x").getAsInt(),
					entry.get("block_y").getAsInt(),
					entry.get("local_z").getAsInt(),
					entry.get("type").getAsString(),
					compound));
		}
		return result;
	}

	private static NbtValue parseNbt(JsonObject spec) {
		return switch (spec.get("type").getAsString()) {
			case "byte" -> new NbtValue.ByteTag(spec.get("byte").getAsByte());
			case "short" -> new NbtValue.ShortTag(spec.get("short").getAsShort());
			case "int" -> new NbtValue.IntTag(spec.get("int").getAsInt());
			case "long" -> new NbtValue.LongTag(Long.parseLong(spec.get("long").getAsString()));
			case "float_bits" -> new NbtValue.FloatTag(
					(int) Long.parseUnsignedLong(stripHexPrefix(spec.get("float_bits").getAsString()), 16));
			case "double_bits" -> new NbtValue.DoubleTag(
					Long.parseUnsignedLong(stripHexPrefix(spec.get("double_bits").getAsString()), 16));
			case "byte_array" -> new NbtValue.ByteArrayTag(HexFormat.of().parseHex(spec.get("bytes_hex").getAsString()));
			case "string" -> new NbtValue.StringTag(spec.get("string").getAsString());
			case "list" -> {
				byte elementType = parseTagType(spec.get("element_type").getAsString());
				List<NbtValue> values = new ArrayList<>();
				for (JsonElement element : arrayOrEmpty(spec, "values")) {
					values.add(parseNbt(element.getAsJsonObject()));
				}
				yield new NbtValue.ListTag(elementType, values);
			}
			case "compound" -> {
				List<NbtValue.Entry> entries = new ArrayList<>();
				for (JsonElement element : arrayOrEmpty(spec, "entries")) {
					JsonObject entry = element.getAsJsonObject();
					entries.add(new NbtValue.Entry(
							entry.get("name").getAsString(), parseNbt(entry.getAsJsonObject("value"))));
				}
				yield new NbtValue.CompoundTag(entries);
			}
			case "int_array" -> {
				JsonArray values = arrayOrEmpty(spec, "ints");
				int[] result = new int[values.size()];
				for (int index = 0; index < values.size(); index++) {
					result[index] = values.get(index).getAsInt();
				}
				yield new NbtValue.IntArrayTag(result);
			}
			case "long_array" -> {
				JsonArray values = arrayOrEmpty(spec, "longs");
				long[] result = new long[values.size()];
				for (int index = 0; index < values.size(); index++) {
					result[index] = Long.parseLong(values.get(index).getAsString());
				}
				yield new NbtValue.LongArrayTag(result);
			}
			default -> throw new IllegalArgumentException("unknown fixture NBT type");
		};
	}

	private static byte parseTagType(String value) {
		return switch (value) {
			case "end" -> NbtValue.END;
			case "byte" -> NbtValue.BYTE;
			case "short" -> NbtValue.SHORT;
			case "int" -> NbtValue.INT;
			case "long" -> NbtValue.LONG;
			case "float_bits" -> NbtValue.FLOAT;
			case "double_bits" -> NbtValue.DOUBLE;
			case "byte_array" -> NbtValue.BYTE_ARRAY;
			case "string" -> NbtValue.STRING;
			case "list" -> NbtValue.LIST;
			case "compound" -> NbtValue.COMPOUND;
			case "int_array" -> NbtValue.INT_ARRAY;
			case "long_array" -> NbtValue.LONG_ARRAY;
			default -> throw new IllegalArgumentException("unknown fixture NBT tag");
		};
	}

	private static JsonArray arrayOrEmpty(JsonObject object, String name) {
		JsonArray value = object.getAsJsonArray(name);
		return value == null ? new JsonArray() : value;
	}

	private static String stripHexPrefix(String value) {
		if (!value.startsWith("0x")) {
			throw new IllegalArgumentException("bit pattern lacks 0x prefix");
		}
		return value.substring(2);
	}
}
