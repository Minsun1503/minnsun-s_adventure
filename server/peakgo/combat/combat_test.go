package combat

import (
	"testing"
)

func TestNewStatsDefaults(t *testing.T) {
	s := NewStats(10)
	if s.Level != 10 {
		t.Fatalf("expected level 10, got %d", s.Level)
	}
	if s.Attack != 30 { // 10 + 10*2
		t.Fatalf("expected attack 30, got %d", s.Attack)
	}
	if s.Defense != 15 { // 5 + 10
		t.Fatalf("expected defense 15, got %d", s.Defense)
	}
}

func TestResolvePhysicalHit(t *testing.T) {
	attacker := NewStats(50)
	defender := NewStats(50)

	result := ResolvePhysical(&attacker, &defender, DamageModifiers{
		IsGuaranteed:    true, // Force hit
		SkillMultiplier: 1000,
	})

	if !result.Hit {
		t.Fatal("expected hit")
	}
	if result.Dodged {
		t.Fatal("expected not dodged")
	}
	if result.DamageDealt < 1 {
		t.Fatal("expected damage > 0")
	}
	if result.RawDamage <= result.Mitigated {
		t.Fatal("expected raw > mitigated")
	}
}

func TestResolvePhysicalCrit(t *testing.T) {
	attacker := NewStats(50)
	defender := NewStats(50)

	result := ResolvePhysical(&attacker, &defender, DamageModifiers{
		IsCrit:       true,
		IsGuaranteed: true,
	})

	if !result.IsCrit {
		t.Fatal("expected crit")
	}
	if result.DamageDealt <= 0 {
		t.Fatal("expected crit damage > 0")
	}
}

func TestResolvePhysicalGuaranteedHit(t *testing.T) {
	// Low level attacker vs high level defender should still hit
	attacker := NewStats(1)
	defender := NewStats(99)

	result := ResolvePhysical(&attacker, &defender, DamageModifiers{
		IsGuaranteed:    true,
		SkillMultiplier: 1000,
	})

	if !result.Hit {
		t.Fatal("expected guaranteed hit")
	}
}

func TestResolvePhysicalDodgePossible(t *testing.T) {
	attacker := Stats{
		Attack: 50, HitRate: 100, // 10% hit chance
	}
	defender := Stats{
		Defense:   10,
		DodgeRate: 901, // Defender has 90.1% dodge
	}

	// Test multiple times, at least some should miss
	dodged := false
	hit := false
	for range 100 {
		result := ResolvePhysical(&attacker, &defender, DamageModifiers{
			SkillMultiplier: 1000,
		})
		if result.Dodged {
			dodged = true
		}
		if result.Hit {
			hit = true
		}
	}

	if !dodged {
		t.Fatal("expected at least one dodge with low hit rate")
	}
	if !hit {
		t.Fatal("expected at least one hit even with low hit rate")
	}
}

func TestResolveMagical(t *testing.T) {
	attacker := NewStats(50)
	defender := NewStats(50)

	result := ResolveMagical(&attacker, &defender, DamageModifiers{
		IsGuaranteed:    true,
		SkillMultiplier: 1000,
	})

	if !result.Hit {
		t.Fatal("expected hit")
	}
	if result.DamageDealt < 1 {
		t.Fatal("expected damage > 0")
	}
}

func TestResolvePure(t *testing.T) {
	attacker := NewStats(50)
	result := ResolvePure(&attacker, DamageModifiers{
		SkillMultiplier: 1000,
	})

	if !result.Hit {
		t.Fatal("expected hit")
	}
	if result.DamageDealt < 1 {
		t.Fatal("expected damage > 0")
	}
	// Pure damage has no mitigation
	if result.Mitigated != 0 {
		t.Fatalf("expected 0 mitigation for pure damage, got %d", result.Mitigated)
	}
}

