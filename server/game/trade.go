package game

import (
	"fmt"
	"server/ecs"
	"sync"
)

// TradeOffer stores the isolated inventory snapshot a player is offering
type TradeOffer struct {
	PlayerID ecs.Entity
	IsLocked bool
	ItemIDs  map[uint64]int // ItemTemplateID -> Quantity
}

// TradeSession orchestrates the isolated interaction space between two players
type TradeSession struct {
	SessionID ecs.Entity
	OfferA    TradeOffer
	OfferB    TradeOffer
}

type TradeSystemRegistry struct {
	mu       sync.Mutex
	sessions map[ecs.Entity]*TradeSession
	bindings map[ecs.Entity]ecs.Entity // Maps PlayerID -> SessionID
}

var GlobalTradeRegistry = &TradeSystemRegistry{
	sessions: make(map[ecs.Entity]*TradeSession),
	bindings: make(map[ecs.Entity]ecs.Entity),
}

// InitializeTradeSession boots an isolated transaction room between two players
func (tr *TradeSystemRegistry) InitializeTradeSession(playerA, playerB ecs.Entity) (string, bool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Guardrail: A player cannot trade with themselves
	if playerA == playerB {
		return "Error: You cannot trade with yourself.\r\n", false
	}

	// Verify target player exists
	registry := ecs.GlobalRegistry
	targetMeta, hasMeta := registry.GetMetadata(playerB)
	if !hasMeta {
		return "Error: Target player does not exist.\r\n", false
	}

	// Guardrail: Ensure neither player is currently locked in an active trade session
	if _, busyA := tr.bindings[playerA]; busyA {
		return "Error: You are already in an active trade session.\r\n", false
	}
	if _, busyB := tr.bindings[playerB]; busyB {
		return "Error: Target player is already in an active trade session.\r\n", false
	}

	sessionID := registry.NewEntity()
	session := &TradeSession{
		SessionID: sessionID,
		OfferA:    TradeOffer{PlayerID: playerA, ItemIDs: make(map[uint64]int)},
		OfferB:    TradeOffer{PlayerID: playerB, ItemIDs: make(map[uint64]int)},
	}

	tr.sessions[sessionID] = session
	tr.bindings[playerA] = sessionID
	tr.bindings[playerB] = sessionID

	// Notify player B
	inviterMeta, _ := registry.GetMetadata(playerA)
	inviterName := fmt.Sprintf("entity_%d", playerA)
	if inviterMeta.Name != "" {
		inviterName = inviterMeta.Name
	}
	targetName := fmt.Sprintf("entity_%d", playerB)
	if targetMeta.Name != "" {
		targetName = targetMeta.Name
	}

	alertMsg := fmt.Sprintf("[TRADE] Player %s has opened a trade session with you!\r\n", inviterName)
	SendNoticeSystem(playerB, []byte(alertMsg))

	fmt.Printf("[TRADE] Session %d created between %d (%s) and %d (%s)\n", sessionID, playerA, inviterName, playerB, targetName)
	return fmt.Sprintf("Trade session opened with %s.\r\n", targetName), true
}

// OfferItemToTrade places items into the isolated trade window
func (tr *TradeSystemRegistry) OfferItemToTrade(playerID ecs.Entity, itemID uint64, qty int) (string, bool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if qty <= 0 {
		return "Error: Invalid quantity.\r\n", false
	}

	sessionID, exists := tr.bindings[playerID]
	if !exists {
		return "Error: You are not in an active trade session.\r\n", false
	}

	session := tr.sessions[sessionID]
	var currentOffer *TradeOffer
	var otherPlayer ecs.Entity
	if session.OfferA.PlayerID == playerID {
		currentOffer = &session.OfferA
		otherPlayer = session.OfferB.PlayerID
	} else {
		currentOffer = &session.OfferB
		otherPlayer = session.OfferA.PlayerID
	}

	if currentOffer.IsLocked {
		return "Error: You cannot modify your offer after locking the trade window!\r\n", false
	}

	// Verify player actually owns the items inside their ECS data column right now
	inv, hasInv := ecs.GlobalRegistry.GetInventory(playerID)
	if !hasInv {
		return "Error: Inventory not found.\r\n", false
	}

	totalOffered := currentOffer.ItemIDs[itemID] + qty
	if inv.Items[itemID] < totalOffered {
		return "Error: Insufficient inventory quantity!\r\n", false
	}

	currentOffer.ItemIDs[itemID] = totalOffered

	// Notify the counterparty about the offer
	offerMsg := fmt.Sprintf("[TRADE] Counterparty offered Item #%d x%d.\r\n", itemID, qty)
	SendNoticeSystem(otherPlayer, []byte(offerMsg))

	return fmt.Sprintf("Successfully offered Item ID #%d x%d to the trade window.\r\n", itemID, qty), true
}

