package org.worldledger.fabric;

import net.fabricmc.api.ClientModInitializer;
import net.fabricmc.loader.api.FabricLoader;

public final class WorldLedgerClient implements ClientModInitializer {
	@Override
	public void onInitializeClient() {
		CapturePaths paths = CapturePaths.fromFabricConfigDirectory(FabricLoader.getInstance().getConfigDir());
		WorldLedgerRuntime.initialize(paths);
		// Registered here rather than inside the runtime's background bootstrap,
		// because command registration has to have happened before the player can
		// type anything, and the runtime deliberately starts off the client thread.
		CaptureCommands.register();
	}
}
