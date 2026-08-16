package org.worldledger.fabric;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.file.Path;
import java.util.List;

import org.junit.jupiter.api.Test;

/**
 * A player types this because they want one question answered: is it working.
 * Every case here is about the first line, which is the answer, and about not
 * claiming something that is not true.
 */
final class CaptureStatusTest {
	private static final Path SPOOL = Path.of("/mc/config/worldledger/spool");
	private static final Path CONFIG = Path.of("/mc/config/worldledger/capture.properties");

	private static CaptureStatus off() {
		return new CaptureStatus(false, "", "", 0, 0, 0, 0, SPOOL, CONFIG, "");
	}

	private static CaptureStatus capturing(long captured, int pending, int ready) {
		return new CaptureStatus(true, "alice", "example.org:25565", captured, 0, pending, ready, SPOOL, CONFIG, "");
	}

	private static String first(CaptureStatus status) {
		return status.lines().get(0);
	}

	private static String joined(CaptureStatus status) {
		return String.join("\n", status.lines());
	}

	@Test
	void captureBeingOffIsTheFirstThingSaidAndNamesTheFileThatTurnsItOn() {
		String line = first(off());
		assertTrue(line.contains("off"), line);
		String all = joined(off());
		assertTrue(all.contains(CONFIG.toString()), all);
		assertTrue(all.contains("/worldledger reload"), all);
	}

	@Test
	void aRunningSessionNamesTheContributorAndTheServer() {
		String line = first(capturing(12, 0, 0));
		assertTrue(line.contains("alice"), line);
		assertTrue(line.contains("example.org:25565"), line);
	}

	/**
	 * On before joining is a real state and a confusing one: nothing is being
	 * captured and nothing is wrong. Saying only "capture is on" would leave a
	 * player waiting for a count that cannot arrive yet.
	 */
	@Test
	void beingOnBeforeJoiningSaysWhenItWillStart() {
		CaptureStatus ready = new CaptureStatus(true, "alice", "", 0, 0, 0, 0, SPOOL, CONFIG, "");
		assertFalse(ready.connected());
		String line = first(ready);
		assertTrue(line.contains("join"), line);
	}

	/** A stopped capture must not read as a running one with nothing to do. */
	@Test
	void aStoppedCaptureSaysSoBeforeAnythingElse() {
		CaptureStatus stopped = new CaptureStatus(
				true, "alice", "example.org", 5, 0, 0, 3, SPOOL, CONFIG, "spool is full");
		String line = first(stopped);
		assertTrue(line.startsWith("Capture stopped"), line);
		assertTrue(line.contains("spool is full"), line);
		assertTrue(joined(stopped).contains("Nothing already recorded was deleted"), joined(stopped));
	}

	@Test
	void droppedCoverageIsNamedRatherThanFoldedIntoTheTotal() {
		CaptureStatus dropped = new CaptureStatus(
				true, "alice", "example.org", 40, 3, 0, 0, SPOOL, CONFIG, "");
		assertTrue(joined(dropped).contains("3 dropped"), joined(dropped));
	}

	@Test
	void theSpoolPathIsAlwaysGivenBecauseItIsWhatNobodyCanGuess() {
		for (CaptureStatus status : List.of(off(), capturing(0, 0, 0), capturing(10, 2, 5))) {
			assertTrue(joined(status).contains(SPOOL.toString()), joined(status));
		}
	}

	/**
	 * The import command appears only when there is something to import.
	 * Telling someone to import an empty spool sends them to run a command that
	 * reports nothing and teaches them the tool is noise.
	 */
	@Test
	void theImportCommandAppearsOnlyWhenThereIsSomethingToImport() {
		assertFalse(joined(capturing(0, 0, 0)).contains("ingest-spool"), joined(capturing(0, 0, 0)));
		assertTrue(joined(capturing(0, 0, 4)).contains("ingest-spool"), joined(capturing(0, 0, 4)));
	}

	@Test
	void countsOfOneReadAsOne() {
		String one = joined(capturing(1, 0, 1));
		assertTrue(one.contains("1 chunk captured"), one);
		assertTrue(one.contains("1 bundle in"), one);
		String many = joined(capturing(2, 0, 2));
		assertTrue(many.contains("2 chunks captured"), many);
		assertTrue(many.contains("2 bundles in"), many);
	}

	@Test
	void nullTextIsTreatedAsAbsentRatherThanPrinted() {
		CaptureStatus status = new CaptureStatus(false, null, null, 0, 0, 0, 0, SPOOL, CONFIG, null);
		assertEquals("", status.contributor());
		assertEquals("", status.serverId());
		assertEquals("", status.stopped());
		assertFalse(joined(status).contains("null"), joined(status));
	}
}
