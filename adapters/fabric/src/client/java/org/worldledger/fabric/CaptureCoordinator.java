package org.worldledger.fabric;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Optional;
import java.util.UUID;

import net.minecraft.client.Minecraft;
import net.minecraft.client.multiplayer.ClientLevel;
import net.minecraft.client.multiplayer.ServerData;
import net.minecraft.client.multiplayer.resolver.ServerAddress;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.protocol.game.ClientboundBlockEntityDataPacket;
import net.minecraft.network.protocol.game.ClientboundLevelChunkWithLightPacket;
import net.minecraft.world.level.block.entity.BlockEntity;
import net.minecraft.world.level.chunk.LevelChunk;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.worldledger.fabric.canonical.BlockEntityValue;
import org.worldledger.fabric.capture.BlockEntityNetworkCache;
import org.worldledger.fabric.capture.BoundedCaptureQueue;
import org.worldledger.fabric.capture.BundleSpoolWriter;
import org.worldledger.fabric.capture.CaptureJob;
import org.worldledger.fabric.capture.CaptureSequence;
import org.worldledger.fabric.capture.ChunkCoordinate;
import org.worldledger.fabric.capture.DirtyChunkTracker;

final class CaptureCoordinator {
	private static final Logger LOGGER = LoggerFactory.getLogger("worldledger");

	/**
	 * How long a final flush may wait, in total, for the writer to accept the
	 * chunks a disconnect released at once. Bounded so that leaving a server is
	 * never delayed indefinitely by a slow disk.
	 */
	private static final Duration FINAL_FLUSH_BUDGET = Duration.ofSeconds(10);

	private static final class ActiveSession {
		private final String id = UUID.randomUUID().toString();
		private final String serverId;
		private final String serverAddress;
		private final String contributor;
		private final CaptureSequence sequence = new CaptureSequence();
		private ClientLevel level;
		private long enqueued;
		private long droppedCoverage;
		private long backpressureEvents;
		private long snapshotFailures;

		private ActiveSession(
				String serverId, String serverAddress, String contributor, ClientLevel level) {
			this.serverId = serverId;
			this.serverAddress = serverAddress;
			this.contributor = contributor;
			this.level = level;
		}

	}

	private final CaptureConfiguration configuration;
	private final DirtyChunkTracker dirtyChunks;
	private final BlockEntityNetworkCache blockEntities = new BlockEntityNetworkCache();
	private final BoundedCaptureQueue queue;
	private ActiveSession session;
	private long tick;

	CaptureCoordinator(CaptureConfiguration configuration, BundleSpoolWriter writer) {
		this.configuration = configuration;
		this.dirtyChunks = new DirtyChunkTracker(
				configuration.coalesceTicks(), Math.max(20L, configuration.coalesceTicks() * 10L));
		this.queue = new BoundedCaptureQueue(
				configuration.queueCapacity(),
				writer::write,
				(job, exception) -> {
					LOGGER.error(
							"Failed to spool capture session={} sequence={} chunk={},{}",
							job.sessionId(),
							job.sequence(),
							job.chunk().x(),
							job.chunk().z(),
							exception);
				});
	}

	void onJoin(Minecraft client) {
		if (session != null) {
			onDisconnect();
		}
		if (!configuration.enabled()) {
			return;
		}
		if (client.hasSingleplayerServer()) {
			LOGGER.info("Single-player session ignored; multiplayer capture only");
			return;
		}
		ServerData server = client.getCurrentServer();
		if (server == null || server.ip == null || !ServerAddress.isValidAddress(server.ip)) {
			LOGGER.warn("Capture disabled for this connection because its server address is unavailable");
			return;
		}
		String normalizedAddress = normalizeServerAddress(server.ip);
		String serverId = configuration.serverId().isEmpty() ? normalizedAddress : configuration.serverId();
		this.tick = 0;
		this.dirtyChunks.clear();
		this.blockEntities.clear();
		this.session = new ActiveSession(serverId, server.ip.trim(), configuration.contributor(), client.level);
		LOGGER.info("Capture session {} started for {}", session.id, serverId);
	}

	void onLevelChange(ClientLevel level) {
		if (session == null || session.level == level) {
			return;
		}
		flushAll("dimension-transition");
		dirtyChunks.clear();
		blockEntities.clear();
		session.level = level;
	}

	void onChunkLoad(ClientLevel level, LevelChunk chunk) {
		if (!isActiveLevel(level)) {
			return;
		}
		dirtyChunks.load(coordinate(chunk), tick);
	}

	void onChunkUnload(ClientLevel level, LevelChunk chunk) {
		if (!isActiveLevel(level)) {
			return;
		}
		ChunkCoordinate coordinate = coordinate(chunk);
		DirtyChunkTracker.Claim claim = dirtyChunks.claimFinal(coordinate);
		if (claim != null) {
			capture(claim, chunk, "final-unload", false);
		}
		dirtyChunks.forget(coordinate);
		blockEntities.forget(coordinate);
	}

