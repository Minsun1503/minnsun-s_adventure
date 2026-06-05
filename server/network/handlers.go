package network

import (
	"fmt"
	"net"
	"server/ecs"
	"server/game"
	"server/logger"
	"server/peakgo/broadcast"
	"server/peakgo/codec"
	"server/peakgo/netio"
	"server/protocol"
	"server/systems"
	"server/world"
	"time"
)

// ─── Movement ─────────────────────────────────────────────────────────────────

func handleMove(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	errMsg, ok := game.HandlePlayerMovementSystem(playerEntity, payload)
	if !ok {
		systems.SendNoticeSystem(playerEntity, []byte(errMsg))
	}
}

func handleInventory(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 0 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: INV packet payload must be empty.\r\n"))
		return
	}
	inventoryTextPacket := game.RunInventoryQuerySystem(playerEntity)
	frame := broadcast.BuildNotice(broadcast.NoticePayload{Message: inventoryTextPacket})
	conn.Write(frame)
}

func handleUseItem(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	noticePacket, success := game.HandleItemUsageSystem(playerEntity, payload)
	if success {
		systems.BroadcastSystem([]byte(noticePacket))
	} else {
		systems.SendNoticeSystem(playerEntity, []byte(noticePacket))
	}
}

func handleWarp(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	systemFeedback, _ := world.HandleWarpSystem(playerEntity, payload)
	systems.SendNoticeSystem(playerEntity, []byte(systemFeedback))
}

func handleAttack(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	atk, ok := codec.ReadAttackPayload(payload)
	if !ok {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid ATTACK payload length. Expected 8 bytes.\r\n"))
		return
	}
	targetID := ecs.Entity(atk.TargetID)

	result, errMsg := game.AttackSystem(playerEntity, targetID)
	if errMsg != "" {
		systems.SendNoticeSystem(playerEntity, []byte(errMsg))
		return
	}

	if result.Killed {
		systems.SendNoticeSystem(playerEntity,
			[]byte(fmt.Sprintf("You killed %s!\r\n", result.TargetName)),
		)
	} else {
		systems.SendNoticeSystem(playerEntity,
			[]byte(fmt.Sprintf(
				"You hit %s for %d damage. %s has %d HP remaining.\r\n",
				result.TargetName, result.Damage,
				result.TargetName, result.TargetHP,
			)),
		)
	}
}

func handleInfo(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 8 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid INFO payload length. Expected 8 bytes.\r\n"))
		return
	}
	targetID := ecs.Entity(codec.ReadUint64(payload[0:8]))
	text, err := systems.GetInfoSystem(targetID)
	if err != nil {
		systems.SendNoticeSystem(playerEntity, []byte("Entity not found.\r\n"))
		return
	}
	systems.SendNoticeSystem(playerEntity, []byte(text))
}

func handleQuit(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 0 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: QUIT packet payload must be empty.\r\n"))
		return
	}
	logger.Info("[QUIT] %s requested graceful disconnect.", conn.RemoteAddr())
	conn.Close()
}

func handlePickup(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 8 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PICKUP payload length. Expected 8 bytes.\r\n"))
		return
	}
	itemEntityID := ecs.Entity(codec.ReadUint64(payload[0:8]))
	personalFeedback, _ := game.HandleItemPickupSystem(playerEntity, itemEntityID)
	systems.SendNoticeSystem(playerEntity, []byte(personalFeedback))
}

func handleEquip(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 8 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid EQUIP payload length. Expected 8 bytes.\r\n"))
		return
	}
	itemID := codec.ReadUint64(payload[0:8])
	feedback, success := game.HandleEquipmentSystem(playerEntity, itemID)
	if success {
		pos, _ := ecs.DefaultRegistry.GetPosition(playerEntity)
		frame := broadcast.BuildNotice(broadcast.NoticePayload{Message: feedback})
		protocol.BroadcastToNeighbors(pos, frame, playerEntity)
	} else {
		systems.SendNoticeSystem(playerEntity, []byte(feedback))
	}
}

// ─── Social ───────────────────────────────────────────────────────────────────

func handlePartyCreate(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) < 1 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PARTY CREATE payload.\r\n"))
		return
	}
	teamNameLen := int(payload[0])
	if teamNameLen == 0 || teamNameLen > len(payload)-1 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid team name length.\r\n"))
		return
	}
	teamName := string(payload[1 : 1+teamNameLen])
	response := game.CreatePartySystem(playerEntity, teamName)
	systems.SendNoticeSystem(playerEntity, []byte(response))
}

func handlePartyInvite(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 8 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PARTY INVITE payload length. Expected 8 bytes.\r\n"))
		return
	}
	targetID := ecs.Entity(codec.ReadUint64(payload[0:8]))
	response, _ := game.SendPartyInviteSystem(playerEntity, targetID)
	systems.SendNoticeSystem(playerEntity, []byte(response))
}

func handlePartyJoin(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 8 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PARTY JOIN payload length. Expected 8 bytes.\r\n"))
		return
	}
	partyID := ecs.Entity(codec.ReadUint64(payload[0:8]))
	response, _ := game.AcceptPartyInviteSystem(playerEntity, partyID)
	systems.SendNoticeSystem(playerEntity, []byte(response))
}

// ─── Trade ────────────────────────────────────────────────────────────────────

func handleTradeInit(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 8 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid TRADE INIT payload length. Expected 8 bytes.\r\n"))
		return
	}
	targetID := ecs.Entity(codec.ReadUint64(payload[0:8]))
	response, _ := game.GlobalTradeRegistry.InitializeTradeSession(playerEntity, targetID)
	systems.SendNoticeSystem(playerEntity, []byte(response))
}

func handleTradeOffer(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 12 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid TRADE OFFER payload length. Expected 12 bytes.\r\n"))
		return
	}
	itemID := codec.ReadUint64(payload[0:8])
	qty := int(codec.ReadInt32(payload[8:12]))
	response, _ := game.GlobalTradeRegistry.OfferItemToTrade(playerEntity, itemID, qty)
	systems.SendNoticeSystem(playerEntity, []byte(response))
}

func handleTradeConfirm(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	response, _ := game.GlobalTradeRegistry.LockTradeStage(playerEntity)
	if response != "" {
		systems.SendNoticeSystem(playerEntity, []byte(response))
	}
}

func handleTradeCancel(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	response, _ := game.GlobalTradeRegistry.CancelTradeSession(playerEntity)
	systems.SendNoticeSystem(playerEntity, []byte(response))
}

// ─── Skills & Chat ────────────────────────────────────────────────────────────

func handleSkillCast(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	if len(payload) != 16 {
		systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid SKILL CAST payload length. Expected 16 bytes.\r\n"))
		return
	}
	skillID := codec.ReadUint64(payload[0:8])
	targetID := ecs.Entity(codec.ReadUint64(payload[8:16]))
	response, _ := game.HandleSkillCastingSystem(playerEntity, skillID, targetID)
	systems.SendNoticeSystem(playerEntity, []byte(response))
}

func handleChat(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	msg := string(payload)
	game.RouteChatMessage(playerEntity, msg)
}

func handleHeartbeat(conn net.Conn, playerEntity ecs.Entity, payload []byte) {
	// Reset read deadline — this is the primary purpose of heartbeat.
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	// Pong back to client.
	pong := [4]byte{0, 1, protocol.OpcodeS2CHeartbeat}
	_ = netio.WritePacket(conn, pong[:3])
}
