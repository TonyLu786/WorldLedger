package org.worldledger.fabric.canonical;

import java.io.ByteArrayOutputStream;

final class CanonicalWriter {
	private final ByteArrayOutputStream buffer = new ByteArrayOutputStream();
	private final int maxBytes;
	private final int maxStringBytes;

	CanonicalWriter(int maxBytes, int maxStringBytes) {
		if (maxBytes <= 0 || maxStringBytes <= 0) {
			throw new IllegalArgumentException("writer limits must be positive");
		}
		this.maxBytes = maxBytes;
		this.maxStringBytes = maxStringBytes;
	}

	byte[] toByteArray() {
		return buffer.toByteArray();
	}

	void write(byte[] value) {
		if ((long) buffer.size() + value.length > maxBytes) {
			throw new IllegalArgumentException("canonical component exceeds " + maxBytes + " bytes");
		}
		buffer.writeBytes(value);
	}

	void writeU8(int value) {
		if (value < 0 || value > 0xff) {
			throw new IllegalArgumentException("value does not fit u8");
		}
		write(new byte[] {(byte) value});
	}

	void writeU16(int value) {
		if (value < 0 || value > 0xffff) {
			throw new IllegalArgumentException("value does not fit u16");
		}
		write(new byte[] {(byte) (value >>> 8), (byte) value});
	}

	void writeU32(long value) {
		if (value < 0 || value > 0xffff_ffffL) {
			throw new IllegalArgumentException("value does not fit u32");
		}
		write(new byte[] {
			(byte) (value >>> 24),
			(byte) (value >>> 16),
			(byte) (value >>> 8),
			(byte) value
		});
	}

	void writeI8(byte value) {
		writeU8(Byte.toUnsignedInt(value));
	}

	void writeI16(short value) {
		writeU16(Short.toUnsignedInt(value));
	}

	void writeI32(int value) {
		writeU32(Integer.toUnsignedLong(value));
	}

	void writeI64(long value) {
		write(new byte[] {
			(byte) (value >>> 56),
			(byte) (value >>> 48),
			(byte) (value >>> 40),
			(byte) (value >>> 32),
			(byte) (value >>> 24),
			(byte) (value >>> 16),
			(byte) (value >>> 8),
			(byte) value
		});
	}

	void writeString(String value) {
		byte[] encoded = Utf8.encode(value);
		if (encoded.length > maxStringBytes) {
			throw new IllegalArgumentException("string exceeds " + maxStringBytes + " bytes");
		}
		writeU32(encoded.length);
		write(encoded);
	}

	void writeBytes(byte[] value) {
		writeU32(value.length);
		write(value);
	}
}
