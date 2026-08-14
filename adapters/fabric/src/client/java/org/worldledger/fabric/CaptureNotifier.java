package org.worldledger.fabric;

import net.minecraft.ChatFormatting;
import net.minecraft.client.Minecraft;
import net.minecraft.network.chat.Component;

/**
 * Puts a line of text in the player's chat.
 *
 * <p>Deliberately the thinnest possible layer over the client. Everything worth
 * testing about what a player is told lives in {@link CaptureNotices}, which
 * needs no Minecraft at all; this class only decides colour and survives a
 * client that is not ready to draw yet.
 */
final class CaptureNotifier {
	private CaptureNotifier() {}

	/** Something the player probably wants to act on. */
	static void warn(Minecraft client, String text) {
		send(client, text, ChatFormatting.YELLOW);
	}

	/** Confirmation. Grey so a working session does not read as an alert. */
	static void info(Minecraft client, String text) {
		send(client, text, ChatFormatting.GRAY);
	}

	private static void send(Minecraft client, String text, ChatFormatting colour) {
		if (client == null || client.gui == null) {
			return;
		}
		// Capture must never be the reason a client fails. A notice that cannot
		// be drawn is not worth propagating an exception for.
		try {
			client.gui.chatListener().handleSystemMessage(Component.literal(text).withStyle(colour), false);
		} catch (RuntimeException ignored) {
			// The log already carries every one of these lines.
		}
	}
}
