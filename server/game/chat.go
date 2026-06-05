package game

import (
	"strings"

	"server/ecs"
	"server/peakgo/broadcast"
	"server/protocol"
)

func RouteChatMessage(senderID ecs.Entity, rawMessage string) {
	trimmed := strings.TrimSpace(rawMessage)
	if len(trimmed) == 0 {
		return
	}

	meta, hasMeta := ecs.DefaultRegistry.GetMetadata(senderID)
	if !hasMeta {
		return
	}

	if strings.HasPrefix(trimmed, "/p ") || strings.HasPrefix(trimmed, "/party ") {
		// PARTY CHAT CHANNEL
		messageBody := stripChatPrefix(trimmed)
		chatPayload := broadcast.ChatPayload{
			Channel:    1, // party channel
			SenderName: meta.Name,
			Message:    messageBody,
		}
		frame := broadcast.BuildChatMessage(chatPayload)

		if memberComp, isGrouped := ecs.DefaultRegistry.GetPartyMember(senderID); isGrouped {
			BroadcastToPartyBinary(memberComp.PartyID, frame)
		} else {
			SendNoticeSystem(senderID, broadcast.BuildNotice(broadcast.NoticePayload{Message: "Error: You are not currently inside an active party group!"}))
		}

	} else if strings.HasPrefix(trimmed, "/g ") || strings.HasPrefix(trimmed, "/global ") {
		// GLOBAL WORLD CHAT CHANNEL
		messageBody := stripChatPrefix(trimmed)
		chatPayload := broadcast.ChatPayload{
			Channel:    2, // global channel
			SenderName: meta.Name,
			Message:    messageBody,
		}
		frame := broadcast.BuildChatMessage(chatPayload)
		BroadcastToWorldBinary(frame)

	} else {
		// LOCAL MAP CHAT CHANNEL (DEFAULT FALLBACK)
		pos, hasPos := ecs.DefaultRegistry.GetPosition(senderID)
		if !hasPos {
			return
		}
		chatPayload := broadcast.ChatPayload{
			Channel:    0, // local/map channel
			SenderName: meta.Name,
			Message:    trimmed,
		}
		frame := broadcast.BuildChatMessage(chatPayload)
		protocol.BroadcastToNeighbors(pos, frame, senderID)
	}
}

func stripChatPrefix(msg string) string {
	idx := strings.IndexByte(msg, ' ')
	if idx != -1 && idx+1 < len(msg) {
		return msg[idx+1:]
	}
	return ""
}

// Deprecated: kept for backward compatibility with text-based systems
func BroadcastToWorld(textPacket string) {
	bytePayload := []byte(textPacket)
	ecs.DefaultRegistry.RangeConnections(func(id ecs.Entity, connComp ecs.ConnectionComponent) bool {
		if connComp.Conn != nil {
			SendNoticeSystem(id, bytePayload)
		}
		return true
	})
}

func BroadcastToWorldBinary(frame []byte) {
	ecs.DefaultRegistry.RangeConnections(func(id ecs.Entity, connComp ecs.ConnectionComponent) bool {
		if connComp.Conn != nil {
			_, _ = connComp.Conn.Write(frame)
		}
		return true
	})
}

func BroadcastToPartyBinary(partyID ecs.Entity, frame []byte) {
	registry := ecs.DefaultRegistry
	party, ok := registry.GetParty(partyID)
	if !ok {
		return
	}
	for _, memberID := range party.MemberIDs {
		if conn, ok := registry.GetConnection(memberID); ok && conn.Conn != nil {
			_, _ = conn.Conn.Write(frame)
		}
	}
}
