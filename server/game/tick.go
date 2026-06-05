package game

// globalTick is the current game loop tick, set by systems/gameloop.go
// before each per-map tick. Game systems use GetCurrentTick() instead of
// time.Now() for drift-free timing.
var globalTick uint64

// GetCurrentTick returns the current game loop tick count.
// Set by the game loop in systems/gameloop.go before each tick dispatch.
func GetCurrentTick() uint64 {
	return globalTick
}

// SetCurrentTick updates the global tick counter from the game loop.
// Called by perMapTick before running game systems.
func SetCurrentTick(tick uint64) {
	globalTick = tick
}
