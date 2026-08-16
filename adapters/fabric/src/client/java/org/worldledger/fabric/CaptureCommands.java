package org.worldledger.fabric;

import com.mojang.brigadier.CommandDispatcher;
import net.fabricmc.fabric.api.client.command.v2.ClientCommands;
import net.fabricmc.fabric.api.client.command.v2.ClientCommandRegistrationCallback;
import net.fabricmc.fabric.api.client.command.v2.FabricClientCommandSource;
import net.minecraft.ChatFormatting;
import net.minecraft.network.chat.Component;

/**
 * The in-game answer to "is this thing working".
 *
 * <p>Everything the adapter did was previously reported in one chat line on
 * join and then never again, which is no use to a player who wants to know now.
 * These are client commands: they are handled locally, run on any server
 * including one that has never heard of this mod, and need no permission.
 *
 * <p>The class stays thin on purpose. What a player is told lives in
 * {@link CaptureStatus} and {@link CaptureNotices}, which have no Minecraft
 * types and are tested without a client.
 */
public final class CaptureCommands {
	private CaptureCommands() {}

	public static void register() {
		ClientCommandRegistrationCallback.EVENT.register(
				(dispatcher, registry) -> register(dispatcher));
	}

	static void register(CommandDispatcher<FabricClientCommandSource> dispatcher) {
		dispatcher.register(ClientCommands.literal("worldledger")
				// Bare /worldledger answers the question people actually have,
				// rather than telling them to pick a subcommand first.
				.executes(context -> status(context.getSource()))
				.then(ClientCommands.literal("status")
						.executes(context -> status(context.getSource())))
				.then(ClientCommands.literal("spool")
						.executes(context -> spool(context.getSource())))
				.then(ClientCommands.literal("reload")
						.executes(context -> reload(context.getSource()))));
	}

	private static int status(FabricClientCommandSource source) {
		CaptureStatus status = WorldLedgerRuntime.status();
		if (status == null) {
			source.sendFeedback(grey(CaptureNotices.PREFIX + "Capture has not finished starting up."));
			return 0;
		}
		boolean first = true;
		for (String line : status.lines()) {
			source.sendFeedback(first ? highlight(status, line) : grey(line));
			first = false;
		}
		return 1;
	}

	private static int spool(FabricClientCommandSource source) {
		CaptureStatus status = WorldLedgerRuntime.status();
		if (status == null) {
			source.sendFeedback(grey(CaptureNotices.PREFIX + "Capture has not finished starting up."));
			return 0;
		}
		source.sendFeedback(grey(CaptureNotices.whereCapturesWent(status.spoolDirectory())));
		return 1;
	}

	private static int reload(FabricClientCommandSource source) {
		String message = WorldLedgerRuntime.reload();
		if (message == null) {
			source.sendFeedback(grey(CaptureNotices.PREFIX + "Capture has not finished starting up."));
			return 0;
		}
		source.sendFeedback(grey(message));
		return 1;
	}

	/**
	 * The first line is the answer, so it is the one that gets a colour: red
	 * when capture is not running, green when it is.
	 */
	private static Component highlight(CaptureStatus status, String line) {
		ChatFormatting colour = status.enabled() && status.stopped().isEmpty()
				? ChatFormatting.GREEN
				: ChatFormatting.RED;
		return Component.literal(line).withStyle(colour);
	}

	private static Component grey(String line) {
		return Component.literal(line).withStyle(ChatFormatting.GRAY);
	}
}
