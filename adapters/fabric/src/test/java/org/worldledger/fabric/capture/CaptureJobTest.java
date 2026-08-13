package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;
import static org.junit.jupiter.api.Assertions.assertThrows;

import org.junit.jupiter.api.Test;

final class CaptureJobTest {
	@Test
	void sectionShapeIsRejectedBeforeSnapshotAllocationCanGrowWithoutBound() {
		assertDoesNotThrow(() -> CaptureJob.validateSectionShape(-64, 24));
		assertThrows(IllegalArgumentException.class, () -> CaptureJob.validateSectionShape(0, 0));
		assertThrows(IllegalArgumentException.class, () -> CaptureJob.validateSectionShape(0, 128));
		assertThrows(
				IllegalArgumentException.class,
				() -> CaptureJob.validateSectionShape(Integer.MAX_VALUE, 2));
	}
}
