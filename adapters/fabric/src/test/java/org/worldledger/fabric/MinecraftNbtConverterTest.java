package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.List;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.ListTag;
import org.junit.jupiter.api.Test;
import org.worldledger.fabric.canonical.MinecraftJavaV1;
import org.worldledger.fabric.canonical.NbtValue;

final class MinecraftNbtConverterTest {
	@Test
	void packetNbtConversionPreservesTagTypesOrderAndRawFloatBits() {
		int floatBits = 0x7fc01234;
		long doubleBits = 0xfff0000000000000L;
		CompoundTag source = new CompoundTag();
		source.putByte("byte", (byte) -128);
		source.putShort("short", (short) -32768);
		source.putInt("int", Integer.MIN_VALUE);
		source.putLong("long", Long.MIN_VALUE);
		source.putFloat("float", Float.intBitsToFloat(floatBits));
		source.putDouble("double", Double.longBitsToDouble(doubleBits));
		source.putByteArray("bytes", new byte[] {0, -1, 127, -128});
		source.putString("string", "fixture");
		ListTag list = new ListTag();
		list.addTag(0, net.minecraft.nbt.IntTag.valueOf(3));
		list.addTag(1, net.minecraft.nbt.IntTag.valueOf(1));
		source.put("list", list);
		source.put("nested", new CompoundTag());
		source.putIntArray("ints", new int[] {Integer.MIN_VALUE, 0, Integer.MAX_VALUE});
		source.putLongArray("longs", new long[] {Long.MIN_VALUE, 0, Long.MAX_VALUE});

		NbtValue.CompoundTag expected = new NbtValue.CompoundTag(List.of(
				new NbtValue.Entry("byte", new NbtValue.ByteTag((byte) -128)),
				new NbtValue.Entry("short", new NbtValue.ShortTag((short) -32768)),
				new NbtValue.Entry("int", new NbtValue.IntTag(Integer.MIN_VALUE)),
				new NbtValue.Entry("long", new NbtValue.LongTag(Long.MIN_VALUE)),
				new NbtValue.Entry("float", new NbtValue.FloatTag(floatBits)),
				new NbtValue.Entry("double", new NbtValue.DoubleTag(doubleBits)),
				new NbtValue.Entry("bytes", new NbtValue.ByteArrayTag(new byte[] {0, -1, 127, -128})),
				new NbtValue.Entry("string", new NbtValue.StringTag("fixture")),
				new NbtValue.Entry(
						"list",
						new NbtValue.ListTag(
								NbtValue.INT,
								List.of(new NbtValue.IntTag(3), new NbtValue.IntTag(1)))),
				new NbtValue.Entry("nested", new NbtValue.CompoundTag(List.of())),
				new NbtValue.Entry(
						"ints", new NbtValue.IntArrayTag(new int[] {Integer.MIN_VALUE, 0, Integer.MAX_VALUE})),
				new NbtValue.Entry(
						"longs", new NbtValue.LongArrayTag(new long[] {Long.MIN_VALUE, 0, Long.MAX_VALUE}))));

		MinecraftNbtConverter.ConvertedCompound converted = MinecraftNbtConverter.convertCompound(source);
		assertTrue(converted.sourceBytes() > 0);
		assertArrayEquals(
				MinecraftJavaV1.encodeNbt(expected),
				MinecraftJavaV1.encodeNbt(converted.value()));
	}
}