	void onEndTick() {
		tick++;
		if (session == null || session.level == null) {
			return;
		}
		for (DirtyChunkTracker.Claim claim :
				dirtyChunks.claimDue(tick, configuration.maxSnapshotsPerTick())) {
			LevelChunk chunk = session.level.getChunkSource().getChunkNow(claim.chunk().x(), claim.chunk().z());
			if (chunk == null) {
				dirtyChunks.forget(claim.chunk());
				blockEntities.forget(claim.chunk());
				continue;
			}
			capture(claim, chunk, "dirty-flush", true);
		}
	}

	void onFullChunkPacket(ClientboundLevelChunkWithLightPacket packet) {
		if (session == null || session.level == null) {
			return;
		}
		ChunkCoordinate chunk = new ChunkCoordinate(packet.getX(), packet.getZ());
		List<BlockEntityNetworkCache.NetworkEntry> entries = new ArrayList<>();
		long[] baselineSourceBytes = {0};
		try {
			packet.getChunkData().getBlockEntitiesTagsConsumer(packet.getX(), packet.getZ())
					.accept((position, type, tag) -> {
						if (tag == null) {
							return;
						}
						var identifier = BuiltInRegistries.BLOCK_ENTITY_TYPE.getKey(type);
						if (identifier == null) {
							throw new IllegalArgumentException("chunk block entity type is not registered");
						}
						MinecraftNbtConverter.ConvertedCompound converted = MinecraftNbtConverter.convertCompound(tag);
						if (entries.size() >= BlockEntityNetworkCache.DEFAULT_MAX_ENTRIES_PER_CHUNK
								|| converted.sourceBytes()
										> BlockEntityNetworkCache.DEFAULT_MAX_BYTES_PER_CHUNK - baselineSourceBytes[0]) {
							throw new IllegalArgumentException("chunk block-entity baseline exceeds cache limits");
						}
						baselineSourceBytes[0] += converted.sourceBytes();
						entries.add(new BlockEntityNetworkCache.NetworkEntry(
								position(position), identifier.toString(), converted.value(), converted.sourceBytes()));
					});
			blockEntities.replaceBaseline(chunk, entries);
		} catch (RuntimeException exception) {
			blockEntities.markUnknown(chunk);
			LOGGER.warn("Block-entity baseline omitted for chunk {},{}: {}", chunk.x(), chunk.z(), exception.getMessage());
		}
		dirtyChunks.markDirty(chunk, tick);
	}

	void onBlockEntityPacket(ClientboundBlockEntityDataPacket packet) {
		if (session == null || session.level == null) {
			return;
		}
		BlockEntityNetworkCache.Position position = position(packet.getPos());
		ChunkCoordinate chunk = BlockEntityNetworkCache.chunkFor(position);
		BlockEntity currentBlockEntity = session.level.getBlockEntity(packet.getPos());
		if (currentBlockEntity == null || currentBlockEntity.getType() != packet.getType()) {
			return;
		}
		try {
			var identifier = BuiltInRegistries.BLOCK_ENTITY_TYPE.getKey(packet.getType());
			if (identifier == null) {
				throw new IllegalArgumentException("block entity type is not registered");
			}
			MinecraftNbtConverter.ConvertedCompound converted = MinecraftNbtConverter.convertCompound(packet.getTag());
			blockEntities.update(new BlockEntityNetworkCache.NetworkEntry(
					position, identifier.toString(), converted.value(), converted.sourceBytes()));
		} catch (RuntimeException exception) {
			blockEntities.markUnknown(chunk);
			LOGGER.warn("Block-entity component became unknown for chunk {},{}: {}", chunk.x(), chunk.z(), exception.getMessage());
		}
		dirtyChunks.markDirty(chunk, tick);
	}

	void onBlockApplied(BlockPos position) {
		if (session == null || session.level == null) {
			return;
		}
		BlockEntityNetworkCache.Position cachePosition = position(position);
		BlockEntity blockEntity = session.level.getBlockEntity(position);
		if (blockEntity == null) {
			blockEntities.remove(cachePosition);
		} else {
			var identifier = BuiltInRegistries.BLOCK_ENTITY_TYPE.getKey(blockEntity.getType());
			if (identifier == null) {
				blockEntities.markUnknown(BlockEntityNetworkCache.chunkFor(cachePosition));
			} else {
				blockEntities.invalidateUnlessType(cachePosition, identifier.toString());
			}
		}
		dirtyChunks.markDirty(BlockEntityNetworkCache.chunkFor(cachePosition), tick);
	}

	void onBiomeApplied(int chunkX, int chunkZ) {
		if (session != null && session.level != null) {
			dirtyChunks.markDirty(new ChunkCoordinate(chunkX, chunkZ), tick);
		}
	}

	void onDisconnect() {
		if (session == null) {
			return;
		}
		flushAll("final-disconnect");
		LOGGER.info(
				"Capture session {} ended; enqueued={} writer_pending={} dropped={} snapshot_failed={}",
				session.id,
				session.enqueued,
				queue.pending(),
				session.droppedCoverage,
				session.snapshotFailures);
		if (session.backpressureEvents > 0) {
			LOGGER.warn("Capture session encountered {} bounded-queue backpressure event(s)", session.backpressureEvents);
		}
		dirtyChunks.clear();
		blockEntities.clear();
		session = null;
	}

