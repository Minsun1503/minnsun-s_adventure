package game

import (
	"fmt"
	"server/ecs"
	"server/logger"
)

const maxPartySize = 4

// CreatePartySystem creates a new party entity and sets the leader as the first member.
// Returns a formatted confirmation message ready to send to the leader.
func CreatePartySystem(leaderID ecs.Entity, teamName string) string {
	registry := ecs.GlobalRegistry

	if _, already := registry.GetPartyMember(leaderID); already {
		return "Error: You are already in a party. Leave your current party first.\r\n"
	}

	partyID := registry.NewEntity()

	registry.SetParty(partyID, ecs.PartyComponent{
		LeaderID:  leaderID,
		TeamName:  teamName,
		MemberIDs: []ecs.Entity{leaderID},
	})

	registry.SetPartyMember(leaderID, ecs.PartyMemberComponent{
		PartyID: partyID,
	})

	logger.Info("[PARTY] Party '%s' created by entity %d (party entity %d)", teamName, leaderID, partyID)
	return fmt.Sprintf("Party '%s' created! You are the leader.\r\n", teamName)
}

// BroadcastToParty sends a text packet to every member of a party.
func BroadcastToParty(partyID ecs.Entity, textPacket string) {
	registry := ecs.GlobalRegistry

	party, ok := registry.GetParty(partyID)
	if !ok {
		return
	}

	data := []byte(textPacket)

	for _, memberID := range party.MemberIDs {
		SendNoticeSystem(memberID, data)
	}
}

// GetPlayerPartyID returns the party entity ID if the player is in a party,
// or 0 if not.
func GetPlayerPartyID(playerID ecs.Entity) ecs.Entity {
	pm, ok := ecs.GlobalRegistry.GetPartyMember(playerID)
	if !ok {
		return 0
	}
	return pm.PartyID
}

// AddMemberToParty appends a player to the party roster and sets their
// PartyMemberComponent. Assumes caller has already validated the invite.
func AddMemberToParty(partyID, playerID ecs.Entity) {
	registry := ecs.GlobalRegistry

	party, ok := registry.GetParty(partyID)
	if !ok {
		return
	}

	party.MemberIDs = append(party.MemberIDs, playerID)
	registry.SetParty(partyID, party)

	registry.SetPartyMember(playerID, ecs.PartyMemberComponent{
		PartyID: partyID,
	})
}

// RemovePlayerFromParty handles graceful party membership cleanup.
// Called when a player disconnects (before RemoveEntity).
//
// Responsibilities:
//   - Removes the player from the MemberIDs list.
//   - If roster becomes empty → deletes the party entity.
//   - If the player was the leader → promotes the next member.
//   - Broadcasts a roster update to remaining members.
func RemovePlayerFromParty(playerID ecs.Entity) {
	registry := ecs.GlobalRegistry

	pm, ok := registry.GetPartyMember(playerID)
	if !ok {
		return // not in a party
	}

	partyID := pm.PartyID
	party, ok := registry.GetParty(partyID)
	if !ok {
		registry.DeletePartyMember(playerID)
		return
	}

	// Remove the player from the member list.
	newMembers := party.MemberIDs[:0]
	for _, mid := range party.MemberIDs {
		if mid != playerID {
			newMembers = append(newMembers, mid)
		}
	}
	party.MemberIDs = newMembers

	// Clean up the player's PartyMemberComponent.
	registry.DeletePartyMember(playerID)

	// If roster is now empty, delete the party entity entirely.
	if len(party.MemberIDs) == 0 {
		registry.DeleteParty(partyID)
		logger.Info("[PARTY] Party '%s' (entity %d) disbanded — no members remain.", party.TeamName, partyID)
		return
	}

	// If the leaving player was the leader, promote the next member.
	if playerID == party.LeaderID {
		party.LeaderID = party.MemberIDs[0]
		logger.Info("[PARTY] Party '%s': leader left — %d promoted to leader.", party.TeamName, party.LeaderID)
	}

	registry.SetParty(partyID, party)

	// Notify remaining members of the roster change.
	leaderMeta, _ := registry.GetMetadata(party.LeaderID)
	leaverMeta, _ := registry.GetMetadata(playerID)
	leaverName := fmt.Sprintf("entity_%d", playerID)
	if leaverMeta.Name != "" {
		leaverName = leaverMeta.Name
	}
	leaderName := fmt.Sprintf("entity_%d", party.LeaderID)
	if leaderMeta.Name != "" {
		leaderName = leaderMeta.Name
	}

	rosterMsg := fmt.Sprintf("[PARTY] %s has left the party. %s is now the leader. Members remaining: %d\r\n",
		leaverName, leaderName, len(party.MemberIDs))
	BroadcastToParty(partyID, rosterMsg)
}

// GetPartyMemberIDs returns a copy of the current roster for a party.
// Returns nil if the party does not exist.
func GetPartyMemberIDs(partyID ecs.Entity) []ecs.Entity {
	party, ok := ecs.GlobalRegistry.GetParty(partyID)
	if !ok {
		return nil
	}
	// Return a copy to prevent external mutation of the internal slice.
	out := make([]ecs.Entity, len(party.MemberIDs))
	copy(out, party.MemberIDs)
	return out
}