// LockTradeStage signs off on Phase 1 of the atomic commit
func (tr *TradeSystemRegistry) LockTradeStage(playerID ecs.Entity) (string, bool) {
	tr.mu.Lock()
	sessionID, exists := tr.bindings[playerID]
	if !exists {
		tr.mu.Unlock()
		return "Error: You are not in an active trade session.\r\n", false
	}
	session := tr.sessions[sessionID]

	var myOffer, opponentOffer *TradeOffer
	var otherPlayer ecs.Entity
	if session.OfferA.PlayerID == playerID {
		myOffer = &session.OfferA
		opponentOffer = &session.OfferB
		otherPlayer = session.OfferB.PlayerID
	} else {
		myOffer = &session.OfferB
		opponentOffer = &session.OfferA
		otherPlayer = session.OfferA.PlayerID
	}

	if myOffer.IsLocked {
		tr.mu.Unlock()
		return "Error: You have already locked your trade stage.\r\n", false
	}

	myOffer.IsLocked = true
	bothLocked := myOffer.IsLocked && opponentOffer.IsLocked
	tr.mu.Unlock()

	// Notify opponent of lock
	lockMsg := "[TRADE] Counterparty has locked their offer window.\r\n"
	SendNoticeSystem(otherPlayer, []byte(lockMsg))

	// Phase 2: If BOTH parties have locked their windows, execute the atomic final inventory swap loop
	if bothLocked {
		return tr.ExecuteAtomicTradeSwap(sessionID)
	}

	return "Offer window locked. Waiting for counterparty lock approval...\r\n", true
}

// ExecuteAtomicTradeSwap performs the double-backpack mutation using copy-modify-overwrite
func (tr *TradeSystemRegistry) ExecuteAtomicTradeSwap(sessionID ecs.Entity) (string, bool) {
	tr.mu.Lock()
	session, exists := tr.sessions[sessionID]
	if !exists {
		tr.mu.Unlock()
		return "Error: Trade session does not exist.\r\n", false
	}
	tr.mu.Unlock()

	pA := session.OfferA.PlayerID
	pB := session.OfferB.PlayerID

	// Copy current inventories
	invA, hasInvA := ecs.GlobalRegistry.GetInventory(pA)
	invB, hasInvB := ecs.GlobalRegistry.GetInventory(pB)

	if !hasInvA || !hasInvB {
		tr.CancelTradeSession(pA)
		return "Error: One or both player inventories are invalid. Trade aborted.\r\n", false
	}

	// Pre-transaction validation: Double check that both players still have the offered items
	for itemID, qty := range session.OfferA.ItemIDs {
		if invA.Items[itemID] < qty {
			tr.CancelTradeSession(pA)
			return "Error: Player A has insufficient items to trade. Trade aborted.\r\n", false
		}
	}
	for itemID, qty := range session.OfferB.ItemIDs {
		if invB.Items[itemID] < qty {
			tr.CancelTradeSession(pA)
			return "Error: Player B has insufficient items to trade. Trade aborted.\r\n", false
		}
	}

	// Initialize Items maps if they are nil
	if invA.Items == nil {
		invA.Items = make(map[uint64]int)
	}
	if invB.Items == nil {
		invB.Items = make(map[uint64]int)
	}

	// MUTATE PLAYER A DATA RECORD (Deduct A's offers, inject B's offers)
	for itemID, qty := range session.OfferA.ItemIDs {
		invA.Items[itemID] -= qty
		if invA.Items[itemID] == 0 {
			delete(invA.Items, itemID)
		}
	}
	for itemID, qty := range session.OfferB.ItemIDs {
		invA.Items[itemID] += qty
	}

	// MUTATE PLAYER B DATA RECORD (Deduct B's offers, inject A's offers)
	for itemID, qty := range session.OfferB.ItemIDs {
		invB.Items[itemID] -= qty
		if invB.Items[itemID] == 0 {
			delete(invB.Items, itemID)
		}
	}
	for itemID, qty := range session.OfferA.ItemIDs {
		invB.Items[itemID] += qty
	}

	// OVERWRITE BOTH DATA TABLES LOCK-FREE
	ecs.GlobalRegistry.SetInventory(pA, invA)
	ecs.GlobalRegistry.SetInventory(pB, invB)

	// PURGE TEMPORARY TRADE SESSION RECORDS
	tr.mu.Lock()
	delete(tr.bindings, pA)
	delete(tr.bindings, pB)
	delete(tr.sessions, sessionID)
	tr.mu.Unlock()

	// Notify player connection blocks
	msg := "[TRADE] Trade complete! Items have been exchanged.\r\n"
	SendNoticeSystem(pA, []byte(msg))
	SendNoticeSystem(pB, []byte(msg))

	return "Trade completed successfully.\r\n", true
}

// CancelTradeSession disbands the active trade session for a player and notifies counterparties
func (tr *TradeSystemRegistry) CancelTradeSession(playerID ecs.Entity) (string, bool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	sessionID, exists := tr.bindings[playerID]
	if !exists {
		return "Error: You are not in an active trade session.\r\n", false
	}

	session := tr.sessions[sessionID]
	pA := session.OfferA.PlayerID
	pB := session.OfferB.PlayerID

	delete(tr.bindings, pA)
	delete(tr.bindings, pB)
	delete(tr.sessions, sessionID)

	msg := "[TRADE] The trade session has been cancelled/disbanded.\r\n"
	if pA != playerID {
		SendNoticeSystem(pA, []byte(msg))
	} else {
		SendNoticeSystem(pB, []byte(msg))
	}

	return "Trade session cancelled successfully.\r\n", true
}
