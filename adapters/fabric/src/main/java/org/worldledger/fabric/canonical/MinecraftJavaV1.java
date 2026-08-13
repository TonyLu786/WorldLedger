package org.worldledger.fabric.canonical;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.TreeSet;

public final class MinecraftJavaV1 {
	public static final int BLOCK_COUNT = 16 * 16 * 16;
	public static final int BIOME_COUNT = 4 * 4 * 4;

	private static final String SHAPE_DOMAIN = "worldledger.minecraft.java.chunk-shape/v1";
	private static final String BLOCK_SECTION_DOMAIN = "worldledger.minecraft.java.block-section/v1";
	private static final String BIOME_SECTION_DOMAIN = "worldledger.minecraft.java.biome-section/v1";
	private static final String BLOCK_ENTITIES_DOMAIN = "worldledger.minecraft.java.block-entities/v1";
	private static final Comparator<String> UTF8_ORDER = Utf8::compare;

	private MinecraftJavaV1() {}

	public static String canonicalBlockState(BlockStateValue state) {
		Objects.requireNonNull(state, "state");
		validateResourceLocation(state.name());
		if (state.properties().isEmpty()) {
			return state.name();
		}
		List<StateProperty> properties = new ArrayList<>(state.properties());
		properties.sort(Comparator.comparing(StateProperty::name, UTF8_ORDER));
		StringBuilder result = new StringBuilder(state.name()).append('[');
		String previousName = null;
		for (int index = 0; index < properties.size(); index++) {
			StateProperty property = properties.get(index);
			validateStateToken(property.name(), "property name");
			validateStateToken(property.value(), "property value");
			if (property.name().equals(previousName)) {
				throw new IllegalArgumentException("duplicate property " + property.name());
			}
			if (index > 0) {
				result.append(',');
			}
			result.append(property.name()).append('=').append(property.value());
			previousName = property.name();
		}
		return result.append(']').toString();
	}

	public static byte[] encodeShape(int minSectionY, long sectionCount) {
		if (sectionCount <= 0 || sectionCount > 0xffff_ffffL) {
			throw new IllegalArgumentException("section count must fit a non-zero u32");
		}
		long maxSectionY = (long) minSectionY + sectionCount - 1;
		if (maxSectionY > Integer.MAX_VALUE) {
			throw new IllegalArgumentException("section range exceeds i32");
		}
		CanonicalLimits limits = CanonicalLimits.defaults();
		CanonicalWriter writer = new CanonicalWriter(limits.maxComponentBytes(), limits.maxStringBytes());
		writer.writeString(SHAPE_DOMAIN);
		writer.writeI32(minSectionY);
		writer.writeU32(sectionCount);
		return writer.toByteArray();
	}

	public static byte[] encodeBlockSection(int sectionY, List<BlockStateValue> states) {
		return encodeBlockSection(sectionY, states, CanonicalLimits.defaults());
	}

	public static byte[] encodeBlockSection(
			int sectionY, List<BlockStateValue> states, CanonicalLimits limits) {
		Objects.requireNonNull(states, "states");
		Objects.requireNonNull(limits, "limits");
		if (states.size() != BLOCK_COUNT) {
			throw new IllegalArgumentException("block section must contain exactly " + BLOCK_COUNT + " states");
		}
		List<String> canonicalStates = new ArrayList<>(BLOCK_COUNT);
		Set<String> paletteSet = new TreeSet<>(UTF8_ORDER);
		for (BlockStateValue state : states) {
			String canonical = canonicalBlockState(state);
			if (Utf8.encode(canonical).length > limits.maxStringBytes()) {
				throw new IllegalArgumentException("block state exceeds string limit");
			}
			canonicalStates.add(canonical);
			paletteSet.add(canonical);
		}
		if (paletteSet.isEmpty() || paletteSet.size() > BLOCK_COUNT) {
			throw new IllegalArgumentException("invalid block palette size");
		}
		List<String> palette = List.copyOf(paletteSet);
		Map<String, Integer> paletteIndices = indexPalette(palette);
		CanonicalWriter writer = new CanonicalWriter(limits.maxComponentBytes(), limits.maxStringBytes());
		writer.writeString(BLOCK_SECTION_DOMAIN);
		writer.writeI32(sectionY);
		writer.writeU16(palette.size());
		for (String value : palette) {
			writer.writeString(value);
		}
		for (String value : canonicalStates) {
			writer.writeU16(paletteIndices.get(value));
		}
		return writer.toByteArray();
	}

	public static byte[] encodeBiomeSection(int sectionY, List<String> biomes) {
		return encodeBiomeSection(sectionY, biomes, CanonicalLimits.defaults());
	}

