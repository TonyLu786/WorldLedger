package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.file.Path;
import org.junit.jupiter.api.Test;

final class CaptureNoticesTest {
	/**
	 * The message a fresh install produces. It is the one a player is most
	 * likely to see and the only thing standing between them and a mod that
	 * silently does nothing, so it has to say where to go and what to change.
	 */
	@Test
	void theDisabledNoticeNamesTheFileAndTheSetting() {
		Path file = Path.of("config", "worldledger", "capture.properties");
		String notice = CaptureNotices.captureDisabled(file);

		assertTrue(notice.startsWith(CaptureNotices.PREFIX), "a player should be able to tell which mod is speaking");
		assertTrue(notice.contains(file.toString()), "the notice must name the file to edit: " + notice);
		assertTrue(notice.contains("contributor"), "the notice must name the setting to change: " + notice);
		assertTrue(notice.contains("restart"), "the change takes effect on restart, so say so: " + notice);
	}

	@Test
	void theStartedNoticeNamesTheContributorAndServer() {
		String notice = CaptureNotices.sessionStarted("alice", "example.org");
		assertTrue(notice.contains("alice"), notice);
		assertTrue(notice.contains("example.org"), notice);
	}

	/**
	 * Dropped coverage means observed state was thrown away. It is the number a
	 * contributor would most want to know and the easiest one to bury in a
	 * total, so it is reported separately.
	 */
	@Test
	void aCleanSessionReportsOnlyWhatWasCaptured() {
		String notice = CaptureNotices.previousSession(158, 0, 0);
		assertTrue(notice.contains("158"), notice);
		assertFalse(notice.contains("dropped"), "nothing was dropped, so the word should not appear: " + notice);
		assertFalse(notice.contains("failed"), "nothing failed, so the word should not appear: " + notice);
	}

	@Test
	void lossIsNamedRatherThanFoldedIntoTheTotal() {
		String notice = CaptureNotices.previousSession(50, 108, 2);
		assertTrue(notice.contains("50"), notice);
		assertTrue(notice.contains("dropped 108"), notice);
		assertTrue(notice.contains("failed on 2"), notice);
	}

	@Test
	void oneChunkReadsAsOneChunk() {
		assertTrue(CaptureNotices.previousSession(1, 0, 0).contains("1 chunk."), "expected singular wording");
		assertTrue(CaptureNotices.previousSession(2, 0, 0).contains("2 chunks"), "expected plural wording");
	}

	/**
	 * A session that captured nothing looks exactly like capture being off
	 * unless the two are worded differently, and the fix for each is different.
	 */
	@Test
	void anEmptySessionIsDistinguishableFromCaptureBeingOff() {
		String empty = CaptureNotices.previousSessionEmpty();
		String off = CaptureNotices.captureDisabled(Path.of("capture.properties"));
		assertFalse(empty.equals(off), "the two states must not read alike");
		assertTrue(empty.contains("nothing"), empty);
	}
}
