package ecs

import "time"

// LifetimeComponent tracks temporary entities that should auto-despawn.
type LifetimeComponent struct {
	SpawnedAt time.Time     // The exact moment the item hit the floor
	Duration  time.Duration // How long it is allowed to live (e.g., 60 * time.Second)
}