	void onClientStopping() {
		onDisconnect();
		try {
			if (!queue.closeGracefully(Duration.ofSeconds(5))) {
				LOGGER.warn("Capture writer did not drain before shutdown");
			}
		} catch (InterruptedException exception) {
			Thread.currentThread().interrupt();
			LOGGER.warn("Interrupted while draining capture writer");
		}
	}

	/**
	 * Enqueues without waiting during play, and waits up to the remaining budget
	 * during a final flush. An interrupt is honoured immediately: a session that
	 * is being torn down should not be delayed further.
	 */
	private boolean enqueue(CaptureJob job, Duration budget) {
		if (budget.isZero() || budget.isNegative()) {
			return queue.offer(job);
		}
		try {
			return queue.offer(job, budget);
		} catch (InterruptedException exception) {
			Thread.currentThread().interrupt();
			return false;
		}
	}

	private void flushAll(String trigger) {
		if (session == null || session.level == null) {
			return;
		}
		List<DirtyChunkTracker.Claim> claims = dirtyChunks.claimAll();
		// A disconnect releases every dirty chunk at once, which is far more
		// than a queue sized for steady play can hold. The flush is therefore
		// allowed to wait for the writer, under one budget shared by the whole
		// flush so that a slow disk delays leaving a server by a bounded amount
		// rather than by the number of chunks in view.
		long deadline = System.nanoTime() + FINAL_FLUSH_BUDGET.toNanos();
		for (DirtyChunkTracker.Claim claim : claims) {
			LevelChunk chunk = session.level.getChunkSource().getChunkNow(claim.chunk().x(), claim.chunk().z());
			if (chunk == null) {
				session.droppedCoverage++;
				LOGGER.warn(
						"Final flush could not read dirty chunk {},{}",
						claim.chunk().x(),
						claim.chunk().z());
				continue;
			}
			capture(claim, chunk, trigger, false, Duration.ofNanos(Math.max(0L, deadline - System.nanoTime())));
		}
	}

	private boolean capture(
			DirtyChunkTracker.Claim claim, LevelChunk chunk, String trigger, boolean retryOnFailure) {
		return capture(claim, chunk, trigger, retryOnFailure, Duration.ZERO);
	}

	private boolean capture(
			DirtyChunkTracker.Claim claim,
			LevelChunk chunk,
			String trigger,
			boolean retryOnFailure,
			Duration enqueueBudget) {
		ActiveSession current = session;
		if (current == null || current.level == null) {
			return false;
		}
		try {
			Optional<List<BlockEntityValue>> entitySnapshot = blockEntities.snapshot(claim.chunk());
			CaptureJob job = MinecraftSnapshotReader.snapshot(
					current.serverId,
					current.serverAddress,
					current.contributor,
					current.id,
					current.sequence.reserve(),
					trigger,
					current.level,
					chunk,
					entitySnapshot,
					diagnostic -> LOGGER.warn("Chunk {},{}: {}", claim.chunk().x(), claim.chunk().z(), diagnostic));
			if (enqueue(job, enqueueBudget)) {
				current.enqueued++;
				return true;
			}
			if (retryOnFailure) {
				current.backpressureEvents++;
			} else {
				current.droppedCoverage++;
			}
			if (retryOnFailure) {
				LOGGER.warn("Capture queue full; chunk {},{} remains dirty for retry", claim.chunk().x(), claim.chunk().z());
			} else {
				LOGGER.warn("Capture queue full during final flush; coverage for chunk {},{} was dropped", claim.chunk().x(), claim.chunk().z());
			}
		} catch (RuntimeException exception) {
			current.snapshotFailures++;
			if (!retryOnFailure) {
				current.droppedCoverage++;
			}
			LOGGER.error("Failed to snapshot chunk {},{}", claim.chunk().x(), claim.chunk().z(), exception);
		}
		if (retryOnFailure) {
			dirtyChunks.retry(claim);
		}
		return false;
	}

	private boolean isActiveLevel(ClientLevel level) {
		return session != null && session.level == level;
	}

	private static ChunkCoordinate coordinate(LevelChunk chunk) {
		return new ChunkCoordinate(chunk.getPos().x(), chunk.getPos().z());
	}

	private static BlockEntityNetworkCache.Position position(BlockPos position) {
		return new BlockEntityNetworkCache.Position(position.getX(), position.getY(), position.getZ());
	}

	private static String normalizeServerAddress(String value) {
		ServerAddress parsed = ServerAddress.parseString(value.trim());
		String host = parsed.getHost().toLowerCase(Locale.ROOT);
		if (host.indexOf(':') >= 0 && !host.startsWith("[")) {
			host = '[' + host + ']';
		}
		return host + ':' + parsed.getPort();
	}
}
