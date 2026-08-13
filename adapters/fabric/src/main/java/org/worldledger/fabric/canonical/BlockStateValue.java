package org.worldledger.fabric.canonical;

import java.util.List;
import java.util.Objects;

public record BlockStateValue(String name, List<StateProperty> properties) {
	public BlockStateValue {
		Objects.requireNonNull(name, "name");
		properties = List.copyOf(Objects.requireNonNull(properties, "properties"));
	}

	public static BlockStateValue simple(String name) {
		return new BlockStateValue(name, List.of());
	}
}
