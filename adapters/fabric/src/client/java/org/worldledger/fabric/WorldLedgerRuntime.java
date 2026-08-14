package org.worldledger.fabric;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import net.fabricmc.fabric.api.client.event.lifecycle.v1.ClientChunkEvents;
import net.fabricmc.fabric.api.client.event.lifecycle.v1.ClientLevelEvents;
import net.fabricmc.fabric.api.client.event.lifecycle.v1.ClientLifecycleEvents;
import net.fabricmc.fabric.api.client.event.lifecycle.v1.ClientTickEvents;
import net.fabricmc.fabric.api.client.networking.v1.ClientPlayConnectionEvents;
import net.minecraft.client.Minecraft;
import net.minecraft.network.protocol.game.ClientboundBlockEntityDataPacket;
import net.minecraft.network.protocol.game.ClientboundBlockUpdatePacket;
import net.minecraft.network.protocol.game.ClientboundChunksBiomesPacket;
import net.minecraft.network.protocol.game.ClientboundLevelChunkWithLightPacket;
import net.minecraft.network.protocol.game.ClientboundSectionBlocksUpdatePacket;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.worldledger.fabric.capture.BundleSpoolWriter;

public final class WorldLedgerRuntime {
	private record PendingJoin(Minecraft client, long epoch) {}

	private static final Logger LOGGER = LoggerFactory.getLogger("worldledger");
	private static final AtomicBoolean INITIALIZED = new AtomicBoolean();
	private static final AtomicLong CONNECTION_EPOCH = new AtomicLong();
	private static final AtomicReference<CaptureCoordinator> COORDINATOR = new AtomicReference<>();
	private static final AtomicReference<PendingJoin> PENDING_JOIN = new AtomicReference<>();

	private WorldLedgerRuntime() {}

	public static void initialize(CapturePaths paths) {
		if (!INITIALIZED.compareAndSet(false, true)) {
			throw new IllegalStateException("WorldLedger runtime is already initialized");
		}
		registerEvents();
		Thread.ofPlatform()
				.name("worldledger-bootstrap")
				.daemon(true)
				.start(() -> bootstrap(paths));
	}

	public static void onFullChunkPacket(ClientboundLevelChunkWithLightPacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			coordinator.onFullChunkPacket(packet);
		}
	}

	public static void onBlockUpdate(ClientboundBlockUpdatePacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			coordinator.onBlockApplied(packet.getPos());
		}
	}

	public static void onSectionBlocksUpdate(ClientboundSectionBlocksUpdatePacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			packet.runUpdates((position, state) -> coordinator.onBlockApplied(position));
		}
	}

	public static void onBlockEntityData(ClientboundBlockEntityDataPacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			coordinator.onBlockEntityPacket(packet);
		}
	}

	public static void onBiomeUpdate(ClientboundChunksBiomesPacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			for (ClientboundChunksBiomesPacket.ChunkBiomeData data : packet.chunkBiomeData()) {
				coordinator.onBiomeApplied(data.pos().x(), data.pos().z());
			}
		}
	}

	private static void registerEvents() {
		ClientPlayConnectionEvents.JOIN.register((handler, sender, client) -> {
			PendingJoin pending = new PendingJoin(client, CONNECTION_EPOCH.incrementAndGet());
			PENDING_JOIN.set(pending);
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null && PENDING_JOIN.compareAndSet(pending, null)) {
				coordinator.onJoin(client);
			}
		});
		ClientPlayConnectionEvents.DISCONNECT.register((handler, client) -> {
			CONNECTION_EPOCH.incrementAndGet();
			PENDING_JOIN.set(null);
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				coordinator.onDisconnect();
			}
		});
		ClientLevelEvents.AFTER_CLIENT_LEVEL_CHANGE.register((client, level) -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				coordinator.onLevelChange(level);
			}
		});
		ClientChunkEvents.CHUNK_LOAD.register((level, chunk) -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				coordinator.onChunkLoad(level, chunk);
			}
		});
		ClientChunkEvents.CHUNK_UNLOAD.register((level, chunk) -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				coordinator.onChunkUnload(level, chunk);
			}
		});
		ClientTickEvents.END_CLIENT_TICK.register(client -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				coordinator.onEndTick();
			}
		});
		ClientLifecycleEvents.CLIENT_STOPPING.register(client -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				coordinator.onClientStopping();
			}
		});
	}

	private static void bootstrap(CapturePaths paths) {
		try {
			paths.ensureDirectories();
			CaptureConfiguration configuration = CaptureConfiguration.loadOrCreate(paths.configDirectory());
			BundleSpoolWriter writer = new BundleSpoolWriter(paths.spoolDirectory());
			CaptureCoordinator coordinator = new CaptureCoordinator(
					configuration, paths.configDirectory().resolve("capture.properties"), writer);
			COORDINATOR.set(coordinator);
			PendingJoin pendingJoin = PENDING_JOIN.getAndSet(null);
			if (pendingJoin != null) {
				pendingJoin.client().execute(() -> {
					if (CONNECTION_EPOCH.get() == pendingJoin.epoch() && COORDINATOR.get() == coordinator) {
						coordinator.onJoin(pendingJoin.client());
					}
				});
			}
			try {
				BundleSpoolWriter.RecoveryReport recovery = writer.recoverTemporaryBundles();
				for (String diagnostic : recovery.diagnostics()) {
					LOGGER.warn("Spool recovery: {}", diagnostic);
				}
			} catch (Exception exception) {
				LOGGER.error("Unable to recover temporary capture bundles; capture remains bounded and active", exception);
			}
			if (configuration.enabled()) {
				LOGGER.info("Capture spool ready at {}", paths.spoolDirectory());
			} else {
				LOGGER.warn(
						"Capture is disabled until contributor is set in {}",
						paths.configDirectory().resolve("capture.properties"));
			}
		} catch (Exception exception) {
			LOGGER.error("Unable to initialize the WorldLedger capture runtime", exception);
		}
	}
}
