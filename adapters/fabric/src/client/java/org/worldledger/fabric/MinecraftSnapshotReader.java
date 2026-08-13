package org.worldledger.fabric;

import java.time.Instant;
import java.util.ArrayList;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.function.Consumer;

import net.minecraft.client.multiplayer.ClientLevel;
import net.minecraft.core.Holder;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.chunk.LevelChunk;
import net.minecraft.world.level.chunk.LevelChunkSection;
import org.worldledger.fabric.canonical.BlockEntityValue;
import org.worldledger.fabric.canonical.BlockStateValue;
import org.worldledger.fabric.canonical.StateProperty;
import org.worldledger.fabric.capture.CaptureJob;
import org.worldledger.fabric.capture.ChunkCoordinate;

final class MinecraftSnapshotReader {
	private MinecraftSnapshotReader() {}

	static CaptureJob snapshot(
			String serverId,
			String serverAddress,
			String contributor,
			String sessionId,
			long sequence,
			String trigger,
			ClientLevel level,
			LevelChunk chunk,
			Optional<List<BlockEntityValue>> blockEntities,
			Consumer<String> diagnostic) {
		int minSectionY = level.getMinSectionY();
		int sectionCount = level.getSectionsCount();
		CaptureJob.validateSectionShape(minSectionY, sectionCount);
		LevelChunkSection[] rawSections = chunk.getSections();
		if (rawSections.length != sectionCount) {
			throw new IllegalStateException(
					"chunk section count " + rawSections.length + " differs from level shape " + sectionCount);
		}

		Map<BlockState, BlockStateValue> blockStateCache = new IdentityHashMap<>();
		Map<Holder<Biome>, String> biomeCache = new IdentityHashMap<>();
		List<CaptureJob.SectionSnapshot> sections = new ArrayList<>(sectionCount);
		for (int index = 0; index < sectionCount; index++) {
			int sectionY = minSectionY + index;
			LevelChunkSection section = rawSections[index];
			Optional<List<BlockStateValue>> blocks;
			try {
				blocks = Optional.of(snapshotBlocks(section, blockStateCache));
			} catch (RuntimeException exception) {
				diagnostic.accept("omitting mcjava.blocks." + sectionY + ": " + exception.getMessage());
				blocks = Optional.empty();
			}
			Optional<List<String>> biomes;
			try {
				biomes = Optional.of(snapshotBiomes(section, biomeCache));
			} catch (RuntimeException exception) {
				diagnostic.accept("omitting mcjava.biomes." + sectionY + ": " + exception.getMessage());
				biomes = Optional.empty();
			}
			sections.add(new CaptureJob.SectionSnapshot(sectionY, blocks, biomes));
		}

		String dimension = level.dimension().identifier().toString();
		return new CaptureJob(
				serverId,
				serverAddress,
				dimension,
				new ChunkCoordinate(chunk.getPos().x(), chunk.getPos().z()),
				Instant.now(),
				contributor,
				sessionId,
				sequence,
				trigger,
				minSectionY,
				sectionCount,
				sections,
				blockEntities);
	}

	private static List<BlockStateValue> snapshotBlocks(
			LevelChunkSection section, Map<BlockState, BlockStateValue> cache) {
		List<BlockStateValue> result = new ArrayList<>(16 * 16 * 16);
		for (int y = 0; y < 16; y++) {
			for (int z = 0; z < 16; z++) {
				for (int x = 0; x < 16; x++) {
					BlockState state = section.getBlockState(x, y, z);
					result.add(cache.computeIfAbsent(state, MinecraftSnapshotReader::convertBlockState));
				}
			}
		}
		return List.copyOf(result);
	}

	private static BlockStateValue convertBlockState(BlockState state) {
		var identifier = BuiltInRegistries.BLOCK.getKey(state.getBlock());
		if (identifier == null) {
			throw new IllegalArgumentException("block has no stable registry identifier");
		}
		List<StateProperty> properties = state.getValues()
				.map(MinecraftSnapshotReader::convertProperty)
				.toList();
		return new BlockStateValue(identifier.toString(), properties);
	}

	private static StateProperty convertProperty(Property.Value<?> value) {
		return new StateProperty(value.property().getName(), value.valueName());
	}

	private static List<String> snapshotBiomes(
			LevelChunkSection section, Map<Holder<Biome>, String> cache) {
		List<String> result = new ArrayList<>(4 * 4 * 4);
		for (int y = 0; y < 4; y++) {
			for (int z = 0; z < 4; z++) {
				for (int x = 0; x < 4; x++) {
					Holder<Biome> biome = section.getNoiseBiome(x, y, z);
					result.add(cache.computeIfAbsent(biome, MinecraftSnapshotReader::biomeIdentifier));
				}
			}
		}
		return List.copyOf(result);
	}

	private static String biomeIdentifier(Holder<Biome> biome) {
		ResourceKey<Biome> key = biome.unwrapKey()
				.orElseThrow(() -> new IllegalArgumentException("biome has no stable registry identifier"));
		return key.identifier().toString();
	}
}
