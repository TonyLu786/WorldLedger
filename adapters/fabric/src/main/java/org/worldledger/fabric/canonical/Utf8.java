package org.worldledger.fabric.canonical;

import java.nio.charset.StandardCharsets;
import java.util.Objects;

final class Utf8 {
	private Utf8() {}

	static byte[] encode(String value) {
		Objects.requireNonNull(value, "value");
		for (int index = 0; index < value.length(); index++) {
			char current = value.charAt(index);
			if (Character.isHighSurrogate(current)) {
				if (++index >= value.length() || !Character.isLowSurrogate(value.charAt(index))) {
					throw new IllegalArgumentException("string contains an unpaired high surrogate");
				}
			} else if (Character.isLowSurrogate(current)) {
				throw new IllegalArgumentException("string contains an unpaired low surrogate");
			}
		}
		return value.getBytes(StandardCharsets.UTF_8);
	}

	static int compare(String left, String right) {
		byte[] leftBytes = encode(left);
		byte[] rightBytes = encode(right);
		int shared = Math.min(leftBytes.length, rightBytes.length);
		for (int index = 0; index < shared; index++) {
			int comparison = Integer.compare(Byte.toUnsignedInt(leftBytes[index]), Byte.toUnsignedInt(rightBytes[index]));
			if (comparison != 0) {
				return comparison;
			}
		}
		return Integer.compare(leftBytes.length, rightBytes.length);
	}
}
