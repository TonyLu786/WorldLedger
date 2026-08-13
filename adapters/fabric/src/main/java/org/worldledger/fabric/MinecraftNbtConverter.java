package org.worldledger.fabric;

import java.util.ArrayList;
import java.util.List;

import net.minecraft.nbt.ByteArrayTag;
import net.minecraft.nbt.ByteTag;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.DoubleTag;
import net.minecraft.nbt.FloatTag;
import net.minecraft.nbt.IntArrayTag;
import net.minecraft.nbt.IntTag;
import net.minecraft.nbt.ListTag;
import net.minecraft.nbt.LongArrayTag;
import net.minecraft.nbt.LongTag;
import net.minecraft.nbt.ShortTag;
import net.minecraft.nbt.StringTag;
import net.minecraft.nbt.Tag;
import org.worldledger.fabric.canonical.CanonicalLimits;
import org.worldledger.fabric.canonical.NbtValue;

final class MinecraftNbtConverter {
	record ConvertedCompound(NbtValue.CompoundTag value, int sourceBytes) {}

	private MinecraftNbtConverter() {}

	static ConvertedCompound convertCompound(CompoundTag tag) {
		CanonicalLimits limits = CanonicalLimits.defaults();
		long sourceBytes = tag.sizeInBytes();
		if (sourceBytes > limits.maxNbtBytes()) {
			throw new IllegalArgumentException("network NBT exceeds byte limit");
		}
		return new ConvertedCompound(
				(NbtValue.CompoundTag) convert(tag, limits, 0), Math.toIntExact(sourceBytes));
	}

	private static NbtValue convert(Tag tag, CanonicalLimits limits, int depth) {
		if (depth > limits.maxNbtDepth()) {
			throw new IllegalArgumentException("network NBT exceeds depth limit");
		}
		return switch (tag.getId()) {
			case Tag.TAG_BYTE -> new NbtValue.ByteTag(((ByteTag) tag).value());
			case Tag.TAG_SHORT -> new NbtValue.ShortTag(((ShortTag) tag).value());
			case Tag.TAG_INT -> new NbtValue.IntTag(((IntTag) tag).value());
			case Tag.TAG_LONG -> new NbtValue.LongTag(((LongTag) tag).value());
			case Tag.TAG_FLOAT -> new NbtValue.FloatTag(Float.floatToRawIntBits(((FloatTag) tag).value()));
			case Tag.TAG_DOUBLE -> new NbtValue.DoubleTag(Double.doubleToRawLongBits(((DoubleTag) tag).value()));
			case Tag.TAG_BYTE_ARRAY -> {
				byte[] value = ((ByteArrayTag) tag).getAsByteArray();
				checkCollection(value.length, limits);
				yield new NbtValue.ByteArrayTag(value);
			}
			case Tag.TAG_STRING -> new NbtValue.StringTag(((StringTag) tag).value());
			case Tag.TAG_LIST -> convertList((ListTag) tag, limits, depth);
			case Tag.TAG_COMPOUND -> convertCompoundPayload((CompoundTag) tag, limits, depth);
			case Tag.TAG_INT_ARRAY -> {
				int[] value = ((IntArrayTag) tag).getAsIntArray();
				checkCollection(value.length, limits);
				yield new NbtValue.IntArrayTag(value);
			}
			case Tag.TAG_LONG_ARRAY -> {
				long[] value = ((LongArrayTag) tag).getAsLongArray();
				checkCollection(value.length, limits);
				yield new NbtValue.LongArrayTag(value);
			}
			default -> throw new IllegalArgumentException("unsupported network NBT tag " + tag.getId());
		};
	}

	private static NbtValue.ListTag convertList(ListTag tag, CanonicalLimits limits, int depth) {
		checkCollection(tag.size(), limits);
		byte elementType = tag.isEmpty() ? Tag.TAG_END : tag.getFirst().getId();
		List<NbtValue> values = new ArrayList<>(tag.size());
		for (Tag item : tag) {
			if (item.getId() != elementType) {
				throw new IllegalArgumentException("heterogeneous network NBT list is unsupported");
			}
			values.add(convert(item, limits, depth + 1));
		}
		return new NbtValue.ListTag(elementType, values);
	}

	private static NbtValue.CompoundTag convertCompoundPayload(
			CompoundTag tag, CanonicalLimits limits, int depth) {
		checkCollection(tag.size(), limits);
		List<NbtValue.Entry> entries = new ArrayList<>(tag.size());
		for (var entry : tag.entrySet()) {
			entries.add(new NbtValue.Entry(entry.getKey(), convert(entry.getValue(), limits, depth + 1)));
		}
		return new NbtValue.CompoundTag(entries);
	}

	private static void checkCollection(int size, CanonicalLimits limits) {
		if (size > limits.maxCollectionItems()) {
			throw new IllegalArgumentException("network NBT collection exceeds item limit");
		}
	}
}
