package game

import (
	"fmt"
	"server/ecs"
	"sync"
)

// InviteRecord represents a pending party invitation.
type InviteRecord struct {
	PartyID   ecs.Entity
	ExpiresAt uint64 // Tick-based expiration
}

// InviteCache is a thread-safe store for pending party invitations.
// Key: invited player's entity ID → InviteRecord.
type InviteCache struct {
	mu    sync.Mutex
	cache map[ecs.Entity]InviteRecord
}

// GlobalInviteCache is the singleton invitation cache.
var GlobalInviteCache = &InviteCache{
	cache: make(map[ecs.Entity]InviteRecord),
}

// Store saves an invitation record for a target player.
func (ic *InviteCache) Store(targetID ecs.Entity, record InviteRecord) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.cache[targetID] = record
}

// Take retrieves and removes an invitation record if it exists and
// has not expired. Returns zero-value record and false if invalid/expired.
func (ic *InviteCache) Take(targetID ecs.Entity) (InviteRecord, bool) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	record, ok := ic.cache[targetID]
	if !ok {
		return InviteRecord{}, false
	}
	delete(ic.cache, targetID)

	if GetCurrentTick() >= record.ExpiresAt {
		return InviteRecord{}, false
	}
	return record, true
}

// PurgeExpired removes all expired invitations. Called periodically by
// the game loop to prevent memory leaks from unclaimed invites.
func (ic *InviteCache) PurgeExpired() {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	currentTick := GetCurrentTick()
	for id, record := range ic.cache {
		if currentTick >= record.ExpiresAt {
			delete(ic.cache, id)
		}
	}
}

// SendPartyInviteSystem handles the Party Invite opcode.
// Validates:
//   - inviter is in a party AND is the leader
//   - party is not full (max 4 members)
//   - target is not already in a party
//   - target exists (has Metadata)
//
// Returns a feedback message and success flag.
func SendPartyInviteSystem(inviterID, targetID ecs.Entity) (string, bool) {
	registry := ecs.DefaultRegistry

	// Validate inviter is the party leader.
	partyID := GetPlayerPartyID(inviterID)
	if partyID == 0 {
		return "Error: You are not in a party.\r\n", false
	}

	party, ok := registry.GetParty(partyID)
	if !ok {
		return "Error: Party not found.\r\n", false
	}

	if party.LeaderID != inviterID {
		return "Error: Only the party leader can send invitations.\r\n", false
	}

	// Check party size limit.
	if len(party.MemberIDs) >= maxPartySize {
		return "Error: Party is full (max 4 members).\r\n", false
	}

	// Validate target player.
	if _, ok := registry.GetMetadata(targetID); !ok {
		return "Error: Target player not found.\r\n", false
	}

	if existingParty := GetPlayerPartyID(targetID); existingParty != 0 {
		return "Error: Target player is already in a party.\r\n", false
	}

	// Store the invitation with 120-tick expiry (30 seconds at 4 ticks/sec).
	GlobalInviteCache.Store(targetID, InviteRecord{
		PartyID:   partyID,
		ExpiresAt: GetCurrentTick() + 120,
	})

	// Notify the target player.
	inviterMeta, _ := registry.GetMetadata(inviterID)
	inviterName := fmt.Sprintf("entity_%d", inviterID)
	if inviterMeta.Name != "" {
		inviterName = inviterMeta.Name
	}

	inviteMsg := fmt.Sprintf("[PARTY] %s has invited you to join party '%s'! Use JOIN command to accept.\r\n",
		inviterName, party.TeamName)
	SendNoticeSystem(targetID, []byte(inviteMsg))

	successMsg := fmt.Sprintf("Invitation sent to %s.\r\n", inviterName)
	return successMsg, true
}

// AcceptPartyInviteSystem handles the Party Join opcode.
// Validates the invitation exists, hasn't expired, and matches the party ID.
// Adds the player to the party and broadcasts a roster update.
func AcceptPartyInviteSystem(playerID, targetPartyID ecs.Entity) (string, bool) {
	registry := ecs.DefaultRegistry

	// Verify invitation exists and is still valid.
	record, ok := GlobalInviteCache.Take(playerID)
	if !ok {
		return "Error: No pending invitation found or invitation has expired.\r\n", false
	}

	if record.PartyID != targetPartyID {
		return "Error: Invitation does not match the specified party.\r\n", false
	}

	// Verify the party still exists.
	party, ok := registry.GetParty(targetPartyID)
	if !ok {
		return "Error: Party no longer exists.\r\n", false
	}

	// Verify the player isn't already in a party (race condition guard).
	if existingParty := GetPlayerPartyID(playerID); existingParty != 0 {
		return "Error: You are already in a party.\r\n", false
	}

	// Atomically check size limit and add the player in a single CoW operation.
	// This eliminates the TOCTOU race between a separate size-check and write.
	if !TryAddMemberToParty(targetPartyID, playerID) {
		return "Error: Party is now full.\r\n", false
	}

	// Fetch names for the roster announcement.
	playerMeta, _ := registry.GetMetadata(playerID)
	playerName := fmt.Sprintf("entity_%d", playerID)
	if playerMeta.Name != "" {
		playerName = playerMeta.Name
	}

	// Re-fetch the party to get the updated member count after TryAddMemberToParty.
	updatedParty, _ := registry.GetParty(targetPartyID)
	rosterMsg := fmt.Sprintf("[PARTY] %s has joined '%s'! Members: %d/%d\r\n",
		playerName, party.TeamName, len(updatedParty.MemberIDs), maxPartySize)
	BroadcastToParty(targetPartyID, rosterMsg)

	personalMsg := fmt.Sprintf("You have joined '%s'!\r\n", party.TeamName)
	return personalMsg, true
}
