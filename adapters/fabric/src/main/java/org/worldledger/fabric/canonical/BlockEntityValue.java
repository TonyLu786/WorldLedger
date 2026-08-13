package org.worldledger.fabric.canonical;

import java.util.Objects;

public record BlockEntityValue(int localX, int blockY, int localZ, String type, NbtValue.CompoundTag nbt) {
	public BlockEntityValue {
		Objects.requireNonNull(type, "type");
		Objects.requireNonNull(nbt, "nbt");
	}
}