	public static byte[] encodeBiomeSection(int sectionY, List<String> biomes, CanonicalLimits limits) {
		Objects.requireNonNull(biomes, "biomes");
		Objects.requireNonNull(limits, "limits");
		if (biomes.size() != BIOME_COUNT) {
			throw new IllegalArgumentException("biome section must contain exactly " + BIOME_COUNT + " samples");
		}
		Set<String> paletteSet = new TreeSet<>(UTF8_ORDER);
		for (String biome : biomes) {
			validateResourceLocation(biome);
			if (Utf8.encode(biome).length > limits.maxStringBytes()) {
				throw new IllegalArgumentException("biome exceeds string limit");
			}
			paletteSet.add(biome);
		}
		List<String> palette = List.copyOf(paletteSet);
		Map<String, Integer> paletteIndices = indexPalette(palette);
		CanonicalWriter writer = new CanonicalWriter(limits.maxComponentBytes(), limits.maxStringBytes());
		writer.writeString(BIOME_SECTION_DOMAIN);
		writer.writeI32(sectionY);
		writer.writeU16(palette.size());
		for (String value : palette) {
			writer.writeString(value);
		}
		for (String biome : biomes) {
			writer.writeU16(paletteIndices.get(biome));
		}
		return writer.toByteArray();
	}

	public static byte[] encodeBlockEntities(List<BlockEntityValue> entries) {
		return encodeBlockEntities(entries, CanonicalLimits.defaults());
	}

	public static byte[] encodeBlockEntities(List<BlockEntityValue> entries, CanonicalLimits limits) {
		Objects.requireNonNull(entries, "entries");
		Objects.requireNonNull(limits, "limits");
		validateCollectionLength(entries.size(), limits, "block entity count");
		List<BlockEntityValue> ordered = new ArrayList<>(entries);
		for (BlockEntityValue entry : ordered) {
			if (entry.localX() < 0 || entry.localX() > 15 || entry.localZ() < 0 || entry.localZ() > 15) {
				throw new IllegalArgumentException("block entity has invalid local coordinates");
			}
			validateResourceLocation(entry.type());
		}
		ordered.sort(Comparator.comparingInt(BlockEntityValue::blockY)
				.thenComparingInt(BlockEntityValue::localZ)
				.thenComparingInt(BlockEntityValue::localX)
				.thenComparing(BlockEntityValue::type, UTF8_ORDER));
		for (int index = 1; index < ordered.size(); index++) {
			if (sameBlockEntityKey(ordered.get(index - 1), ordered.get(index))) {
				throw new IllegalArgumentException("duplicate block entity key");
			}
		}

		CanonicalWriter writer = new CanonicalWriter(limits.maxComponentBytes(), limits.maxStringBytes());
		writer.writeString(BLOCK_ENTITIES_DOMAIN);
		writer.writeU32(ordered.size());
		for (BlockEntityValue entry : ordered) {
			byte[] nbt = encodeNbt(entry.nbt(), limits);
			writer.writeU8(entry.localX());
			writer.writeI32(entry.blockY());
			writer.writeU8(entry.localZ());
			writer.writeString(entry.type());
			writer.writeBytes(nbt);
		}
		return writer.toByteArray();
	}

	public static byte[] encodeNbt(NbtValue value) {
		return encodeNbt(value, CanonicalLimits.defaults());
	}

	public static byte[] encodeNbt(NbtValue value, CanonicalLimits limits) {
		Objects.requireNonNull(value, "value");
		Objects.requireNonNull(limits, "limits");
		CanonicalWriter writer = new CanonicalWriter(limits.maxNbtBytes(), limits.maxStringBytes());
		encodeNbtValue(writer, value, limits, 0);
		return writer.toByteArray();
	}

	private static void encodeNbtValue(
			CanonicalWriter writer, NbtValue value, CanonicalLimits limits, int depth) {
		writer.writeU8(Byte.toUnsignedInt(value.type()));
		encodeNbtPayload(writer, value, limits, depth);
	}

	private static void encodeNbtPayload(
			CanonicalWriter writer, NbtValue value, CanonicalLimits limits, int depth) {
		if (depth > limits.maxNbtDepth()) {
			throw new IllegalArgumentException("NBT depth exceeds " + limits.maxNbtDepth());
		}
		if (value instanceof NbtValue.ByteTag tag) {
			writer.writeI8(tag.value());
		} else if (value instanceof NbtValue.ShortTag tag) {
			writer.writeI16(tag.value());
		} else if (value instanceof NbtValue.IntTag tag) {
			writer.writeI32(tag.value());
		} else if (value instanceof NbtValue.LongTag tag) {
			writer.writeI64(tag.value());
		} else if (value instanceof NbtValue.FloatTag tag) {
			writer.writeI32(tag.rawBits());
		} else if (value instanceof NbtValue.DoubleTag tag) {
			writer.writeI64(tag.rawBits());
		} else if (value instanceof NbtValue.ByteArrayTag tag) {
			byte[] array = tag.value();
			validateCollectionLength(array.length, limits, "byte array");
			writer.writeU32(array.length);
			writer.write(array);
		} else if (value instanceof NbtValue.StringTag tag) {
			writer.writeString(tag.value());
		} else if (value instanceof NbtValue.ListTag tag) {
			encodeList(writer, tag, limits, depth);
		} else if (value instanceof NbtValue.CompoundTag tag) {
			encodeCompound(writer, tag, limits, depth);
		} else if (value instanceof NbtValue.IntArrayTag tag) {
			int[] array = tag.value();
			validateCollectionLength(array.length, limits, "int array");
			writer.writeU32(array.length);
			for (int item : array) {
				writer.writeI32(item);
			}
		} else if (value instanceof NbtValue.LongArrayTag tag) {
			long[] array = tag.value();
			validateCollectionLength(array.length, limits, "long array");
			writer.writeU32(array.length);
			for (long item : array) {
				writer.writeI64(item);
			}
		} else {
			throw new IllegalArgumentException("unsupported NBT value " + value.getClass().getName());
		}
	}

