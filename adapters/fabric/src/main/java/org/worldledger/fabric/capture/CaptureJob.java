package org.worldledger.fabric.capture;

import java.time.Instant;
import java.util.List;
import java.util.Objects;
import java.util.Optional;

import org.worldledger.fabric.canonical.BlockEntityValue;
import org.worldledger.fabric.canonical.BlockStateValue;
import org.worldledger.fabric.canonical.MinecraftJavaV1;

public record CaptureJob(
		String serverId,
		String serverAddress,
		String dimension,
		ChunkCoordinate chunk,
		Instant observedAt,
		String contributor,
		String sessionId,
		long sequence,
		String trigger,
		int minSectionY,
		int sectionCount,
		List<SectionSnapshot> sections,
		Optional<List<BlockEntityValue>> blockEntities) {
	public static final String PROTOCOL =
			"minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1";
	public static final String AGENT = "worldledger-fabric/0.1.0-dev";
	// shape + two section components + block entities must fit the 256-component bundle limit.
	private static final int MAX_SECTION_COUNT = 127;
	private static final int MAX_BLOCK_ENTITIES = 4096;

	public CaptureJob {
		Objects.requireNonNull(serverId, "serverId");
		Objects.requireNonNull(serverAddress, "serverAddress");
		Objects.requireNonNull(dimension, "dimension");
		Objects.requireNonNull(chunk, "chunk");
		Objects.requireNonNull(observedAt, "observedAt");
		Objects.requireNonNull(contributor, "contributor");
		Objects.requireNonNull(sessionId, "sessionId");
		Objects.requireNonNull(trigger, "trigger");
		sections = List.copyOf(Objects.requireNonNull(sections, "sections"));
		blockEntities = Objects.requireNonNull(blockEntities, "blockEntities")
				.map(List::copyOf);
		if (serverId.isBlank() || dimension.isBlank() || contributor.isBlank() || sessionId.isBlank()) {
			throw new IllegalArgumentException("capture identity fields must not be blank");
		}
		if (sequence < 0) {
			throw new IllegalArgumentException("capture sequence must be non-negative");
		}
		validateSectionShape(minSectionY, sectionCount);
		if (sections.size() != sectionCount) {
			throw new IllegalArgumentException("section snapshots do not match the declared count");
		}
		if (blockEntities.isPresent() && blockEntities.orElseThrow().size() > MAX_BLOCK_ENTITIES) {
			throw new IllegalArgumentException("block-entity snapshot exceeds the entry limit");
		}
		for (int index = 0; index < sections.size(); index++) {
			if (sections.get(index).sectionY() != minSectionY + index) {
				throw new IllegalArgumentException("section snapshots do not match the declared shape");
			}
		}
	}

	public static void validateSectionShape(int minSectionY, int sectionCount) {
		if (sectionCount <= 0
				|| sectionCount > MAX_SECTION_COUNT
				|| (long) minSectionY + sectionCount - 1 > Integer.MAX_VALUE) {
			throw new IllegalArgumentException("section shape exceeds capture limits");
		}
	}

	public record SectionSnapshot(
			int sectionY,
			Optional<List<BlockStateValue>> blocks,
			Optional<List<String>> biomes) {
		public SectionSnapshot {
			blocks = Objects.requireNonNull(blocks, "blocks").map(List::copyOf);
			biomes = Objects.requireNonNull(biomes, "biomes").map(List::copyOf);
			if (blocks.isPresent() && blocks.orElseThrow().size() != MinecraftJavaV1.BLOCK_COUNT) {
				throw new IllegalArgumentException("block snapshot has the wrong section size");
			}
			if (biomes.isPresent() && biomes.orElseThrow().size() != MinecraftJavaV1.BIOME_COUNT) {
				throw new IllegalArgumentException("biome snapshot has the wrong section size");
			}
		}
	}
}
