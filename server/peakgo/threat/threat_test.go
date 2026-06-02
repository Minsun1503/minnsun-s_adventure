package threat

import (
	"testing"
)

func TestNewThreatTable(t *testing.T) {
	tt := NewThreatTable()
	if tt.Len() != 0 {
		t.Fatalf("expected empty threat table, got %d entries", tt.Len())
	}
	if tt.decayRate != DefaultThreatDecay {
		t.Fatalf("expected default decay rate %d, got %d", DefaultThreatDecay, tt.decayRate)
	}
	tt.Close()
}

func TestAddAndTop(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Add(200, 500)
	tt.Add(300, 2000)

	topID, threat := tt.Top()
	if topID != 300 {
		t.Fatalf("expected player 300 as top, got %d", topID)
	}
	if threat != 2000 {
		t.Fatalf("expected threat 2000, got %d", threat)
	}

	if tt.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", tt.Len())
	}
}

func TestAddExisting(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Add(100, 500) // Add more threat to same player

	id, threat := tt.Top()
	if id != 100 {
		t.Fatalf("expected player 100 as top, got %d", id)
	}
	if threat != 1500 {
		t.Fatalf("expected threat 1500, got %d", threat)
	}
}

func TestSet(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Set(100, 5000) // Override

	_, threat := tt.Top()
	if threat != 5000 {
		t.Fatalf("expected threat 5000 after Set, got %d", threat)
	}
}

func TestRemove(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Add(200, 2000)
	tt.Remove(100)

	if tt.Len() != 1 {
		t.Fatalf("expected 1 entry after remove, got %d", tt.Len())
	}

	id, _ := tt.Top()
	if id != 200 {
		t.Fatalf("expected player 200 as top after removal, got %d", id)
	}
}

func TestClear(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Add(200, 2000)
	tt.Clear()

	if tt.Len() != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", tt.Len())
	}
}

func TestDecay(t *testing.T) {
	tt := NewThreatTableWithDecay(500) // 50% retained = 50% decay per tick
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Decay()

	threat := tt.Get(100)
	if threat != 500 { // 1000 * 500 / 1000 = 500
		t.Fatalf("expected threat 500 after 50%% decay, got %d", threat)
	}
}

func TestTaunt(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Taunt(200, TauntMultiplier) // 2x taunt

	id, threat := tt.Top()
	if id != 200 {
		t.Fatalf("expected taunting player 200 as top, got %d", id)
	}
	// Expected: (1000 + 1) * 2000 / 1000 = 2002
	if threat != 2002 {
		t.Fatalf("expected threat 2002 after taunt, got %d", threat)
	}
}

func TestTransfer(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Transfer(100, 200, 500) // Transfer 50% from 100 to 200

	threat1 := tt.Get(100)
	threat2 := tt.Get(200)

	if threat1 != 500 { // 1000 - 500 = 500
		t.Fatalf("expected player 100 threat 500 after transfer, got %d", threat1)
	}
	if threat2 != 500 { // 0 + 500 = 500
		t.Fatalf("expected player 200 threat 500 after transfer, got %d", threat2)
	}
}

func TestTopN(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Add(200, 2000)
	tt.Add(300, 3000)

	top2 := tt.TopN(2)
	if len(top2) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(top2))
	}
	if top2[0].PlayerID != 300 {
		t.Fatalf("expected player 300 as #1, got %d", top2[0].PlayerID)
	}
	if top2[1].PlayerID != 200 {
		t.Fatalf("expected player 200 as #2, got %d", top2[1].PlayerID)
	}
}

func TestGet(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	threat := tt.Get(100)
	if threat != 1000 {
		t.Fatalf("expected threat 1000, got %d", threat)
	}

	// Non-existent player
	threat = tt.Get(999)
	if threat != 0 {
		t.Fatalf("expected 0 for non-existent player, got %d", threat)
	}
}

func TestTotal(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Add(200, 2000)
	tt.Add(300, 500)

	if tt.Total() != 3500 {
		t.Fatalf("expected total threat 3500, got %d", tt.Total())
	}
}

func TestSetDecayRate(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.SetDecayRate(800)
	if tt.decayRate != 800 {
		t.Fatalf("expected decay rate 800, got %d", tt.decayRate)
	}

	// Invalid value should be rejected
	tt.SetDecayRate(200)
	if tt.decayRate == 200 {
		t.Fatal("expected invalid decay rate 200 to be rejected")
	}
}

func TestFullWipe(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	tt.Add(100, 1000)
	tt.Add(200, 2000)
	tt.FullWipe()

	if tt.Len() != 0 {
		t.Fatal("expected empty table after full wipe")
	}
	if tt.Total() != 0 {
		t.Fatal("expected 0 total threat after full wipe")
	}
}

func TestMaxPlayers(t *testing.T) {
	tt := NewThreatTable()
	defer tt.Close()

	// Add MaxPlayers entries
	for i := range MaxPlayers {
		tt.Add(uint64(i+1), int64((i+1)*100))
	}

	if tt.Len() != MaxPlayers {
		t.Fatalf("expected %d entries, got %d", MaxPlayers, tt.Len())
	}

	// Next add should be dropped
	tt.Add(999, 9999)
	if tt.Len() != MaxPlayers {
		t.Fatalf("expected still %d entries after overflow, got %d", MaxPlayers, tt.Len())
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkThreatAdd(b *testing.B) {
	tt := NewThreatTable()
	defer tt.Close()

	b.ResetTimer()
	for i := range b.N {
		tt.Add(uint64(i%64+1), 1000)
	}
}

func BenchmarkThreatTop(b *testing.B) {
	tt := NewThreatTable()
	defer tt.Close()

	for i := range 10 {
		tt.Add(uint64(i+1), int64((i+1)*100))
	}

	b.ResetTimer()
	for range b.N {
		tt.Top()
	}
}

func BenchmarkThreatDecay(b *testing.B) {
	tt := NewThreatTable()
	defer tt.Close()

	for i := range 10 {
		tt.Add(uint64(i+1), int64((i+1)*100))
	}

	b.ResetTimer()
	for range b.N {
		tt.Decay()
	}
}

func BenchmarkThreatRemove(b *testing.B) {
	tt := NewThreatTable()
	defer tt.Close()

	for i := range 10 {
		tt.Add(uint64(i+1), int64((i+1)*100))
	}

	b.ResetTimer()
	for range b.N {
		tt.Remove(uint64(b.N%10 + 1))
	}
}
