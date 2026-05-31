package models

import (
	"encoding/json"
	"fmt"
	"os"
	"server/ecs"
	"strconv"
)

// MonsterTemplate defines the raw template data loaded from configuration.
type MonsterTemplate struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	HP   int    `json:"hp"`
	Dam  int    `json:"damage"`
}

var nextMonsterInstanceID = 1000

// CreateMonsterEntity registers a monster template as an Entity in ECS.
//
// Parameters:
//   - m: The raw MonsterTemplate data.
//
// Returns:
//   - The newly registered ecs.Entity ID.
func CreateMonsterEntity(m MonsterTemplate) ecs.Entity {
	entityID := ecs.Entity(strconv.Itoa(m.ID))

	ecs.GlobalRegistry.RegisterEntity(entityID)
	ecs.GlobalRegistry.SetMetadata(entityID, &ecs.MetadataComponent{Name: m.Name, Type: "monster_template"})
	ecs.GlobalRegistry.SetStats(entityID, &ecs.StatsComponent{HP: m.HP, Dam: m.Dam})

	return entityID
}

// SpawnMonsterInstance registers a live, active monster instance on the map in the ECS registry.
// It assigns unique ID, spawn coordinates, and attaches its components.
//
// Parameters:
//   - template: The base MonsterTemplate.
//   - instanceID: Unique instance ID for the live monster (e.g., 1001, 1002).
//   - x: Spawn X coordinate.
//   - z: Spawn Z coordinate.
//
// Returns:
//   - The registered active monster instance ecs.Entity ID.
func SpawnMonsterInstance(template MonsterTemplate, instanceID int, x, z int) ecs.Entity {
	entityID := ecs.Entity("monster_instance_" + strconv.Itoa(instanceID))

	ecs.GlobalRegistry.RegisterEntity(entityID)
	ecs.GlobalRegistry.SetPosition(entityID, &ecs.PositionComponent{X: x, Z: z})
	ecs.GlobalRegistry.SetMetadata(entityID, &ecs.MetadataComponent{Name: template.Name, Type: "monster"})
	ecs.GlobalRegistry.SetStats(entityID, &ecs.StatsComponent{HP: template.HP, Dam: template.Dam})

	return entityID
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
