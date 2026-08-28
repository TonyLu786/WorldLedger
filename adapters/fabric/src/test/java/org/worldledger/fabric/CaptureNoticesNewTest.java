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
				CaptureNotices.reloaded("alice", false), CaptureNotices.reloaded("", false),
				CaptureNotices.reloaded("alice", true), CaptureNotices.reloaded("", true)}) {
			assertTrue(text.contains("coalesce_ticks"), text);
			assertTrue(text.contains("queue_capacity"), text);
			assertTrue(text.contains("restart"), text);
		}
	}

	@Test
	void reloadingWithNoContributorSaysCaptureStaysOff() {
		String text = CaptureNotices.reloaded("", false);
		assertTrue(text.contains("stays off"), text);
		assertFalse(text.contains("Capturing as"), text);
	}

	/**
	 * The sentence a player reads when they have just decided they do not want
	 * this server recorded. It used to say capture was off while the running
	 * session carried on under the name it took at join.
	 */
	@Test
	void blankingTheNameDuringASessionSaysRecordingHasStopped() {
		String text = CaptureNotices.reloaded("", true);
		assertTrue(text.contains("stopped"), text);
		assertFalse(text.contains("stays off"), text);
		// What was recorded before they changed their mind is not thrown away,
		// and somebody deciding to stop should not have to wonder.
		assertTrue(text.contains("already recorded is kept"), text);
	}

	/**
	 * The other half of the same truth: a name changed mid-session does not
	 * apply to the session, because the session took its copy when it started.
	 */
	@Test
	void changingTheNameDuringASessionSaysWhenItApplies() {
		String text = CaptureNotices.reloaded("bob", true);
		assertTrue(text.contains("still being recorded"), text);
		assertTrue(text.contains("next server you join"), text);
	}

	@Test
	void aFailedReloadSaysTheOldSettingsAreStillInUse() {
		String text = CaptureNotices.reloadFailed(CONFIG, "denied");
		assertTrue(text.contains(CONFIG.toString()), text);
		assertTrue(text.contains("denied"), text);
		assertTrue(text.contains("still in use"), text);
	}
}
