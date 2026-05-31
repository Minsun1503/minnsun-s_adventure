package network

import (
	"fmt"
	"server/state"
)

// BroadcastToAll goes through our master ledger and syncs information to everyone
func BroadcastToAll(message string) {
	// Reads directly from our isolated state file map
	for _, playerLink := range state.WorldPlayer {
		if playerLink.Conn != nil {
			playerLink.Conn.Write([]byte(message))
		}
	}
	fmt.Print("[BROADCAST] " + message)
}
