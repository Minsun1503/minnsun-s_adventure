// Package threat provides a zero-allocation threat/aggro management system
// for the Minnsun's Adventure 2.5D MMORPG server.
//
// # Why this package exists
//
// Boss fights and group PvE require a threat table to determine which player
// the monster/boss should attack. This package provides a pooled threat table
// with top-threat tracking, decay, taunt mechanics, and threat transfer.
//
// # Peak Go Contract
//
// Zero heap allocations on hot-path operations (add threat, get top threat).
// Uses pool.SlicePool for internal storage.
package threat

import (
	"server/peakgo/pool"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	// MaxPlayers is the maximum number of players tracked per threat table.
	MaxPlayers = 64

	// DefaultThreatDecay is the default threat decay per tick (per-mille).
	// 990 = 1% decay per tick (99% retained).
	DefaultThreatDecay = 990

	// TauntMultiplier is how much taunt abilities multiply existing threat (per-mille).
	TauntMultiplier = 2000 // 2x threat
)

// ─── Types ────────────────────────────────────────────────────────────────────

// ThreatEntry represents a single player's threat value.
// Value type — stored inline in the pool.
type ThreatEntry struct {
	PlayerID uint64
	Threat   int64
}

// ThreatTable manages per-player threat values for a single monster/boss.
// Embed this into monster ECS components.
type ThreatTable struct {
	entries     []ThreatEntry // Pooled slice, sorted by threat (highest first)
	decayRate   int           // Per-mille decay per tick (e.g., 990 = 99% retained)
	totalThreat int64         // Sum of all threat for normalization

	// Heap structure for top-threat tracking
	heapSize int
	heap     [MaxPlayers]int // Indices into entries
}

// referenced pools
var entryPool = pool.NewSlicePool[ThreatEntry](8)

// NewThreatTable creates a new threat table with default decay.
func NewThreatTable() *ThreatTable {
	return &ThreatTable{
		entries:   *entryPool.Get(),
		decayRate: DefaultThreatDecay,
	}
}

// NewThreatTableWithDecay creates a new threat table with a custom decay rate.
func NewThreatTableWithDecay(decayRate int) *ThreatTable {
	if decayRate < 500 || decayRate > 1000 {
		decayRate = DefaultThreatDecay
	}
	return &ThreatTable{
		entries:   *entryPool.Get(),
		decayRate: decayRate,
	}
}

// ─── Threat Operations ────────────────────────────────────────────────────────

// Add adds threat for a player. If the player doesn't exist, creates a new entry.
// O(n) for insert, O(1) for update. Zero alloc.
func (t *ThreatTable) Add(playerID uint64, amount int64) {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			// Update existing
			t.entries[i].Threat += amount
			t.totalThreat += amount
			// Keep sorted
			t.fixSort(i)
			return
		}
	}

	// New player (only if under limit)
	if len(t.entries) >= MaxPlayers {
		return
	}

	t.entries = append(t.entries, ThreatEntry{
		PlayerID: playerID,
		Threat:   amount,
	})
	t.totalThreat += amount

	// Sort new entry into position
	t.fixSort(len(t.entries) - 1)
}

// Set sets a player's threat to an exact value.
// Useful for threat wiping or initial aggro.
func (t *ThreatTable) Set(playerID uint64, amount int64) {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			t.totalThreat += amount - t.entries[i].Threat
			t.entries[i].Threat = amount
			t.fixSort(i)
			return
		}
	}

	// Not found, add as new
	if len(t.entries) < MaxPlayers {
		t.entries = append(t.entries, ThreatEntry{
			PlayerID: playerID,
			Threat:   amount,
		})
		t.totalThreat += amount
		t.fixSort(len(t.entries) - 1)
	}
}

// Top returns the player ID with the highest threat.
// Returns 0 if no players in the threat table.
// O(1) — threat table is always sorted.
func (t *ThreatTable) Top() (playerID uint64, threat int64) {
	if len(t.entries) == 0 {
		return 0, 0
	}
	return t.entries[0].PlayerID, t.entries[0].Threat
}

// TopN returns the top N players by threat (up to MaxPlayers).
// Uses the pre-sorted entries array. Returns the actual number found.
func (t *ThreatTable) TopN(n int) []ThreatEntry {
	if n <= 0 {
		return nil
	}
	if n > len(t.entries) {
		n = len(t.entries)
	}
	return t.entries[:n]
}

// Remove removes a player from the threat table (e.g., on death or leaving range).
func (t *ThreatTable) Remove(playerID uint64) {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			t.totalThreat -= t.entries[i].Threat
			// Remove by swapping with last
			t.entries[i] = t.entries[len(t.entries)-1]
			t.entries = t.entries[:len(t.entries)-1]
			// Fix sort at current position
			if i < len(t.entries) {
				t.fixSort(i)
			}
			return
		}
	}
}

