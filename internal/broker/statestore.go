package broker

import (
	"context"
	"fmt"
	"sync"
)

// StateStore persists broker state (instances and bindings) independently
// of the broker process. Implementations:
//   - memoryStateStore: tests and single-process use
//   - K8sStateStore:    ConfigMap-backed, survives pod restarts
type StateStore interface {
	PutInstance(ctx context.Context, i *Instance) error
	GetInstance(ctx context.Context, id string) (*Instance, error)
	DeleteInstance(ctx context.Context, id string) error
	PutBinding(ctx context.Context, b *Binding) error
	GetBinding(ctx context.Context, id string) (*Binding, error)
	DeleteBinding(ctx context.Context, id string) error
	ListBindingsByInstance(ctx context.Context, instanceID string) ([]*Binding, error)
}

// memoryStateStore is a thread-safe in-memory StateStore.
type memoryStateStore struct {
	mu        sync.RWMutex
	instances map[string]*Instance
	bindings  map[string]*Binding
}

// NewInMemoryStateStore returns an empty in-memory StateStore.
func NewInMemoryStateStore() StateStore {
	return &memoryStateStore{
		instances: make(map[string]*Instance),
		bindings:  make(map[string]*Binding),
	}
}

func (m *memoryStateStore) PutInstance(_ context.Context, i *Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *i
	m.instances[i.ID] = &cp
	return nil
}

func (m *memoryStateStore) GetInstance(_ context.Context, id string) (*Instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i, ok := m.instances[id]
	if !ok {
		return nil, fmt.Errorf("%w: instance %q", ErrNotFound, id)
	}
	cp := *i
	return &cp, nil
}

func (m *memoryStateStore) DeleteInstance(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.instances, id)
	return nil
}

func (m *memoryStateStore) PutBinding(_ context.Context, b *Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *b
	m.bindings[b.ID] = &cp
	return nil
}

func (m *memoryStateStore) GetBinding(_ context.Context, id string) (*Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bindings[id]
	if !ok {
		return nil, fmt.Errorf("%w: binding %q", ErrNotFound, id)
	}
	cp := *b
	return &cp, nil
}

func (m *memoryStateStore) DeleteBinding(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bindings, id)
	return nil
}

func (m *memoryStateStore) ListBindingsByInstance(_ context.Context, instanceID string) ([]*Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Binding
	for _, b := range m.bindings {
		if b.InstanceID == instanceID {
			cp := *b
			out = append(out, &cp)
		}
	}
	return out, nil
}
