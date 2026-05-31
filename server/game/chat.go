package game

import (
	"fmt"
	"server/ecs"
	"server/protocol"
	"strings"
)

// RouteChatMessage inspects the prefix of a text string and dispatches it
// down the appropriate isolated network channel.
func RouteChatMessage(senderID ecs.Entity, rawMessage string) {
	trimmed := strings.TrimSpace(rawMessage)
	if len(trimmed) == 0 {
		return
	}

	// 1. Fetch sender identity information lock-free
	meta, hasMeta := ecs.GlobalRegistry.GetMetadata(senderID)
	if !hasMeta {
		return
	}

	// 2. CHANNEL ROUTING PATHS
	if strings.HasPrefix(trimmed, "/p ") || strings.HasPrefix(trimmed, "/party ") {
		// PARTY CHAT CHANNEL
		messageBody := stripChatPrefix(trimmed)
		formattedPacket := fmt.Sprintf("[PARTY] %s: %s\r\n", meta.Name, messageBody)
		
		if memberComp, isGrouped := ecs.GlobalRegistry.GetPartyMember(senderID); isGrouped {
			BroadcastToParty(memberComp.PartyID, formattedPacket)
		} else {
			SendNoticeSystem(senderID, []byte("Error: You are not currently inside an active party group!\r\n"))
		}

	} else if strings.HasPrefix(trimmed, "/g ") || strings.HasPrefix(trimmed, "/global ") {
		// GLOBAL WORLD CHAT CHANNEL
		messageBody := stripChatPrefix(trimmed)
		formattedPacket := fmt.Sprintf("[GLOBAL] %s: %s\r\n", meta.Name, messageBody)
		BroadcastToWorld(formattedPacket)

	} else {
		// LOCAL MAP CHAT CHANNEL (DEFAULT FALLBACK)
		pos, hasPos := ecs.GlobalRegistry.GetPosition(senderID)
		if !hasPos {
			return
		}
		formattedPacket := fmt.Sprintf("[MAP] %s: %s\r\n", meta.Name, trimmed)
		protocol.BroadcastToMap(pos.MapID, formattedPacket)
	}
}

// Helper tool to separate prefix commands from raw message bodies
func stripChatPrefix(msg string) string {
	parts := strings.SplitN(msg, " ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// BroadcastToWorld pumps a serialized string packet into every connected client on the server.
func BroadcastToWorld(textPacket string) {
	bytePayload := []byte(textPacket)
	ecs.GlobalRegistry.RangeConnections(func(id ecs.Entity, connComp ecs.ConnectionComponent) bool {
		if connComp.Conn != nil {
			SendNoticeSystem(id, bytePayload)
		}
		return true
	})
}
