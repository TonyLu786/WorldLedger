package org.worldledger.fabric;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/**
 * What capture is doing, in the words a player would use.
 *
 * <p>Until now the only way to find out was to join a server and read the one
 * line that appears in chat, or to open the log. Neither answers the question
 * while you are standing there wondering whether the mod is working.
 *
 * <p>This is plain data with no Minecraft types, like {@link CaptureNotices},
 * so what a player is shown can be tested without a client. The command that
 * puts it on screen is deliberately thin for the same reason.
 *
 * @param enabled            whether a contributor is set, which is what decides
 *                           if anything is captured at all
 * @param contributor        the name observations are recorded under
 * @param serverId           the archive id for the current connection, empty
 *                           when not connected to a multiplayer server
 * @param capturedThisSession chunks handed to the writer so far
 * @param droppedThisSession  chunks whose coverage was lost, which is the number
 *                            worth knowing and the easiest one to hide
 * @param pendingWrites       chunks still queued for the writer thread
 * @param readyBundles        bundles sitting in the spool waiting to be imported
 * @param spoolDirectory      where those bundles are
 * @param configurationFile   the file that turns capture on
 * @param stopped             why capture stopped, empty if it has not
 */
public record CaptureStatus(
		boolean enabled,
		String contributor,
		String serverId,
		long capturedThisSession,
		long droppedThisSession,
		int pendingWrites,
		int readyBundles,
		Path spoolDirectory,
		Path configurationFile,
		String stopped) {

	public CaptureStatus {
		contributor = contributor == null ? "" : contributor;
		serverId = serverId == null ? "" : serverId;
		stopped = stopped == null ? "" : stopped;
	}

	public boolean connected() {
		return !serverId.isEmpty();
	}

	/**
	 * The status as lines to print, most important first.
	 *
	 * <p>The first line always answers "is it on", because that is the question,
	 * and every other line is only interesting once it is answered.
	 */
	public List<String> lines() {
		List<String> out = new ArrayList<>();
		if (!stopped.isEmpty()) {
			out.add("Capture stopped: " + stopped);
			out.add("Nothing already recorded was deleted. Import the spool and restart the client.");
		} else if (!enabled) {
			out.add("Capture is off: no contributor is set.");
			out.add("Set contributor= in " + configurationFile + ", then run /worldledger reload.");
		} else if (connected()) {
			out.add("Capturing as " + contributor + " on " + serverId + ".");
		} else {
			out.add("Capture is on as " + contributor + ", and starts when you join a multiplayer server.");
		}

		if (connected() || capturedThisSession > 0) {
			StringBuilder session = new StringBuilder(String.format(Locale.ROOT,
					"This session: %d chunk%s captured", capturedThisSession, capturedThisSession == 1 ? "" : "s"));
			if (pendingWrites > 0) {
				session.append(String.format(Locale.ROOT, ", %d waiting to be written", pendingWrites));
			}
			if (droppedThisSession > 0) {
				session.append(String.format(Locale.ROOT, ", %d dropped", droppedThisSession));
			}
			out.add(session.append('.').toString());
		}

		out.add(String.format(Locale.ROOT, "Spool: %d bundle%s in %s",
				readyBundles, readyBundles == 1 ? "" : "s", spoolDirectory));
		if (readyBundles > 0) {
			// The command that turns a spool into a world, with the one path a
			// player could not have guessed already filled in.
			out.add("Import them with:");
			out.add("  worldledger ingest-spool --archive ./archive " + spoolDirectory);
		}
		return out;
	}
}
