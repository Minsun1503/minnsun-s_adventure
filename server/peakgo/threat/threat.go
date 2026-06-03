package threat

import (
	"sync"
)

const (
	// MaxPlayers is the maximum number of players tracked in a single threat table.
	MaxPlayers = 64

	// DefaultThreatDecay is the default decay rate per tick (per-mille).
	DefaultThreatDecay = 20 // 2% per tick

	// TauntMultiplier is the multiplier applied by taunt effects (per-mille).
	TauntMultiplier = 2000 // 2x threat generation
)

// ThreatEntry represents a single player's threat against a monster.
type ThreatEntry struct {
	PlayerID uint64
	Threat   int64
}

// ThreatTable manages aggro/hate for a single monster using a max-heap.
type ThreatTable struct {
	entries     []ThreatEntry
	decayRate   int
	totalThreat int64
	heapSize    int
	heap        [MaxPlayers]int
}

// entryPool recycles threat table entry slices to reduce GC pressure.
var entryPool = sync.Pool{
	New: func() interface{} {
		slice := make([]ThreatEntry, 0, MaxPlayers)
		return &slice
	},
}

// NewThreatTable creates a new ThreatTable with default decay.
func NewThreatTable() *ThreatTable {
	return &ThreatTable{
		entries:   *entryPool.Get().(*[]ThreatEntry),
		decayRate: DefaultThreatDecay,
	}
}

// NewThreatTableWithDecay creates a new ThreatTable with a custom decay rate.
func NewThreatTableWithDecay(decayRate int) *ThreatTable {
	return &ThreatTable{
		entries:   *entryPool.Get().(*[]ThreatEntry),
		decayRate: decayRate,
	}
}

// Add adds or increments threat for a player.
func (t *ThreatTable) Add(playerID uint64, amount int64) {
	if len(t.entries) >= MaxPlayers {
		return
	}

	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			t.entries[i].Threat += amount
			t.totalThreat += amount
			t.sort()
			return
		}
	}

	// New entry
	t.entries = append(t.entries, ThreatEntry{
		PlayerID: playerID,
		Threat:   amount,
	})
	t.totalThreat += amount
	t.sort()
}

// Set sets the exact threat value for a player.
func (t *ThreatTable) Set(playerID uint64, amount int64) {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			diff := amount - t.entries[i].Threat
			t.entries[i].Threat = amount
			t.totalThreat += diff
			t.sort()
			return
		}
	}

	// New entry
	t.entries = append(t.entries, ThreatEntry{
		PlayerID: playerID,
		Threat:   amount,
	})
	t.totalThreat += amount
	t.sort()
}

// Top returns the player with the highest threat and their threat value.
func (t *ThreatTable) Top() (playerID uint64, threat int64) {
	if len(t.entries) == 0 {
		return 0, 0
	}
	return t.entries[t.heap[0]].PlayerID, t.entries[t.heap[0]].Threat
}

// TopN returns the top N players by threat.
func (t *ThreatTable) TopN(n int) []ThreatEntry {
	if n > len(t.entries) {
		n = len(t.entries)
	}
	result := make([]ThreatEntry, n)
	for i := 0; i < n; i++ {
		result[i] = t.entries[t.heap[i]]
	}
	return result
}

// Remove removes a player from the threat table.
func (t *ThreatTable) Remove(playerID uint64) {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			t.totalThreat -= t.entries[i].Threat
			t.entries = append(t.entries[:i], t.entries[i+1:]...)
			t.sort()
			return
		}
	}
}

// Clear removes all entries and returns memory to the pool.
func (t *ThreatTable) Clear() {
	if t.entries != nil {
		entryPool.Put(&t.entries)
		t.entries = nil
	}
	t.totalThreat = 0
}

// Len returns the number of entries in the threat table.
func (t *ThreatTable) Len() int {
	return len(t.entries)
}

