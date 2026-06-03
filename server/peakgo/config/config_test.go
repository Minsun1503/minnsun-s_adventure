package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxMoveDistance != 2 {
		t.Fatalf("expected MaxMoveDistance=2, got %d", cfg.MaxMoveDistance)
	}
	if cfg.TickRateMS != 250 {
		t.Fatalf("expected TickRateMS=250, got %d", cfg.TickRateMS)
	}
	if cfg.BroadcastAOIRadius != 60.0 {
		t.Fatalf("expected BroadcastAOIRadius=60.0, got %f", cfg.BroadcastAOIRadius)
	}
}

func TestNewConfigManager_Defaults(t *testing.T) {
	cm := NewConfigManager("")
	cfg := cm.Get()
	if cfg.MaxMoveDistance != 2 {
		t.Fatalf("expected MaxMoveDistance=2 from defaults, got %d", cfg.MaxMoveDistance)
	}
	if cfg.TickRateDur == 0 {
		t.Fatal("expected TickRateDur to be computed")
	}
}

func TestNewConfigManager_FromFile(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "config*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer tmpFile.Close()

	content := `{"max_move_distance":5,"tick_rate_ms":100}`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cm := NewConfigManager(tmpFile.Name())
	cfg := cm.Get()
	if cfg.MaxMoveDistance != 5 {
		t.Fatalf("expected MaxMoveDistance=5, got %d", cfg.MaxMoveDistance)
	}
	if cfg.TickRateMS != 100 {
		t.Fatalf("expected TickRateMS=100, got %d", cfg.TickRateMS)
	}
	if cfg.TickRateDur == 0 {
		t.Fatal("expected TickRateDur to be computed")
	}
}

func TestReload(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "config*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer tmpFile.Close()

	content := `{"max_move_distance":3}`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cm := NewConfigManager(tmpFile.Name())
	cfg := cm.Get()
	if cfg.MaxMoveDistance != 3 {
		t.Fatalf("expected MaxMoveDistance=3, got %d", cfg.MaxMoveDistance)
	}

	// Rewrite file and reload
	newContent := `{"max_move_distance":7}`
	if err := os.WriteFile(tmpFile.Name(), []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := cm.Reload(); err != nil {
		t.Fatal(err)
	}
	cfg = cm.Get()
	if cfg.MaxMoveDistance != 7 {
		t.Fatalf("expected MaxMoveDistance=7 after reload, got %d", cfg.MaxMoveDistance)
	}
}

// BenchmarkGetConfig ensures zero-alloc hot-path read.
func BenchmarkGetConfig(b *testing.B) {
	cm := NewConfigManager("")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cfg := cm.Get()
		_ = cfg.MaxMoveDistance
	}
}
