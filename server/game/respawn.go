package game

import (
	"fmt"
	"server/ecs"
	"server/logger"
	"server/models"
	"server/world"
	"sync"
)

// RespawnEvent holds the data needed to respawn a monster after a delay.
type RespawnEvent struct {
	TemplateID int
	MapID      int
	SpawnX     int
	SpawnZ     int
	RespawnAt  uint64 // Tick when respawn should fire
}

// RespawnScheduler manages a thread-safe queue of pending respawn events.
type RespawnScheduler struct {
	mu     sync.Mutex
	events []RespawnEvent
}

// GlobalRespawnManager is the singleton respawn scheduler.
var GlobalRespawnManager = &RespawnScheduler{}

// ScheduleMonsterRespawn queues a monster for respawn after the given delay in ticks.
// At 4 ticks/sec, delay is measured in ticks (e.g., 60 ticks = 15 seconds).
func (rs *RespawnScheduler) ScheduleMonsterRespawn(templateID, mapID, spawnX, spawnZ int, delay uint64) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.events = append(rs.events, RespawnEvent{
		TemplateID: templateID,
		MapID:      mapID,
		SpawnX:     spawnX,
		SpawnZ:     spawnZ,
		RespawnAt:  GetCurrentTick() + delay,
	})
	logger.Info("[RESPAWN] Scheduled %s (template %d) at (%d,%d) in %d ticks",
		monsterName(templateID), templateID, spawnX, spawnZ, delay)
}

// RunRespawnSystem checks all pending events and spawns any whose timer has expired.
// Called once per game-loop tick.
func (rs *RespawnScheduler) RunRespawnSystem() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if len(rs.events) == 0 {
		return
	}

	currentTick := GetCurrentTick()
	remaining := rs.events[:0] // reuse backing array

	for _, ev := range rs.events {
		if currentTick >= ev.RespawnAt {
			// Timer expired — spawn the monster on the correct map.
			id, err := models.SpawnMonsterFromTemplate(ev.TemplateID, ev.MapID, ev.SpawnX, ev.SpawnZ)
			if err != nil {
				logger.Error("[RESPAWN] Failed to spawn template %d: %v", ev.TemplateID, err)
				remaining = append(remaining, ev) // Keep in queue to retry next tick
				continue
			}
			// Register in the spatial grid so AI systems can find it.
			if pos, ok := ecs.DefaultRegistry.GetPosition(id); ok {
				world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
			}
			logger.Info("[RESPAWN] Spawned %s (entity %d) at map=%d (%d,%d)",
				monsterName(ev.TemplateID), id, ev.MapID, ev.SpawnX, ev.SpawnZ)
		} else {
			// Not yet — keep in the queue.
			remaining = append(remaining, ev)
		}
	}

	rs.events = remaining
}

// monsterName returns a human-readable name for a template ID.
func monsterName(templateID int) string {
	if t, ok := models.GetTemplate(templateID); ok {
		return t.Name
	}
	return fmt.Sprintf("unknown_template_%d", templateID)
}
