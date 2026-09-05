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

// Counter zaehlt den Bestand. Ein Zustandsspeicher darf das nicht koennen -
// dann gibt es die Bestandsmetriken nicht, statt eine 0 zu behaupten.
//
// Gezaehlt wird beim Abholen und nicht mitgefuehrt: ein Zaehler, den der
// Broker bei jedem Provision hochzaehlt, faellt beim Neustart auf 0 zurueck,
// waehrend die Instanzen weiterlaufen - und er verpasst jede Aenderung, die
// nicht durch diesen Prozess ging. Der Zustand liegt in CRDs, nicht im
// Prozess; also fragt man ihn.
type Counter interface {
	// CountInstances zaehlt je Angebot und Plan. Die blosse Gesamtzahl sagt
	// einem Betreiber wenig - die Frage ist "wovon wie viele", also welches
	// Angebot genutzt wird und wo Kosten entstehen.
	CountInstances(ctx context.Context) (map[InstanceKey]int, error)
	// CountBindings zaehlt je Angebot.
	CountBindings(ctx context.Context) (map[string]int, error)
}

// InstanceKey ist die Aufschluesselung des Bestands: Angebot und Plan.
type InstanceKey struct {
	ServiceID string
	PlanID    string
}

// memoryStateStore is a thread-safe in-memory StateStore.
type memoryStateStore struct {
	mu        sync.RWMutex
	instances map[string]*Instance
	bindings  map[string]*Binding
}

// CountInstances und CountBindings erfuellen Counter.
func (m *memoryStateStore) CountInstances(context.Context) (map[InstanceKey]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[InstanceKey]int{}
	for _, i := range m.instances {
		out[InstanceKey{ServiceID: i.ServiceID, PlanID: i.PlanID}]++
	}
	return out, nil
}

func (m *memoryStateStore) CountBindings(context.Context) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]int{}
	for _, b := range m.bindings {
		out[b.ServiceID]++
	}
	return out, nil
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
	m.instances[i.ID] = i.DeepCopy()
	return nil
}

func (m *memoryStateStore) GetInstance(_ context.Context, id string) (*Instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i, ok := m.instances[id]
	if !ok {
		return nil, fmt.Errorf("%w: instance %q", ErrNotFound, id)
	}
	return i.DeepCopy(), nil
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
	m.bindings[b.ID] = b.DeepCopy()
	return nil
}

func (m *memoryStateStore) GetBinding(_ context.Context, id string) (*Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bindings[id]
	if !ok {
		return nil, fmt.Errorf("%w: binding %q", ErrNotFound, id)
	}
	return b.DeepCopy(), nil
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
			out = append(out, b.DeepCopy())
		}
	}
	return out, nil
}
