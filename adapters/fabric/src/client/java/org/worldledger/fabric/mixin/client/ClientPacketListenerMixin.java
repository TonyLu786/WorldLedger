package org.worldledger.fabric.mixin.client;

import net.minecraft.client.multiplayer.ClientPacketListener;
import net.minecraft.network.protocol.game.ClientboundBlockEntityDataPacket;
import net.minecraft.network.protocol.game.ClientboundBlockUpdatePacket;
import net.minecraft.network.protocol.game.ClientboundChunksBiomesPacket;
import net.minecraft.network.protocol.game.ClientboundLevelChunkWithLightPacket;
import net.minecraft.network.protocol.game.ClientboundSectionBlocksUpdatePacket;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Inject;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfo;
import org.worldledger.fabric.WorldLedgerRuntime;

@Mixin(ClientPacketListener.class)
abstract class ClientPacketListenerMixin {
	@Inject(method = "handleLevelChunkWithLight", at = @At("TAIL"))
	private void worldledger$afterFullChunk(
			ClientboundLevelChunkWithLightPacket packet, CallbackInfo callback) {
		WorldLedgerRuntime.onFullChunkPacket(packet);
	}

	@Inject(method = "handleBlockUpdate", at = @At("TAIL"))
	private void worldledger$afterBlockUpdate(ClientboundBlockUpdatePacket packet, CallbackInfo callback) {
		WorldLedgerRuntime.onBlockUpdate(packet);
	}

	@Inject(method = "handleChunkBlocksUpdate", at = @At("TAIL"))
	private void worldledger$afterSectionBlocksUpdate(
			ClientboundSectionBlocksUpdatePacket packet, CallbackInfo callback) {
		WorldLedgerRuntime.onSectionBlocksUpdate(packet);
	}

	@Inject(method = "handleBlockEntityData", at = @At("TAIL"))
	private void worldledger$afterBlockEntityData(
			ClientboundBlockEntityDataPacket packet, CallbackInfo callback) {
		WorldLedgerRuntime.onBlockEntityData(packet);
	}

	@Inject(method = "handleChunksBiomes", at = @At("TAIL"))
	private void worldledger$afterBiomeUpdate(
			ClientboundChunksBiomesPacket packet, CallbackInfo callback) {
		WorldLedgerRuntime.onBiomeUpdate(packet);
	}
}
