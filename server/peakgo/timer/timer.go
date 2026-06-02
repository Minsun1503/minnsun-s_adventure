// Package timer provides two zero-footgun timer primitives for the
// Minnsun's Adventure game server.
//
// # Why two types
//
// The server has two fundamentally different timing domains:
//
//  1. Game-loop domain: AI cooldowns, idle durations — things that only
//     matter while the game loop is ticking. Measuring these in wall-clock
//     time adds unnecessary drift; counting ticks is simpler, faster, and
//     perfectly accurate for the 250ms tick budget.
//
//  2. Wall-clock domain: respawn delays, buff durations, invite expiry,
//     floor-item lifetime — things that must fire at a real-world time
//     regardless of tick rate or server pause. These need time.Time.
//
// # Current codebase pain points this package resolves
//
// Tick pattern duplicated in every AIComponent:
//
//	ai.AttackTick++
//	if ai.AttackTick >= ai.AttackCooldown { ai.AttackTick = 0; ... }
//
// Wall-clock pattern written four different ways:
//
//	respawn.go:       time.Now().After(ev.RespawnAt)
//	party_invite.go:  time.Now().After(record.ExpiresAt)
//	ground_item.go:   time.Since(lt.SpawnedAt) >= lt.Duration
//	effects_system.go: effect.Duration -= tickInterval; effect.Duration <= 0
//
// # Peak Go contract
//
//	TickTimer.Tick()  → 0 allocs/op (pure int arithmetic)
//	WallTimer.Done()  → 0 allocs/op (time.Time comparison)
package timer

import "time"

// ─── TickTimer ────────────────────────────────────────────────────────────────

// TickTimer counts game-loop ticks and fires when a cooldown is reached.
// Stored as an inline value — copy-modify-overwrite like all ECS components.
//
// Typical usage inside a game-loop system:
//
//	ai.AttackTimer.Advance()
//	if ai.AttackTimer.Ready() {
//	    ai.AttackTimer.Reset()
//	    // fire attack
//	}
//
// This replaces the manual pattern:
//
//	ai.AttackTick++
//	if ai.AttackTick >= ai.AttackCooldown { ai.AttackTick = 0; ... }
type TickTimer struct {
	cur      int // ticks elapsed since last reset
	cooldown int // ticks required before Ready() returns true
}

// NewTickTimer creates a TickTimer with the given cooldown duration (in ticks).
// A cooldown of 4 at 250ms/tick means the timer fires once per second.
func NewTickTimer(cooldown int) TickTimer {
	return TickTimer{cooldown: cooldown}
}

// Advance increments the internal tick counter by one.
// Call once per game-loop tick for every entity owning this timer.
func (t *TickTimer) Advance() {
	t.cur++
}

// Ready reports whether the cooldown has been reached.
// Does NOT reset — call Reset() explicitly to restart the cooldown.
func (t *TickTimer) Ready() bool {
	return t.cur >= t.cooldown
}

// AdvanceAndCheck advances the counter and returns true if the cooldown is met.
// Convenience method for the common advance-then-check pattern.
// Does NOT reset on true — caller must call Reset() if they want to fire once.
func (t *TickTimer) AdvanceAndCheck() bool {
	t.cur++
	return t.cur >= t.cooldown
}

// Reset restarts the cooldown from zero.
func (t *TickTimer) Reset() {
	t.cur = 0
}

// SetCooldown changes the cooldown duration without resetting the current counter.
// Use this when a buff or equipment change alters attack speed mid-combat.
func (t *TickTimer) SetCooldown(cooldown int) {
	t.cooldown = cooldown
}

// Elapsed returns the number of ticks elapsed since the last reset.
func (t *TickTimer) Elapsed() int {
	return t.cur
}

// Remaining returns how many more ticks until Ready() fires.
// Returns 0 if the timer is already ready.
func (t *TickTimer) Remaining() int {
	r := t.cooldown - t.cur
	if r < 0 {
		return 0
	}
	return r
}

// ─── WallTimer ────────────────────────────────────────────────────────────────

// WallTimer tracks a real-world deadline using time.Time.
// Stored as an inline value — copy-modify-overwrite like all ECS components.
//
// Typical usage:
//
//	// On spawn:
//	item.Lifetime = timer.NewWallTimer(60 * time.Second)
//
//	// On tick:
//	if item.Lifetime.Done() { despawn(item) }
//
// This replaces the four different wall-clock patterns currently spread across:
//   - respawn.go      (RespawnAt  time.Time)
//   - party_invite.go (ExpiresAt  time.Time)
//   - ground_item.go  (SpawnedAt  time.Time + Duration)
//   - effects_system.go (Duration time.Duration decremented per tick)
type WallTimer struct {
	expiresAt time.Time
}

// NewWallTimer creates a WallTimer that fires after the given duration from now.
func NewWallTimer(d time.Duration) WallTimer {
	return WallTimer{expiresAt: time.Now().Add(d)}
}

// NewWallTimerAt creates a WallTimer with an explicit expiry time.
// Use when the deadline is computed externally (e.g. loaded from DB).
func NewWallTimerAt(t time.Time) WallTimer {
	return WallTimer{expiresAt: t}
}

// Done reports whether the deadline has passed.
// Calls time.Now() once — use a cached now for hot loops.
func (w *WallTimer) Done() bool {
	return time.Now().After(w.expiresAt)
}

// DoneAt reports whether the deadline has passed relative to a provided time.
// Use in tight loops where time.Now() should be called once outside the loop:
//
//	now := time.Now()
//	for _, item := range items {
//	    if item.Lifetime.DoneAt(now) { despawn(item) }
//	}
func (w *WallTimer) DoneAt(now time.Time) bool {
	return now.After(w.expiresAt)
}

// Remaining returns how much time is left until the deadline.
// Returns 0 if the timer has already expired.
func (w *WallTimer) Remaining() time.Duration {
	r := time.Until(w.expiresAt)
	if r < 0 {
		return 0
	}
	return r
}

// ExpiresAt returns the raw deadline time.Time.
// Useful for serialization (e.g. storing respawn time in a queue).
func (w *WallTimer) ExpiresAt() time.Time {
	return w.expiresAt
}

// Extend pushes the deadline forward by the given duration from now.
// Use for refreshable buffs or re-invitations.
func (w *WallTimer) Extend(d time.Duration) {
	w.expiresAt = time.Now().Add(d)
}

// ExtendFrom pushes the deadline forward by d from the current expiry.
// Use when stacking a buff on top of an existing one without losing remaining time.
func (w *WallTimer) ExtendFrom(d time.Duration) {
	w.expiresAt = w.expiresAt.Add(d)
}