	private static void encodeList(
			CanonicalWriter writer, NbtValue.ListTag list, CanonicalLimits limits, int depth) {
		validateCollectionLength(list.values().size(), limits, "list");
		int elementType = Byte.toUnsignedInt(list.elementType());
		if (elementType > NbtValue.LONG_ARRAY || (elementType == NbtValue.END && !list.values().isEmpty())) {
			throw new IllegalArgumentException("invalid NBT list element type");
		}
		writer.writeU8(elementType);
		writer.writeU32(list.values().size());
		for (NbtValue item : list.values()) {
			if (item.type() != list.elementType()) {
				throw new IllegalArgumentException("NBT list item type mismatch");
			}
			encodeNbtPayload(writer, item, limits, depth + 1);
		}
	}

	private static void encodeCompound(
			CanonicalWriter writer, NbtValue.CompoundTag compound, CanonicalLimits limits, int depth) {
		validateCollectionLength(compound.entries().size(), limits, "compound");
		List<NbtValue.Entry> entries = new ArrayList<>(compound.entries());
		entries.sort(Comparator.comparing(NbtValue.Entry::name, UTF8_ORDER));
		writer.writeU32(entries.size());
		String previousName = null;
		for (NbtValue.Entry entry : entries) {
			if (entry.name().equals(previousName)) {
				throw new IllegalArgumentException("duplicate NBT compound key " + entry.name());
			}
			writer.writeString(entry.name());
			encodeNbtValue(writer, entry.value(), limits, depth + 1);
			previousName = entry.name();
		}
	}

	private static Map<String, Integer> indexPalette(List<String> palette) {
		Map<String, Integer> result = new HashMap<>(palette.size());
		for (int index = 0; index < palette.size(); index++) {
			result.put(palette.get(index), index);
		}
		return result;
	}

	private static void validateResourceLocation(String value) {
		Objects.requireNonNull(value, "resource location");
		Utf8.encode(value);
		int separator = value.indexOf(':');
		if (separator <= 0 || separator != value.lastIndexOf(':') || separator == value.length() - 1) {
			throw new IllegalArgumentException("invalid resource location " + value);
		}
		for (int index = 0; index < separator; index++) {
			char character = value.charAt(index);
			if (!isLowerAlphaNumeric(character) && character != '_' && character != '-' && character != '.') {
				throw new IllegalArgumentException("invalid resource location " + value);
			}
		}
		for (int index = separator + 1; index < value.length(); index++) {
			char character = value.charAt(index);
			if (!isLowerAlphaNumeric(character)
					&& character != '_'
					&& character != '-'
					&& character != '.'
					&& character != '/') {
				throw new IllegalArgumentException("invalid resource location " + value);
			}
		}
	}

	private static boolean isLowerAlphaNumeric(char character) {
		return (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9');
	}

	private static void validateStateToken(String value, String label) {
		if (value.isEmpty()) {
			throw new IllegalArgumentException(label + " is empty");
		}
		Utf8.encode(value);
		for (int offset = 0; offset < value.length(); ) {
			int codePoint = value.codePointAt(offset);
			if (Character.isWhitespace(codePoint)
					|| Character.isSpaceChar(codePoint)
					|| codePoint == ','
					|| codePoint == '='
					|| codePoint == '['
					|| codePoint == ']') {
				throw new IllegalArgumentException("invalid " + label + " " + value);
			}
			offset += Character.charCount(codePoint);
		}
	}

	private static void validateCollectionLength(int length, CanonicalLimits limits, String label) {
		if (length < 0 || length > limits.maxCollectionItems()) {
			throw new IllegalArgumentException(label + " exceeds collection limit");
		}
	}

	private static boolean sameBlockEntityKey(BlockEntityValue left, BlockEntityValue right) {
		return left.blockY() == right.blockY()
				&& left.localZ() == right.localZ()
				&& left.localX() == right.localX()
				&& left.type().equals(right.type());
	}
}
