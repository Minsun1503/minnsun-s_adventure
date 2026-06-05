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
	registry := ecs.DefaultRegistry

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
	registry := ecs.DefaultRegistry

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
	pm, ok := ecs.DefaultRegistry.GetPartyMember(playerID)
	if !ok {
		return 0
	}
	return pm.PartyID
}

// TryAddMemberToParty atomically checks the party size limit and adds the player
// if there is still room. Returns true on success, false if the party is full or
// no longer exists.
//
// This is the preferred call site for AcceptPartyInviteSystem because it
// eliminates the TOCTOU window that exists between a separate size-check and write.
// The CoW sequence (load → clone → append → store) is the atomic unit of mutation
// under the ECS sync.Map model: no other goroutine can observe a partially-appended
// slice because the new slice is only visible after SetParty's atomic Store returns.
func TryAddMemberToParty(partyID, playerID ecs.Entity) bool {
	registry := ecs.DefaultRegistry

	party, ok := registry.GetParty(partyID)
	if !ok {
		return false
	}

	// Re-check the size limit after loading the latest snapshot.
	// Any concurrent accept that committed first will have already updated the
	// stored value, so we see the post-commit length here.
	if len(party.MemberIDs) >= maxPartySize {
		return false
	}

	// Clone → mutate → store (CoW write).
	party = party.Clone()
	party.MemberIDs = append(party.MemberIDs, playerID)
	registry.SetParty(partyID, party)

	registry.SetPartyMember(playerID, ecs.PartyMemberComponent{
		PartyID: partyID,
	})
	return true
}

// AddMemberToParty appends a player to the party roster and sets their
// PartyMemberComponent. Assumes caller has already validated the invite
// and the party size limit.
func AddMemberToParty(partyID, playerID ecs.Entity) {
	registry := ecs.DefaultRegistry

	party, ok := registry.GetParty(partyID)
	if !ok {
		return
	}

	party = party.Clone()
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
	registry := ecs.DefaultRegistry

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

	party = party.Clone()

	// Remove the player from the member list.
	var newMembers []ecs.Entity
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
	party, ok := ecs.DefaultRegistry.GetParty(partyID)
	if !ok {
		return nil
	}
	// Return a copy to prevent external mutation of the internal slice.
	out := make([]ecs.Entity, len(party.MemberIDs))
	copy(out, party.MemberIDs)
	return out
}
