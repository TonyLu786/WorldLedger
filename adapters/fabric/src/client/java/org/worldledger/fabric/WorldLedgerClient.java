package org.worldledger.fabric;

import net.fabricmc.api.ClientModInitializer;
import net.fabricmc.loader.api.FabricLoader;

public final class WorldLedgerClient implements ClientModInitializer {
	@Override
	public void onInitializeClient() {
		CapturePaths paths = CapturePaths.fromFabricConfigDirectory(FabricLoader.getInstance().getConfigDir());
		WorldLedgerRuntime.initialize(paths);
	}
}