// Clear removes all entries from the threat table.
func (t *ThreatTable) Clear() {
	// Zero out entries for GC safety
	for i := range t.entries {
		t.entries[i] = ThreatEntry{}
	}
	t.entries = t.entries[:0]
	t.totalThreat = 0
}

// Len returns the number of players in the threat table.
func (t *ThreatTable) Len() int {
	return len(t.entries)
}

// Total returns the sum of all threat values.
func (t *ThreatTable) Total() int64 {
	return t.totalThreat
}

// Get returns the threat value for a specific player.
// Returns 0 if the player is not in the threat table.
func (t *ThreatTable) Get(playerID uint64) int64 {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			return t.entries[i].Threat
		}
	}
	return 0
}

// ─── Decay ────────────────────────────────────────────────────────────────────

// Decay applies threat decay to all entries.
// Call once per game-loop tick for active monsters.
func (t *ThreatTable) Decay() {
	if len(t.entries) == 0 {
		return
	}

	newTotal := int64(0)
	for i := range t.entries {
		// Apply decay (per-mille)
		t.entries[i].Threat = (t.entries[i].Threat * int64(t.decayRate)) / 1000
		newTotal += t.entries[i].Threat
	}
	t.totalThreat = newTotal

	// Re-sort after decay (threat values changed)
	t.sort()
}

// SetDecayRate changes the decay rate.
// decayRate is per-mille (990 = 99% retained = 1% decay per tick).
func (t *ThreatTable) SetDecayRate(decayRate int) {
	if decayRate < 500 || decayRate > 1000 {
		return
	}
	t.decayRate = decayRate
}

// ─── Taunt ────────────────────────────────────────────────────────────────────

// Taunt makes the monster focus on the taunting player.
// Sets the player's threat to (current_highest_threat + 1) * multiplier.
func (t *ThreatTable) Taunt(playerID uint64, multiplier int) {
	if len(t.entries) == 0 {
		t.Add(playerID, 1000)
		return
	}

	topThreat := t.entries[0].Threat
	if multiplier <= 0 {
		multiplier = TauntMultiplier
	}

	// New threat = (top + 1) * multiplier / 1000
	newThreat := ((topThreat + 1) * int64(multiplier)) / 1000
	t.Add(playerID, newThreat)
}

// ─── Transfer ─────────────────────────────────────────────────────────────────

// Transfer moves a percentage of threat from one player to another.
// Useful for threat reduction abilities or boss mechanics.
func (t *ThreatTable) Transfer(fromID, toID uint64, percentage int) {
	threat := t.Get(fromID)
	transferAmount := (threat * int64(percentage)) / 1000

	t.Add(fromID, -transferAmount)
	t.Add(toID, transferAmount)
}

// ─── Reset ────────────────────────────────────────────────────────────────────

// ResetWipe sets a specific player's threat to 0 (used for aggro wipe mechanics).
func (t *ThreatTable) ResetWipe(playerID uint64) {
	t.Set(playerID, 0)
}

// FullWipe resets all threat to 0.
func (t *ThreatTable) FullWipe() {
	t.Clear()
}

// ─── Sorting ──────────────────────────────────────────────────────────────────

// fixSort bubble-sorts an element at index i into its correct position.
// Assumes the rest of the array is already correctly sorted.
func (t *ThreatTable) fixSort(i int) {
	// Bubble up if needed (higher threat should be first)
	for i > 0 && t.entries[i].Threat > t.entries[i-1].Threat {
		t.entries[i], t.entries[i-1] = t.entries[i-1], t.entries[i]
		i--
	}
	// Bubble down if needed
	for i < len(t.entries)-1 && t.entries[i].Threat < t.entries[i+1].Threat {
		t.entries[i], t.entries[i+1] = t.entries[i+1], t.entries[i]
		i++
	}
}

// sort performs a full insertion sort on the entries.
func (t *ThreatTable) sort() {
	for i := 1; i < len(t.entries); i++ {
		key := t.entries[i]
		j := i - 1
		for j >= 0 && t.entries[j].Threat < key.Threat {
			t.entries[j+1] = t.entries[j]
			j--
		}
		t.entries[j+1] = key
	}
}

// ─── Destructor ───────────────────────────────────────────────────────────────

// Close returns the threat table's internal storage to the pool.
// Call when the monster is despawned.
func (t *ThreatTable) Close() {
	t.Clear()
	entryPool.Put(&t.entries)
}
