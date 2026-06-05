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
package timer

import (
	"fmt"
	"time"
)

// ─── TickTimer ────────────────────────────────────────────────────────────────

// TickTimer counts game-loop ticks and fires when a cooldown is reached.
// Stored as an inline value — copy-modify-overwrite like all ECS components.
type TickTimer struct {
	Cur      int // Ticks elapsed since last reset
	Cooldown int // Ticks required before Ready() returns true
}

// NewTickTimer creates a TickTimer with the given cooldown duration (in ticks).
// Đã sửa: Tự động đưa cooldown về 1 nếu giá trị truyền vào nhỏ hơn 1 để tránh lỗi Ready liên tục.
func NewTickTimer(cooldown int) TickTimer {
	if cooldown < 1 {
		cooldown = 1
	}
	return TickTimer{Cooldown: cooldown}
}

// Advance increments the internal tick counter by one.
// Call once per game-loop tick for every entity owning this timer.
func (t *TickTimer) Advance() {
	t.Cur++
}

// Ready reports whether the cooldown has been reached.
// Does NOT reset — call Reset() explicitly to restart the cooldown.
func (t *TickTimer) Ready() bool {
	return t.Cur >= t.Cooldown
}

// AdvanceAndCheck advances the counter and returns true if the cooldown is met.
// Convenience method for the common advance-then-check pattern.
// Does NOT reset on true — caller must call Reset() if they want to fire once.
func (t *TickTimer) AdvanceAndCheck() bool {
	t.Cur++
	return t.Cur >= t.Cooldown
}

// Tick là hàm tối ưu tối đa cho logic lặp của Game Server.
// Hàm tự động tăng 1 tick, kiểm tra trạng thái và tự động RESET counter về 0 nếu Ready.
//
// Ví dụ sử dụng:
//
//	if ai.AttackTimer.Tick() {
//	    ai.FireAttack() // Không cần gọi Reset() thủ công nữa
//	}
func (t *TickTimer) Tick() bool {
	t.Cur++
	if t.Cur < t.Cooldown {
		return false
	}
	t.Cur = 0 // Tự động reset bộ đếm
	return true
}

// Reset restarts the cooldown from zero.
func (t *TickTimer) Reset() {
	t.Cur = 0
}

// SetCooldown changes the cooldown duration without resetting the current counter.
// Đã sửa: Bảo vệ cấu trúc logic, ép giá trị cooldown tối thiểu luôn bằng 1 tick.
func (t *TickTimer) SetCooldown(cooldown int) {
	if cooldown < 1 {
		cooldown = 1
	}
	t.Cooldown = cooldown
}

// Cooldown returns the current target cooldown duration in ticks.
func (t *TickTimer) GetCooldown() int {
	return t.Cooldown
}

// Elapsed returns the number of ticks elapsed since the last reset.
func (t *TickTimer) Elapsed() int {
	return t.Cur
}

// Remaining returns how many more ticks until Ready() fires.
// Đã sửa: Sử dụng toán tử `<=` rõ ràng để tối ưu hóa điều kiện biên.
func (t *TickTimer) Remaining() int {
	r := t.Cooldown - t.Cur
	if r <= 0 {
		return 0
	}
	return r
}

// Progress returns the completion ratio from 0.0 (started) to 1.0 (ready).
// Highly useful for UI progression bars, action bars, or AI weight evaluations.
func (t *TickTimer) Progress() float64 {
	if t.Cooldown <= 0 {
		return 1.0
	}
	p := float64(t.Cur) / float64(t.Cooldown)
	if p > 1.0 {
		return 1.0
	}
	return p
}

// ─── WallTimer ────────────────────────────────────────────────────────────────

// WallTimer tracks a real-world deadline using time.Time.
// Stored as an inline value — copy-modify-overwrite like all ECS components.
type WallTimer struct {
	ExpiresAtTime time.Time
}

// NewWallTimer creates a WallTimer that fires after the given duration from now.
func NewWallTimer(d time.Duration) WallTimer {
	return WallTimer{ExpiresAtTime: time.Now().Add(d)}
}

// NewWallTimerAt creates a WallTimer with an explicit expiry time.
// Use when the deadline is computed externally (e.g. loaded from DB).
func NewWallTimerAt(t time.Time) WallTimer {
	return WallTimer{ExpiresAtTime: t}
}

// IsZero reports whether the timer is uninitialized (has no expiration set).
func (w *WallTimer) IsZero() bool {
	return w.ExpiresAtTime.IsZero()
}

// Done reports whether the deadline has passed.
// Đã sửa: Thay After bằng `!Before` để tính cả trường hợp thời gian hiện tại trùng khít với deadline.
func (w *WallTimer) Done() bool {
	return !time.Now().Before(w.ExpiresAtTime)
}

// DoneAt reports whether the deadline has passed relative to a provided time.
// Use in tight loops where time.Now() should be called once outside the loop.
// Đã sửa: Đã sửa lỗi điều kiện biên tương tự hàm Done().
func (w *WallTimer) DoneAt(now time.Time) bool {
	return !now.Before(w.ExpiresAtTime)
}

// Remaining returns how much time is left until the deadline.
// Returns 0 if the timer has already expired.
func (w *WallTimer) Remaining() time.Duration {
	r := time.Until(w.ExpiresAtTime)
	if r < 0 {
		return 0
	}
	return r
}

// ExpiredDuration reports how much time has passed since the timer expired.
// Returns 0 if the timer is still active or uninitialized.
func (w *WallTimer) ExpiredDuration() time.Duration {
	if w.ExpiresAtTime.IsZero() {
		return 0
	}
	r := time.Since(w.ExpiresAtTime)
	if r < 0 {
		return 0
	}
	return r
}

// ExpiresAt returns the raw deadline time.Time.
// Useful for serialization (e.g. storing respawn time in a queue).
func (w *WallTimer) ExpiresAt() time.Time {
	return w.ExpiresAtTime
}

// Refresh ghi đè thời hạn mới tính từ thời điểm hiện tại (Now + d).
// Đã sửa: Đổi tên từ Extend sang Refresh để phản ánh đúng chính xác bản chất thay thế mốc thời gian.
func (w *WallTimer) Refresh(d time.Duration) {
	w.ExpiresAtTime = time.Now().Add(d)
}

// ExtendFrom pushes the deadline forward by d from the current expiry.
// Use when stacking a buff on top of an existing one without losing remaining time.
func (w *WallTimer) ExtendFrom(d time.Duration) {
	w.ExpiresAtTime = w.ExpiresAtTime.Add(d)
}

// String implements the fmt.Stringer interface. Making debugging and logging trivial.
func (w WallTimer) String() string {
	if w.IsZero() {
		return "WallTimer(uninitialized)"
	}
	r := time.Until(w.ExpiresAtTime)
	if r <= 0 {
		return "WallTimer(expired)"
	}
	return fmt.Sprintf("WallTimer(expires in %v)", r.Truncate(time.Millisecond))
}
