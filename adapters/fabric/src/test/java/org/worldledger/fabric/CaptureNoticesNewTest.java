package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.file.Path;

import org.junit.jupiter.api.Test;

/**
 * The two notices added so that a successful session is not a dead end, and so
 * that a reload does not promise more than it does.
 */
final class CaptureNoticesNewTest {
	private static final Path SPOOL = Path.of("/mc/config/worldledger/spool");
	private static final Path CONFIG = Path.of("/mc/config/worldledger/capture.properties");

	/**
	 * Before this, the spool path only appeared when the spool was full, so the
	 * ordinary outcome of a good session was a number and no way to act on it.
	 */
	@Test
	void aSuccessfulSessionSaysWhereTheChunksWentAndWhatToRun() {
		String text = CaptureNotices.whereCapturesWent(SPOOL);
		assertTrue(text.contains(SPOOL.toString()), text);
		assertTrue(text.contains("ingest-spool"), text);
	}

	/**
	 * Two settings are consumed when capture starts and a reload cannot change
	 * them. Not saying so would make the next confusing thing a player's fault.
	 */
	@Test
	void reloadNamesTheSettingsItCannotChange() {
		for (String text : new String[] {
				CaptureNotices.reloaded("alice"), CaptureNotices.reloaded("")}) {
			assertTrue(text.contains("coalesce_ticks"), text);
			assertTrue(text.contains("queue_capacity"), text);
			assertTrue(text.contains("restart"), text);
		}
	}

	@Test
	void reloadingWithNoContributorSaysCaptureStaysOff() {
		String text = CaptureNotices.reloaded("");
		assertTrue(text.contains("stays off"), text);
		assertFalse(text.contains("Capturing as"), text);
	}

	@Test
	void aFailedReloadSaysTheOldSettingsAreStillInUse() {
		String text = CaptureNotices.reloadFailed(CONFIG, "denied");
		assertTrue(text.contains(CONFIG.toString()), text);
		assertTrue(text.contains("denied"), text);
		assertTrue(text.contains("still in use"), text);
	}
}
