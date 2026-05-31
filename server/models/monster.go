package models

import (
	"encoding/json"
	"fmt"
	"os"
	"server/ecs"
)

// Global tracker to ensure every spawned creature gets a distinct row anchor
var nextMonsterInstanceID = 1000

// MonsterTemplate defines the raw template data loaded from configuration.
type MonsterTemplate struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	HP   int    `json:"hp"`
	Dam  int    `json:"damage"`
}

// CreateMonsterEntity registers a monster template as an Entity in ECS.
//
// Parameters:
//   - m: The raw MonsterTemplate data.
//
// Returns:
//   - The newly registered ecs.Entity ID.
func CreateMonsterEntity(m MonsterTemplate) ecs.Entity {
	entityID := ecs.Entity(m.ID)

	ecs.GlobalRegistry.SetMetadata(entityID, ecs.MetadataComponent{Name: m.Name, Type: "monster_template"})
	ecs.GlobalRegistry.SetStats(entityID, ecs.StatsComponent{HP: m.HP, Dam: m.Dam})

	return entityID
}

// SpawnMonsterFromTemplate instantiates a live monster on the map using a registered monster template.
// It retrieves the template's properties from the ECS registry, generates a unique instance ID,
// and attaches the corresponding components (Position, Metadata, Stats) to the new live entity.
//
// Parameters:
//   - templateID: The unique ID identifying the static monster template.
//   - spawnX: The initial X coordinate where the monster instance will spawn.
//   - spawnZ: The initial Z coordinate where the monster instance will spawn.
//
// Returns:
//   - The registered live monster instance ecs.Entity ID.
//   - An error if the specified templateID is not found in the ECS registry.
func SpawnMonsterFromTemplate(templateID int, spawnX int, spawnZ int) (ecs.Entity, error) {
	// Look up the static read-only JSON profile registered in ecs.GlobalRegistry
	templateEntity := ecs.Entity(templateID)
	templateMeta, metaOk := ecs.GlobalRegistry.GetMetadata(templateEntity)
	templateStats, statsOk := ecs.GlobalRegistry.GetStats(templateEntity)

	if !metaOk || !statsOk {
		return 0, fmt.Errorf("failed to spawn: template ID %d not found", templateID)
	}

	// 1. Generate the unique Entity ID atomically using Registry
	entityID := ecs.GlobalRegistry.NewEntity()

	// 2. Populate columns using Registry helper tools with inline values
	ecs.GlobalRegistry.SetMetadata(entityID, ecs.MetadataComponent{
		Name: templateMeta.Name,
		Type: "monster",
	})

	ecs.GlobalRegistry.SetPosition(entityID, ecs.PositionComponent{
		X: spawnX,
		Z: spawnZ,
	})

	ecs.GlobalRegistry.SetStats(entityID, ecs.StatsComponent{
		HP:  templateStats.HP, // Unique current HP instance tracking
		Dam: templateStats.Dam,
	})

	fmt.Printf("[ECS ENGINE] Row entry %d (%s) initialized at X:%d, Z:%d\n",
		entityID, templateMeta.Name, spawnX, spawnZ)

	return entityID, nil
}

// LoadMonster loads the list of template monsters from a pre-formatted JSON file.
//
// Parameters:
//   - filePath: The relative or absolute path to the JSON configuration file.
//
// Returns:
//   - A slice of MonsterTemplate if loaded successfully, or an error.
func LoadMonster(filePath string) ([]MonsterTemplate, error) {
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read json file: %v", err)
	}

	var temporaryList []MonsterTemplate
	err = json.Unmarshal(fileBytes, &temporaryList)
	if err != nil {
		return nil, fmt.Errorf("failed to parse json data: %v", err)
	}

	return temporaryList, nil
}
