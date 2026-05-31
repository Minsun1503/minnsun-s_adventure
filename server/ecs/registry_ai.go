package ecs

// Extend Registry with AI component storage.
// Kept in a separate file to avoid bloating ecs.go.

// We add the ai field by embedding it into the existing Registry via init.
// Since Registry is a struct we own, just add the field directly:

// NOTE: add this field to the Registry struct in ecs.go:
//   ai *state.TypedSyncMap[Entity, AIComponent]
//
// And in NewRegistry():
//   ai: &state.TypedSyncMap[Entity, AIComponent]{},
//
// Then add these methods:

func (r *Registry) SetAI(id Entity, comp AIComponent) {
	r.ai.Set(id, comp)
}

func (r *Registry) GetAI(id Entity) (AIComponent, bool) {
	return r.ai.Get(id)
}

func (r *Registry) DeleteAI(id Entity) {
	r.ai.Delete(id)
}

// RangeAI iterates all entities with an AIComponent.
// Called once per game tick by the AI system.
func (r *Registry) RangeAI(f func(id Entity, ai AIComponent) bool) {
	r.ai.Range(f)
}

// RemoveEntity — extend the existing method to also clean up AI component.
// Update the body in ecs.go to add:
//   r.ai.Delete(id)
