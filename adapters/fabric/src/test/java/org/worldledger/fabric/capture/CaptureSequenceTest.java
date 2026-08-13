package org.worldledger.fabric.capture;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import org.junit.jupiter.api.Test;

final class CaptureSequenceTest {
	@Test
	void reservesStrictlyIncreasingValues() {
		CaptureSequence sequence = new CaptureSequence();
		assertEquals(1, sequence.reserve());
		assertEquals(2, sequence.reserve());
		assertEquals(3, sequence.reserve());
	}

	@Test
	void refusesToWrapTheSignedManifestSequence() {
		CaptureSequence sequence = new CaptureSequence(Long.MAX_VALUE - 1);
		assertEquals(Long.MAX_VALUE - 1, sequence.reserve());
		assertThrows(IllegalStateException.class, sequence::reserve);
	}
}
