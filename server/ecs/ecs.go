package ecs

import (
	"net"
	"server/state"
)

// Entity represents a unique identifier for any game object in the ECS architecture.
// Typically, it is derived from the player's TCP connection remote address (IP:port)
// or a string representation of static data IDs (e.g., monster template ID).
type Entity string

// PositionComponent stores spatial coordinates for an entity on the game map.
// The coordinates X and Z are represented as integers.
type PositionComponent struct {
	X int // Horizontal coordinate.
	Z int // Vertical/Depth coordinate.
}

// ConnectionComponent holds the active network socket link for a player entity.
// Systems can query this component to transmit packages or detect disconnection.
type ConnectionComponent struct {
	Conn net.Conn // The TCP socket connection.
}

// MetadataComponent holds identification attributes such as name and category.
// This is used to differentiate between players, monsters, or objects.
type MetadataComponent struct {
	Name string // The display name of the entity.
	Type string // The classification type (e.g., "player" or "monster").
}

// StatsComponent holds attributes related to combat, health, and damage.
type StatsComponent struct {
	HP  int // Hit points / Health points.
	Dam int // Attack damage power.
}

// Registry is the authoritative central database storing all Entities and their Components.
// It leverages state.SafeMap to ensure that all read and write component stores are thread-safe,
// allowing concurrent operations from multiple client goroutines.
type Registry struct {
	entities  *state.SafeMap[Entity, bool]                 // Set of all registered entities.
	positions *state.SafeMap[Entity, *PositionComponent]  // Map of entities to their spatial position component.
	conns     *state.SafeMap[Entity, *ConnectionComponent] // Map of entities to their active TCP network socket connection.
	metadata  *state.SafeMap[Entity, *MetadataComponent]   // Map of entities to their naming and category metadata.
	stats     *state.SafeMap[Entity, *StatsComponent]      // Map of entities to their health and combat statistics.
}

// GlobalRegistry is the main authoritative registry instance utilized by the game server.
var GlobalRegistry = NewRegistry()

// NewRegistry initializes and returns a new empty ECS registry with initialized safe component maps.
//
// Returns:
//   - A pointer to the newly allocated Registry instance.
func NewRegistry() *Registry {
	return &Registry{
		entities:  state.NewSafeMap[Entity, bool](),
		positions: state.NewSafeMap[Entity, *PositionComponent](),
		conns:     state.NewSafeMap[Entity, *ConnectionComponent](),
		metadata:  state.NewSafeMap[Entity, *MetadataComponent](),
		stats:     state.NewSafeMap[Entity, *StatsComponent](),
	}
}

// RegisterEntity registers a new Entity ID in the central registry system.
//
// Parameters:
//   - id: The unique Entity ID to register.
func (r *Registry) RegisterEntity(id Entity) {
	r.entities.Set(id, true)
}

// RemoveEntity removes an Entity ID from the registry and deletes all components associated with it.
//
// Parameters:
//   - id: The unique Entity ID to completely unregister and clean up.
func (r *Registry) RemoveEntity(id Entity) {
	r.entities.Delete(id)
	r.positions.Delete(id)
	r.conns.Delete(id)
	r.metadata.Delete(id)
	r.stats.Delete(id)
}

// SetPosition associates a PositionComponent with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//   - comp: A pointer to the PositionComponent to attach.
func (r *Registry) SetPosition(id Entity, comp *PositionComponent) {
	r.positions.Set(id, comp)
}

// GetPosition retrieves the PositionComponent associated with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//
// Returns:
//   - A pointer to the entity's PositionComponent, or nil if not found.
func (r *Registry) GetPosition(id Entity) *PositionComponent {
	return r.positions.Get(id)
}

// SetConnection associates a ConnectionComponent with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//   - comp: A pointer to the ConnectionComponent to attach.
func (r *Registry) SetConnection(id Entity, comp *ConnectionComponent) {
	r.conns.Set(id, comp)
}

// GetConnection retrieves the ConnectionComponent associated with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//
// Returns:
//   - A pointer to the entity's ConnectionComponent, or nil if not found.
func (r *Registry) GetConnection(id Entity) *ConnectionComponent {
	return r.conns.Get(id)
}

// SetMetadata associates a MetadataComponent with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//   - comp: A pointer to the MetadataComponent to attach.
func (r *Registry) SetMetadata(id Entity, comp *MetadataComponent) {
	r.metadata.Set(id, comp)
}

// GetMetadata retrieves the MetadataComponent associated with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//
// Returns:
//   - A pointer to the entity's MetadataComponent, or nil if not found.
func (r *Registry) GetMetadata(id Entity) *MetadataComponent {
	return r.metadata.Get(id)
}

// SetStats associates a StatsComponent with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//   - comp: A pointer to the StatsComponent to attach.
func (r *Registry) SetStats(id Entity, comp *StatsComponent) {
	r.stats.Set(id, comp)
}

// GetStats retrieves the StatsComponent associated with a specific Entity.
//
// Parameters:
//   - id: The target Entity ID.
//
// Returns:
//   - A pointer to the entity's StatsComponent, or nil if not found.
func (r *Registry) GetStats(id Entity) *StatsComponent {
	return r.stats.Get(id)
}

// GetAllEntities returns a slice containing all currently registered Entity IDs in the system.
//
// Returns:
//   - A slice of Entity IDs.
func (r *Registry) GetAllEntities() []Entity {
	list := make([]Entity, 0, r.entities.Len())
	r.entities.Range(func(key Entity, value bool) {
		list = append(list, key)
	})
	return list
}
