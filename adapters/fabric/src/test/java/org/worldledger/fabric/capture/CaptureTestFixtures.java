package org.worldledger.fabric.capture;

import java.time.Instant;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

import org.worldledger.fabric.canonical.BlockStateValue;

final class CaptureTestFixtures {
	private CaptureTestFixtures() {}

	static CaptureJob job(long sequence) {
		List<BlockStateValue> blocks = Collections.nCopies(16 * 16 * 16, BlockStateValue.simple("minecraft:air"));
		List<String> biomes = Collections.nCopies(4 * 4 * 4, "minecraft:plains");
		return new CaptureJob(
				"example.org:25565",
				"example.org:25565",
				"minecraft:overworld",
				new ChunkCoordinate(14, -8),
				Instant.parse("2026-08-09T12:00:03.123456Z"),
				"fixture-contributor",
				"5dfe3db2-208e-4cd8-8d11-1d83fa4f951b",
				sequence,
				"dirty-flush",
				-4,
				1,
				List.of(new CaptureJob.SectionSnapshot(-4, Optional.of(blocks), Optional.of(biomes))),
				Optional.of(List.of()));
	}
}
