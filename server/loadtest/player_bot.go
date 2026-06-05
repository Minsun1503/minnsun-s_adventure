package loadtest

import (
	"fmt"
	"server/ecs"
	"server/peakgo/rng"
	"server/world"
)

// ─── Dummy Player Bot ─────────────────────────────────────────────────────────
//
// PlayerBot simulates a real player entity living inside the ECS registry.
// It has all the components a real player has (Position, Stats, Metadata, etc.)
// but no network connection. Movement and combat are driven by the LoadTest
// harness, not by a real client.

// PlayerBotState holds the live bot tracking state.
type PlayerBotState struct {
	ID         ecs.Entity
	Name       string
	MapID      int
	X, Z       int
	Alive      bool
	TargetID   ecs.Entity // monster this bot is attacking, or 0
	NextAction uint64     // tick at which this bot may act again
}

// playerBots is the global slice of active player bots.
// Accessed only from the load test tick goroutine — no lock needed.
var playerBots []*PlayerBotState

// SpawnPlayerBot creates a new dummy player entity in the ECS registry.
// Returns a PlayerBotState that the load test harness uses to drive simulation.
func SpawnPlayerBot(name string, mapID, x, z int) *PlayerBotState {
	id := ecs.DefaultRegistry.NewEntity()

	ecs.DefaultRegistry.SetMetadata(id, ecs.MetadataComponent{
		Name: name,
		Type: ecs.EntityPlayer,
	})
	ecs.DefaultRegistry.SetPosition(id, ecs.PositionComponent{
		MapID: mapID,
		X:     x,
		Z:     z,
	})
	ecs.DefaultRegistry.SetStats(id, ecs.StatsComponent{
		Level:     10,
		HP:        500,
		MaxHP:     500,
		MP:        200,
		MaxMP:     200,
		Attack:    50,
		Defense:   20,
		HitRate:   800,
		DodgeRate: 50,
		CritRate:  50,
	})
	ecs.DefaultRegistry.SetInventory(id, ecs.InventoryComponent{
		Items: make(map[uint64]int),
	})

	// Register in spatial grid so proximity queries work.
	pos := ecs.PositionComponent{MapID: mapID, X: x, Z: z}
	world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)

	// Register as AOI watcher so AOI works for this bot.
	world.RegisterPlayerAOI(id)

	bot := &PlayerBotState{
		ID:    id,
		Name:  name,
		MapID: mapID,
		X:     x,
		Z:     z,
		Alive: true,
	}
	playerBots = append(playerBots, bot)
	return bot
}

// DespawnPlayerBot removes a bot from the ECS registry and cleans up.
func DespawnPlayerBot(bot *PlayerBotState) {
	world.GlobalSpatialGrid.RemoveEntity(bot.ID)
	world.UnregisterPlayerAOI(bot.ID)
	ecs.DefaultRegistry.RemoveEntity(bot.ID)
	bot.Alive = false
}

// SpawnNBots creates N player bots at random positions on the given map.
// Returns the slice of spawned bots for tracking.
func SpawnNBots(n int, mapID int) []*PlayerBotState {
	bots := make([]*PlayerBotState, 0, n)
	for i := 0; i < n; i++ {
		x := 10 + rng.Intn(80) // keep within map bounds [10, 90]
		z := 10 + rng.Intn(80)
		name := fmt.Sprintf("Bot_%d_%d", mapID, i)
		bot := SpawnPlayerBot(name, mapID, x, z)
		bots = append(bots, bot)
		playerBots = append(playerBots, bot)
	}
	return bots
}

// ActiveBotCount returns the number of currently alive player bots.
func ActiveBotCount() int {
	count := 0
	for _, b := range playerBots {
		if b.Alive {
			count++
		}
	}
	return count
}
