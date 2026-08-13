package org.worldledger.fabric.canonical;

import java.util.Objects;

public record StateProperty(String name, String value) {
	public StateProperty {
		Objects.requireNonNull(name, "name");
		Objects.requireNonNull(value, "value");
	}
}
