package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.List;
import org.junit.jupiter.api.Test;
import org.worldledger.fabric.canonical.NbtValue;

final class BlockEntityNetworkCacheTest {
	private static final NbtValue.CompoundTag EMPTY_NBT = new NbtValue.CompoundTag(List.of());

	@Test
	void unknownIsNeverSubstitutedWithEmpty() {
		BlockEntityNetworkCache cache = new BlockEntityNetworkCache();
		ChunkCoordinate chunk = new ChunkCoordinate(0, 0);
		BlockEntityNetworkCache.NetworkEntry update = new BlockEntityNetworkCache.NetworkEntry(
				new BlockEntityNetworkCache.Position(1, 64, 1), "minecraft:sign", EMPTY_NBT, 1);
		cache.update(update);
		assertTrue(cache.snapshot(chunk).isEmpty());
		cache.replaceBaseline(chunk, List.of());
		assertTrue(cache.snapshot(chunk).isPresent());
		assertTrue(cache.snapshot(chunk).orElseThrow().isEmpty());
	}

	@Test
	void packetUpdatesAndTypeChangesModifyOnlyKnownBaseline() {
		BlockEntityNetworkCache cache = new BlockEntityNetworkCache();
		ChunkCoordinate chunk = new ChunkCoordinate(-1, 2);
		BlockEntityNetworkCache.Position position = new BlockEntityNetworkCache.Position(-1, -20, 33);
		BlockEntityNetworkCache.NetworkEntry sign =
				new BlockEntityNetworkCache.NetworkEntry(position, "minecraft:sign", EMPTY_NBT, 1);
		cache.replaceBaseline(chunk, List.of(sign));
		var snapshot = cache.snapshot(chunk).orElseThrow();
		assertEquals(1, snapshot.size());
		assertEquals(15, snapshot.getFirst().localX());
		assertEquals(1, snapshot.getFirst().localZ());
		cache.invalidateUnlessType(position, "minecraft:sign");
		assertEquals(1, cache.snapshot(chunk).orElseThrow().size());
		cache.invalidateUnlessType(position, "minecraft:banner");
		assertTrue(cache.snapshot(chunk).orElseThrow().isEmpty());
	}

	@Test
	void byteAndEntryLimitsRejectGrowthWithoutPartialMutation() {
		BlockEntityNetworkCache cache = new BlockEntityNetworkCache(2, 10, 15);
		ChunkCoordinate firstChunk = new ChunkCoordinate(0, 0);
		BlockEntityNetworkCache.NetworkEntry first = new BlockEntityNetworkCache.NetworkEntry(
				new BlockEntityNetworkCache.Position(1, 64, 1), "minecraft:sign", EMPTY_NBT, 6);
		cache.replaceBaseline(firstChunk, List.of(first));

		BlockEntityNetworkCache.NetworkEntry tooLarge = new BlockEntityNetworkCache.NetworkEntry(
				new BlockEntityNetworkCache.Position(2, 64, 1), "minecraft:sign", EMPTY_NBT, 5);
		assertThrows(IllegalArgumentException.class, () -> cache.update(tooLarge));
		assertEquals(1, cache.snapshot(firstChunk).orElseThrow().size());

		ChunkCoordinate secondChunk = new ChunkCoordinate(1, 0);
		BlockEntityNetworkCache.NetworkEntry exceedsGlobal = new BlockEntityNetworkCache.NetworkEntry(
				new BlockEntityNetworkCache.Position(17, 64, 1), "minecraft:sign", EMPTY_NBT, 10);
		assertThrows(
				IllegalArgumentException.class,
				() -> cache.replaceBaseline(secondChunk, List.of(exceedsGlobal)));
		assertTrue(cache.snapshot(secondChunk).isEmpty());
	}
}
