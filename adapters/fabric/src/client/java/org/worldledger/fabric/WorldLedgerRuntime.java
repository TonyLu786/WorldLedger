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
			guard("a chunk packet", () -> coordinator.onFullChunkPacket(packet));
		}
	}

	/**
	 * What capture is doing, or null while the runtime is still starting.
	 *
	 * <p>Bootstrap runs on its own thread so a slow disk cannot delay the title
	 * screen, which means a command typed early can arrive before there is
	 * anything to report. Saying so is better than reporting a state that is
	 * merely the absence of one.
	 */
	public static CaptureStatus status() {
		CaptureCoordinator coordinator = COORDINATOR.get();
		return coordinator == null ? null : coordinator.status();
	}

	/** Re-reads the configuration file, or null if there is nothing to reload yet. */
	public static String reload() {
		CaptureCoordinator coordinator = COORDINATOR.get();
		return coordinator == null ? null : coordinator.reload();
	}

	public static void onBlockUpdate(ClientboundBlockUpdatePacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			guard("a block update", () -> coordinator.onBlockApplied(packet.getPos()));
		}
	}

	public static void onSectionBlocksUpdate(ClientboundSectionBlocksUpdatePacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			guard("a section block update", () -> packet.runUpdates((position, state) -> coordinator.onBlockApplied(position)));
		}
	}

	public static void onBlockEntityData(ClientboundBlockEntityDataPacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			guard("a block entity packet", () -> coordinator.onBlockEntityPacket(packet));
		}
	}

	public static void onBiomeUpdate(ClientboundChunksBiomesPacket packet) {
		CaptureCoordinator coordinator = COORDINATOR.get();
		if (coordinator != null) {
			guard("a biome update", () -> {
				for (ClientboundChunksBiomesPacket.ChunkBiomeData data : packet.chunkBiomeData()) {
					coordinator.onBiomeApplied(data.pos().x(), data.pos().z());
				}
			});
		}
	}

	/**
	 * Runs one piece of capture work, and never lets it reach Minecraft.
	 *
	 * <p>This class is the seam between the game and this mod: the mixins inject
	 * at the tail of vanilla's own packet handlers, and the event registrations
	 * below run inside the client's tick and connection handling. Anything that
	 * escapes from here unwinds into code that has no idea what this mod is, and
	 * takes the client down with it.
	 *
	 * <p>Nothing this mod does is worth that. Capture is a passenger: it records
	 * what the client was shown, and a session that records nothing is a session
	 * that recorded nothing. A player losing their connection -- or their
	 * afternoon -- because a chunk could not be read is the one outcome that is
	 * worse than not capturing at all.
	 *
	 * <p>Errors are not caught. An OutOfMemoryError or a linkage failure is not
	 * this mod's to absorb, and pretending to carry on after one would hide the
	 * only evidence of what happened.
	 */
	private static void guard(String what, Runnable body) {
		try {
			body.run();
		} catch (RuntimeException exception) {
			LOGGER.error("Capture failed during {}; the client is unaffected", what, exception);
		}
	}

	private static void registerEvents() {
		ClientPlayConnectionEvents.JOIN.register((handler, sender, client) -> {
			PendingJoin pending = new PendingJoin(client, CONNECTION_EPOCH.incrementAndGet());
			PENDING_JOIN.set(pending);
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null && PENDING_JOIN.compareAndSet(pending, null)) {
				guard("joining a server", () -> coordinator.onJoin(client));
			}
		});
		ClientPlayConnectionEvents.DISCONNECT.register((handler, client) -> {
			CONNECTION_EPOCH.incrementAndGet();
			PENDING_JOIN.set(null);
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				guard("leaving a server", coordinator::onDisconnect);
			}
		});
		ClientLevelEvents.AFTER_CLIENT_LEVEL_CHANGE.register((client, level) -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				guard("a dimension change", () -> coordinator.onLevelChange(level));
			}
		});
		ClientChunkEvents.CHUNK_LOAD.register((level, chunk) -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				guard("a chunk loading", () -> coordinator.onChunkLoad(level, chunk));
			}
		});
		ClientChunkEvents.CHUNK_UNLOAD.register((level, chunk) -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				guard("a chunk unloading", () -> coordinator.onChunkUnload(level, chunk));
			}
		});
		ClientTickEvents.END_CLIENT_TICK.register(client -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				guard("the end of a tick", coordinator::onEndTick);
			}
		});
		ClientLifecycleEvents.CLIENT_STOPPING.register(client -> {
			CaptureCoordinator coordinator = COORDINATOR.get();
			if (coordinator != null) {
				guard("the client stopping", coordinator::onClientStopping);
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
						// This runs on the client thread, so it needs the same
						// guard as the join it stands in for.
						guard("joining a server", () -> coordinator.onJoin(pendingJoin.client()));
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
