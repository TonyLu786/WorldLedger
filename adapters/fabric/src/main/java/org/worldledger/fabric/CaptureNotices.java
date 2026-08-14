package org.worldledger.fabric;

import java.nio.file.Path;

/**
 * The text the adapter shows a player in game.
 *
 * <p>Capture is silent by design: it writes to a spool and never touches the
 * world. That is correct behaviour and a terrible first experience, because a
 * player who installs the mod and joins a server has no way to tell whether
 * anything is happening. The most common outcome is the worst one, since
 * capture stays disabled until a contributor is set, and until now the only
 * record of that was a line in the log.
 *
 * <p>These are plain strings with no Minecraft types, so what a player is told
 * can be tested without a client. The class that puts them on screen is
 * deliberately thin for the same reason.
 */
public final class CaptureNotices {
	/** Prefix so a player can tell which mod is speaking. */
	public static final String PREFIX = "[WorldLedger] ";

	private CaptureNotices() {}

	/**
	 * Shown when the contributor is blank, which is how a fresh install
	 * arrives. It names the file and the exact edit, because "capture is
	 * disabled" without either is a dead end.
	 */
	public static String captureDisabled(Path configurationFile) {
		return PREFIX + "Capture is off. Set contributor= in " + configurationFile
				+ " and restart the client.";
	}

	/** Shown once per session so a player knows it is on and under whose name. */
	public static String sessionStarted(String contributor, String serverId) {
		return PREFIX + "Capturing as " + contributor + " on " + serverId + ".";
	}

	/**
	 * Shown when a connection has no address the archive can key on. Rare, and
	 * confusing enough without an explanation that it is worth one.
	 */
	public static String serverAddressUnavailable() {
		return PREFIX + "Capture is off for this connection: it has no usable server address.";
	}

	/**
	 * Shown on the next join, because a disconnect leaves no screen to write to.
	 *
	 * <p>Dropped coverage is named rather than folded into the total. It means
	 * observed state was thrown away, which is the one number a contributor
	 * would want to know about and the one most easily hidden.
	 */
	public static String previousSession(long captured, long dropped, long failures) {
		StringBuilder text = new StringBuilder(PREFIX)
				.append("Last session captured ")
				.append(captured)
				.append(captured == 1 ? " chunk" : " chunks");
		if (dropped > 0) {
			text.append(", dropped ").append(dropped);
		}
		if (failures > 0) {
			text.append(", failed on ").append(failures);
		}
		return text.append('.').toString();
	}

	/**
	 * Shown when a session produced nothing, which is not the same as capture
	 * being off and would otherwise look identical to it.
	 */
	public static String previousSessionEmpty() {
		return PREFIX + "Last session captured nothing. No chunk stayed loaded long enough to snapshot.";
	}
}
