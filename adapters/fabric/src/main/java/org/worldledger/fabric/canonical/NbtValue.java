package org.worldledger.fabric.canonical;

import java.util.List;
import java.util.Objects;

public sealed interface NbtValue
		permits NbtValue.ByteTag,
				NbtValue.ShortTag,
				NbtValue.IntTag,
				NbtValue.LongTag,
				NbtValue.FloatTag,
				NbtValue.DoubleTag,
				NbtValue.ByteArrayTag,
				NbtValue.StringTag,
				NbtValue.ListTag,
				NbtValue.CompoundTag,
				NbtValue.IntArrayTag,
				NbtValue.LongArrayTag {
	byte END = 0;
	byte BYTE = 1;
	byte SHORT = 2;
	byte INT = 3;
	byte LONG = 4;
	byte FLOAT = 5;
	byte DOUBLE = 6;
	byte BYTE_ARRAY = 7;
	byte STRING = 8;
	byte LIST = 9;
	byte COMPOUND = 10;
	byte INT_ARRAY = 11;
	byte LONG_ARRAY = 12;

	byte type();

	record ByteTag(byte value) implements NbtValue {
		@Override
		public byte type() {
			return BYTE;
		}
	}

	record ShortTag(short value) implements NbtValue {
		@Override
		public byte type() {
			return SHORT;
		}
	}

	record IntTag(int value) implements NbtValue {
		@Override
		public byte type() {
			return INT;
		}
	}

	record LongTag(long value) implements NbtValue {
		@Override
		public byte type() {
			return LONG;
		}
	}

	record FloatTag(int rawBits) implements NbtValue {
		@Override
		public byte type() {
			return FLOAT;
		}
	}

	record DoubleTag(long rawBits) implements NbtValue {
		@Override
		public byte type() {
			return DOUBLE;
		}
	}

	record ByteArrayTag(byte[] value) implements NbtValue {
		public ByteArrayTag {
			value = Objects.requireNonNull(value, "value").clone();
		}

		@Override
		public byte[] value() {
			return value.clone();
		}

		@Override
		public byte type() {
			return BYTE_ARRAY;
		}
	}

	record StringTag(String value) implements NbtValue {
		public StringTag {
			Objects.requireNonNull(value, "value");
		}

		@Override
		public byte type() {
			return STRING;
		}
	}

	record ListTag(byte elementType, List<NbtValue> values) implements NbtValue {
		public ListTag {
			values = List.copyOf(Objects.requireNonNull(values, "values"));
		}

		@Override
		public byte type() {
			return LIST;
		}
	}

	record Entry(String name, NbtValue value) {
		public Entry {
			Objects.requireNonNull(name, "name");
			Objects.requireNonNull(value, "value");
		}
	}

	record CompoundTag(List<Entry> entries) implements NbtValue {
		public CompoundTag {
			entries = List.copyOf(Objects.requireNonNull(entries, "entries"));
		}

		@Override
		public byte type() {
			return COMPOUND;
		}
	}

	record IntArrayTag(int[] value) implements NbtValue {
		public IntArrayTag {
			value = Objects.requireNonNull(value, "value").clone();
		}

		@Override
		public int[] value() {
			return value.clone();
		}

		@Override
		public byte type() {
			return INT_ARRAY;
		}
	}

	record LongArrayTag(long[] value) implements NbtValue {
		public LongArrayTag {
			value = Objects.requireNonNull(value, "value").clone();
		}

		@Override
		public long[] value() {
			return value.clone();
		}

		@Override
		public byte type() {
			return LONG_ARRAY;
		}
	}
}
