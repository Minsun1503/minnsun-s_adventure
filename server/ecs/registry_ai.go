package ecs

func (r *Registry) SetAI(id Entity, comp AIComponent) {
	r.ai.Set(id, comp)
}

func (r *Registry) GetAI(id Entity) (AIComponent, bool) {
	return r.ai.Get(id)
}

func (r *Registry) DeleteAI(id Entity) {
	r.ai.Delete(id)
}

func (r *Registry) RangeAI(f func(id Entity, ai AIComponent) bool) {
	r.ai.Range(f)
}
