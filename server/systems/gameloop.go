package systems

import (
	"fmt"
	"server/ecs"
	"time"
)

func StartGameLoop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	go func() {
		fmt.Println("[ENGINE] Heartbeat game loop started at 4 ticks/sec.")
		for range ticker.C {
			tickWorld()
		}
	}()
}

// tickWorld does a single metadata scan per tick.
// hasPlayers and monster processing happen in the same pass — no double scan.
func tickWorld() {
	hasPlayers := false

	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		switch snap.Meta.Type {
		case "player":
			hasPlayers = true
		case "monster":
			processMonster(snap)
		}
		return true // always full scan; early-exit only if you add a sleep gate
	})

	if !hasPlayers {
		// MAP SLEEP TRICK: no players → skip heavy logic next tick
		// (you can set a flag here to gate UpdateWorldEntitiesSystem)
	}
}

func processMonster(snap ecs.EntitySnapshot) {
	if !snap.HasPos || !snap.HasStats {
		return
	}
	fmt.Printf("[SYSTEM MONITOR] Active Instance ID: %d | Type: %s | Position: (%d, %d) | HP: %d\n",
		snap.ID, snap.Meta.Name, snap.Pos.X, snap.Pos.Z, snap.Stats.HP)
}

// UpdateWorldEntitiesSystem — kept for external callers, now zero double-lookup.
func UpdateWorldEntitiesSystem() {
	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		if snap.Meta.Type != "monster" || !snap.HasPos || !snap.HasStats {
			return true
		}
		processMonster(snap)
		return true
	})
}
