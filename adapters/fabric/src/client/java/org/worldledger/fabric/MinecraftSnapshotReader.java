package org.worldledger.fabric;

import java.time.Instant;
import java.util.ArrayList;
import java.util.Collections;
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
import net.minecraft.world.level.chunk.PalettedContainerRO;
import org.worldledger.fabric.canonical.BlockEntityValue;
import org.worldledger.fabric.canonical.BlockStateValue;
import org.worldledger.fabric.canonical.MinecraftJavaV1;
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

		Caches caches = new Caches();
		List<CaptureJob.SectionSnapshot> sections = new ArrayList<>(sectionCount);
		for (int index = 0; index < sectionCount; index++) {
			int sectionY = minSectionY + index;
			LevelChunkSection section = rawSections[index];
			Optional<List<BlockStateValue>> blocks;
			try {
				blocks = Optional.of(snapshotBlocks(section, caches));
			} catch (RuntimeException exception) {
				diagnostic.accept("omitting mcjava.blocks." + sectionY + ": " + exception.getMessage());
				blocks = Optional.empty();
			}
			Optional<List<String>> biomes;
			try {
				biomes = Optional.of(snapshotBiomes(section, caches));
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

	/**
	 * What one chunk's sections have in common, so that the same answer is not
	 * worked out once per section.
	 *
	 * <p>The uniform maps hold whole sections rather than single values. Most of
	 * a chunk is one state repeated: everything above the terrain is air, and a
	 * chunk carries 24 sections. Building that list once and handing the same
	 * instance to every section that needs it is the difference between 1.2 MB of
	 * garbage per captured chunk and 233 KB, on the client thread, in the part of
	 * a tick a player can feel.
	 */
	private static final class Caches {
		private final Map<BlockState, BlockStateValue> blockStates = new IdentityHashMap<>();
		private final Map<BlockState, List<BlockStateValue>> uniformBlockSections = new IdentityHashMap<>();
		private final Map<Holder<Biome>, String> biomes = new IdentityHashMap<>();
		private final Map<Holder<Biome>, List<String>> uniformBiomeSections = new IdentityHashMap<>();
	}

	private static List<BlockStateValue> snapshotBlocks(LevelChunkSection section, Caches caches) {
		PalettedContainerRO<BlockState> states = section.getStates();
		// A container with no bits per entry has a single-value palette: every
		// position in the section resolves to that one state, so reading all 4096
		// of them asks the same question 4096 times. The encoder is handed the
		// same 4096 values in the same order either way, which is all its output
		// depends on.
		//
		// hasOnlyAir() looks like it would serve here and does not: cave_air and
		// void_air both report as air while canonicalizing to different strings,
		// so a section it accepts is not necessarily a section of minecraft:air.
		if (states.bitsPerEntry() == 0) {
			return caches.uniformBlockSections.computeIfAbsent(states.get(0, 0, 0), state ->
					List.copyOf(Collections.nCopies(
							MinecraftJavaV1.BLOCK_COUNT,
							caches.blockStates.computeIfAbsent(state, MinecraftSnapshotReader::convertBlockState))));
		}
		List<BlockStateValue> result = new ArrayList<>(16 * 16 * 16);
		for (int y = 0; y < 16; y++) {
			for (int z = 0; z < 16; z++) {
				for (int x = 0; x < 16; x++) {
					BlockState state = section.getBlockState(x, y, z);
					result.add(caches.blockStates.computeIfAbsent(state, MinecraftSnapshotReader::convertBlockState));
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

	private static List<String> snapshotBiomes(LevelChunkSection section, Caches caches) {
		PalettedContainerRO<Holder<Biome>> biomes = section.getBiomes();
		// Same shape as the block path, and a section of one biome is the ordinary
		// case rather than the exception.
		if (biomes.bitsPerEntry() == 0) {
			return caches.uniformBiomeSections.computeIfAbsent(biomes.get(0, 0, 0), biome ->
					List.copyOf(Collections.nCopies(
							MinecraftJavaV1.BIOME_COUNT,
							caches.biomes.computeIfAbsent(biome, MinecraftSnapshotReader::biomeIdentifier))));
		}
		List<String> result = new ArrayList<>(4 * 4 * 4);
		for (int y = 0; y < 4; y++) {
			for (int z = 0; z < 4; z++) {
				for (int x = 0; x < 4; x++) {
					Holder<Biome> biome = section.getNoiseBiome(x, y, z);
					result.add(caches.biomes.computeIfAbsent(biome, MinecraftSnapshotReader::biomeIdentifier));
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