// Total returns the total threat across all players.
func (t *ThreatTable) Total() int64 {
	return t.totalThreat
}

// Get returns the threat value for a specific player.
func (t *ThreatTable) Get(playerID uint64) int64 {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			return t.entries[i].Threat
		}
	}
	return 0
}

// Decay applies percentage-based decay to all entries.
func (t *ThreatTable) Decay() {
	for i := range t.entries {
		decay := t.entries[i].Threat * int64(t.decayRate) / 1000
		t.entries[i].Threat -= decay
	}
	t.totalThreat = 0
	for i := range t.entries {
		t.totalThreat += t.entries[i].Threat
	}
	t.sort()
}

// SetDecayRate sets the decay rate for the threat table.
func (t *ThreatTable) SetDecayRate(decayRate int) {
	if decayRate < 500 || decayRate > 1000 {
		return
	}
	t.decayRate = decayRate
}

// Taunt applies threat multiplier for taunt effects.
func (t *ThreatTable) Taunt(playerID uint64, multiplier int) {
	if len(t.entries) == 0 {
		return
	}
	topThreat := t.entries[t.heap[0]].Threat
	newThreat := ((topThreat + 1) * int64(multiplier)) / 1000
	t.Add(playerID, newThreat)
}

// Transfer moves a percentage of threat from one player to another.
func (t *ThreatTable) Transfer(fromID, toID uint64, percentage int) {
	var fromThreat int64
	for i := range t.entries {
		if t.entries[i].PlayerID == fromID {
			fromThreat = t.entries[i].Threat
			break
		}
	}
	if fromThreat == 0 {
		return
	}

	transferAmount := fromThreat * int64(percentage) / 1000
	t.Add(toID, transferAmount)
	t.Add(fromID, -transferAmount)
}

// ResetWipe sets a player's threat to zero and re-sorts.
func (t *ThreatTable) ResetWipe(playerID uint64) {
	for i := range t.entries {
		if t.entries[i].PlayerID == playerID {
			t.totalThreat -= t.entries[i].Threat
			t.entries[i].Threat = 0
			t.sort()
			return
		}
	}
}

// FullWipe zeros out all threat values.
func (t *ThreatTable) FullWipe() {
	t.Clear()
}

// fixSort maintains heap property after an update at position i.
func (t *ThreatTable) fixSort(i int) {
	if i > 0 && t.entries[t.heap[(i-1)/2]].Threat < t.entries[t.heap[i]].Threat {
		t.up(i)
	} else {
		t.down(i)
	}
}

// up moves element at position i up the heap.
func (t *ThreatTable) up(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if t.entries[t.heap[parent]].Threat >= t.entries[t.heap[i]].Threat {
			break
		}
		t.heap[parent], t.heap[i] = t.heap[i], t.heap[parent]
		i = parent
	}
}

// down moves element at position i down the heap.
func (t *ThreatTable) down(i int) {
	n := len(t.entries)
	for {
		largest := i
		left := 2*i + 1
		right := 2*i + 2

		if left < n && t.entries[t.heap[left]].Threat > t.entries[t.heap[largest]].Threat {
			largest = left
		}
		if right < n && t.entries[t.heap[right]].Threat > t.entries[t.heap[largest]].Threat {
			largest = right
		}
		if largest == i {
			break
		}
		t.heap[i], t.heap[largest] = t.heap[largest], t.heap[i]
		i = largest
	}
}

// sort rebuilds the heap from static indices.
func (t *ThreatTable) sort() {
	t.heapSize = len(t.entries)

	// heap[i] stores entry index
	for i := 0; i < t.heapSize; i++ {
		t.heap[i] = i
	}

	// Build max-heap
	for i := (t.heapSize - 1) / 2; i >= 0; i-- {
		t.down(i)
	}
}

// Close releases the threat table and returns pooled memory.
func (t *ThreatTable) Close() {
	t.Clear()
}
