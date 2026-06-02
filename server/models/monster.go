package models

import (
	"encoding/json"
	"fmt"
	"os"
	"server/ecs"
	"server/logger"
	"sync"
)

// MonsterTemplate defines the static read-only data loaded from JSON config.
// Templates are never registered as ECS entities — they live in templateStore
// and are copied into live instances via SpawnMonsterFromTemplate.
type MonsterTemplate struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	HP             int     `json:"hp"`
	Dam            int     `json:"damage"`
	MapID          int     `json:"map_id"`    // World map this monster belongs to. Defaults to 1 if omitted.
	SpawnX         int     `json:"spawn_x"`
	SpawnZ         int     `json:"spawn_z"`
	RoamRadius     int     `json:"roam_radius"`
	AggroRadius    float64 `json:"aggro_radius"`
	AttackCooldown int     `json:"attack_cooldown"`
	XPReward       uint64  `json:"xp_reward"`
}

// templateStore is the in-memory registry for static monster templates.
// Keyed by template ID from JSON. Separate from ECS to avoid ID collisions
// between template IDs (1, 2, 3...) and live entity IDs (atomic counter).
var (
	templateStore   = make(map[int]MonsterTemplate)
	templateStoreMu sync.RWMutex
)

// LoadMonster reads monster templates from a JSON file and populates
// the in-memory templateStore. Call once at server boot before any spawns.
//
// Parameters:
//   - filePath: path to the JSON configuration file.
//
// Returns error if the file cannot be read or parsed.
func LoadMonster(filePath string) ([]MonsterTemplate, error) {
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read monster config: %w", err)
	}

	var list []MonsterTemplate
	if err := json.Unmarshal(fileBytes, &list); err != nil {
		return nil, fmt.Errorf("failed to parse monster config: %w", err)
	}

	// Populate the template store for SpawnMonsterFromTemplate lookups.
	// Apply default MapID=1 for templates that omit the field in JSON.
	templateStoreMu.Lock()
	for i := range list {
		if list[i].MapID == 0 {
			list[i].MapID = 1
		}
		templateStore[list[i].ID] = list[i]
	}
	templateStoreMu.Unlock()

	return list, nil
}

// GetTemplate returns a MonsterTemplate by its JSON ID.
// Returns false if the template has not been loaded.
func GetTemplate(templateID int) (MonsterTemplate, bool) {
	templateStoreMu.RLock()
	defer templateStoreMu.RUnlock()
	t, ok := templateStore[templateID]
	return t, ok
}

// SpawnMonsterFromTemplate instantiates a live monster entity from a loaded template.
// The template provides base stats and AI configuration; spawn coordinates
// and the target map can override the template's defaults for scripted placement.
//
// Parameters:
//   - templateID:    JSON template ID to copy stats from.
//   - mapID:         World map ID the monster should be placed on.
//   - spawnX, spawnZ: World coordinates for this instance.
//
// Returns the new ecs.Entity ID, or an error if the template is not found.
func SpawnMonsterFromTemplate(templateID, mapID, spawnX, spawnZ int) (ecs.Entity, error) {
	t, ok := GetTemplate(templateID)
	if !ok {
		return 0, fmt.Errorf("template ID %d not found — did you call LoadMonster?", templateID)
	}

	// Generate a unique entity ID from the atomic counter.
	// This is guaranteed not to collide with template IDs because
	// templates never enter the ECS registry.
	id := ecs.GlobalRegistry.NewEntity()

	ecs.GlobalRegistry.SetMetadata(id, ecs.MetadataComponent{
		Name: t.Name,
		Type: "monster",
	})

	spawnPos := ecs.PositionComponent{MapID: mapID, X: spawnX, Z: spawnZ}
	ecs.GlobalRegistry.SetPosition(id, spawnPos)

	ecs.GlobalRegistry.SetStats(id, ecs.StatsComponent{
		HP:    t.HP,
		MaxHP: t.HP,
		Dam:   t.Dam,
	})

	// Derive leash radius from aggro radius — always 2× so monsters
	// don't instantly give up but also don't chase indefinitely.
	leashRadius := t.AggroRadius * 2.0

	ecs.GlobalRegistry.SetAI(id, ecs.AIComponent{
		State:          ecs.AIStateIdle,
		SpawnX:         spawnX,
		SpawnZ:         spawnZ,
		SpawnRadius:    t.RoamRadius,
		AggroRadius:    t.AggroRadius,
		LeashRadius:    leashRadius,
		MeleeRange:     2.0, // melee is always 2 units; could be a template field later
		AttackCooldown: t.AttackCooldown,
		IdleDuration:   8, // 2 sec idle before roaming; same for all monsters for now
	})

	logger.Info("[SPAWN] %s (entity %d) at (%d, %d) | HP:%d ATK:%d aggro:%.0f leash:%.0f",
		t.Name, id, spawnX, spawnZ, t.HP, t.Dam, t.AggroRadius, leashRadius)

	return id, nil
}

// GetTemplateByName returns a MonsterTemplate matching the given name.
// Returns false if no template with that name has been loaded.
func GetTemplateByName(name string) (MonsterTemplate, bool) {
	templateStoreMu.RLock()
	defer templateStoreMu.RUnlock()
	for _, t := range templateStore {
		if t.Name == name {
			return t, true
		}
	}
	return MonsterTemplate{}, false
}

// SpawnFromDefaultPosition spawns a monster at its template-defined default coordinates
// and map. Convenience wrapper used during server boot when no override is needed.
func SpawnFromDefaultPosition(templateID int) (ecs.Entity, error) {
	t, ok := GetTemplate(templateID)
	if !ok {
		return 0, fmt.Errorf("template ID %d not found", templateID)
	}
	return SpawnMonsterFromTemplate(templateID, t.MapID, t.SpawnX, t.SpawnZ)
}