func TestElementEffectiveness(t *testing.T) {
	// Fire -> Water: 2x (2000)
	mult := GetElementEffectiveness(ElementFire, ElementWater)
	if mult != 2000 {
		t.Fatalf("expected Fire->Water = 2000, got %d", mult)
	}

	// Fire -> Fire: 0.5x (500)
	mult = GetElementEffectiveness(ElementFire, ElementFire)
	if mult != 500 {
		t.Fatalf("expected Fire->Fire = 500, got %d", mult)
	}

	// None -> anything: 1x
	mult = GetElementEffectiveness(ElementNone, ElementFire)
	if mult != 1000 {
		t.Fatalf("expected None->Fire = 1000, got %d", mult)
	}
}

func TestDoTInstance(t *testing.T) {
	dot := DoTInstance{
		DamagePerTick:  10,
		RemainingTicks: 5,
	}

	for i := range 5 {
		dmg := dot.Tick()
		if dmg != 10 {
			t.Fatalf("expected tick %d damage 10, got %d", i, dmg)
		}
	}

	if !dot.Expired() {
		t.Fatal("expected DoT expired after 5 ticks")
	}

	// Tick after expiry should return 0
	if dot.Tick() != 0 {
		t.Fatal("expected 0 damage after expiry")
	}
}

func TestCalculateHealing(t *testing.T) {
	heal := CalculateHealing(50, 100, 1000, false)
	if heal.Amount != 100 {
		t.Fatalf("expected heal 100, got %d", heal.Amount)
	}
	if heal.IsCrit {
		t.Fatal("expected no crit")
	}
}

func TestApplyHealing(t *testing.T) {
	heal := HealResult{Amount: 50}
	newHP, overheal := ApplyHealing(80, 100, heal)
	if newHP != 100 {
		t.Fatalf("expected HP 100 (capped at max), got %d", newHP)
	}
	if overheal != 30 {
		t.Fatalf("expected overheal 30, got %d", overheal)
	}
}

func TestApplyHealingNoOverheal(t *testing.T) {
	heal := HealResult{Amount: 30}
	newHP, overheal := ApplyHealing(50, 100, heal)
	if newHP != 80 {
		t.Fatalf("expected HP 80, got %d", newHP)
	}
	if overheal != 0 {
		t.Fatalf("expected 0 overheal, got %d", overheal)
	}
}

func TestCombatResultValueType(t *testing.T) {
	// Verify CombatResult is returned by value (no heap allocation)
	attacker := NewStats(10)
	defender := NewStats(10)

	result := ResolvePhysical(&attacker, &defender, DamageModifiers{
		IsGuaranteed: true,
	})

	// Modify the copy
	result.DamageDealt = 999

	// Call again, should not be affected
	result2 := ResolvePhysical(&attacker, &defender, DamageModifiers{
		IsGuaranteed: true,
	})

	if result2.DamageDealt == 999 {
		t.Fatal("CombatResult should be a value type, not shared reference")
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkResolvePhysical(b *testing.B) {
	attacker := NewStats(50)
	defender := NewStats(50)
	mods := DamageModifiers{
		IsGuaranteed:    true,
		SkillMultiplier: 1000,
	}
	b.ResetTimer()
	for range b.N {
		ResolvePhysical(&attacker, &defender, mods)
	}
}

func BenchmarkResolvePhysicalWithRng(b *testing.B) {
	attacker := NewStats(50)
	defender := NewStats(50)
	mods := DamageModifiers{
		SkillMultiplier: 1000,
	}
	b.ResetTimer()
	for range b.N {
		ResolvePhysical(&attacker, &defender, mods)
	}
}

func BenchmarkResolveMagical(b *testing.B) {
	attacker := NewStats(50)
	defender := NewStats(50)
	mods := DamageModifiers{
		IsGuaranteed:    true,
		SkillMultiplier: 1000,
	}
	b.ResetTimer()
	for range b.N {
		ResolveMagical(&attacker, &defender, mods)
	}
}

func BenchmarkDoTTick(b *testing.B) {
	dot := DoTInstance{DamagePerTick: 10, RemainingTicks: 100}
	b.ResetTimer()
	for range b.N {
		dot.Tick()
	}
}
