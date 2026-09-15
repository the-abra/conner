package rooms

import "sync"

const Default = "general"

type Room struct {
	ID    string
	Epoch uint64
}

type Registry struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

func NewRegistry() *Registry {
	r := &Registry{rooms: make(map[string]*Room)}
	r.rooms[Default] = &Room{ID: Default, Epoch: 1}
	return r
}

func (r *Registry) Get(id string) *Room {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rooms[id]
}

func (r *Registry) Ensure(id string) *Room {
	if id == "" {
		id = Default
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if rm, ok := r.rooms[id]; ok {
		return rm
	}
	rm := &Room{ID: id, Epoch: 1}
	r.rooms[id] = rm
	return rm
}

func (r *Registry) BumpEpoch(id string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	rm, ok := r.rooms[id]
	if !ok {
		rm = &Room{ID: id, Epoch: 1}
		r.rooms[id] = rm
	}
	rm.Epoch++
	return rm.Epoch
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.rooms))
	for id := range r.rooms {
		out = append(out, id)
	}
	return out
}
