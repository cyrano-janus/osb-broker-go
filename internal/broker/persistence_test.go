package broker

import (
	"context"

	"testing"

	"github.com/example/osb-broker/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 1.1: Persistence contract. Any Store implementation must survive
// "restarts" (i.e. a fresh Broker wired to the same store sees the data).

func newTestInstance(id string) *Instance {
	return &Instance{
		ID:           id,
		ServiceID:    "service-1",
		PlanID:       "plan-free",
		Context:      Context{Platform: "cloudfoundry", SpaceGUID: "space-1"},
		Parameters:   map[string]interface{}{"foo": "bar"},
		DashboardURL: "https://dashboard.example.com/" + id,
		Ready:        true,
	}
}

func newTestBinding(id, instanceID string) *Binding {
	return &Binding{
		ID:          id,
		InstanceID:  instanceID,
		ServiceID:   "service-1",
		PlanID:      "plan-free",
		AppGUID:     "app-1",
		Credentials: map[string]interface{}{"uri": "https://example.com"},
		Ready:       true,
	}
}

func TestInMemoryStore_PutGetInstance(t *testing.T) {
	s := NewInMemoryStateStore()

	err := s.PutInstance(context.Background(), newTestInstance("inst-1"))
	require.NoError(t, err)

	got, err := s.GetInstance(context.Background(), "inst-1")
	require.NoError(t, err)
	assert.Equal(t, "service-1", got.ServiceID)
	assert.Equal(t, "plan-free", got.PlanID)
	assert.Equal(t, "space-1", got.Context.SpaceGUID)
}

func TestInMemoryStore_GetInstanceNotFound(t *testing.T) {
	s := NewInMemoryStateStore()

	_, err := s.GetInstance(context.Background(), "missing")
	assert.Error(t, err)
}

func TestInMemoryStore_DeleteInstance(t *testing.T) {
	s := NewInMemoryStateStore()

	require.NoError(t, s.PutInstance(context.Background(), newTestInstance("inst-1")))
	require.NoError(t, s.DeleteInstance(context.Background(), "inst-1"))

	_, err := s.GetInstance(context.Background(), "inst-1")
	assert.Error(t, err)
}

func TestInMemoryStore_PutGetDeleteBinding(t *testing.T) {
	s := NewInMemoryStateStore()

	err := s.PutBinding(context.Background(), newTestBinding("bind-1", "inst-1"))
	require.NoError(t, err)

	got, err := s.GetBinding(context.Background(), "bind-1")
	require.NoError(t, err)
	assert.Equal(t, "inst-1", got.InstanceID)
	assert.Equal(t, "https://example.com", got.Credentials["uri"])

	require.NoError(t, s.DeleteBinding(context.Background(), "bind-1"))
	_, err = s.GetBinding(context.Background(), "bind-1")
	assert.Error(t, err)
}

func TestInMemoryStore_ListBindingsByInstance(t *testing.T) {
	s := NewInMemoryStateStore()

	require.NoError(t, s.PutBinding(context.Background(), newTestBinding("b1", "inst-1")))
	require.NoError(t, s.PutBinding(context.Background(), newTestBinding("b2", "inst-1")))
	require.NoError(t, s.PutBinding(context.Background(), newTestBinding("b3", "inst-2")))

	bindings, err := s.ListBindingsByInstance(context.Background(), "inst-1")
	require.NoError(t, err)
	assert.Len(t, bindings, 2)
}

func TestInMemoryStore_SurvivesBrokerRestart(t *testing.T) {
	// The contract that matters for Phase 1.1: state outlives the broker
	// process. Wire two brokers to the same store and verify visibility.
	s := NewInMemoryStateStore()
	catalog := store.NewInMemoryStore()
	b1 := New(catalog, s)
	_, err := b1.Provision(context.Background(), "inst-9", &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context:   Context{Platform: "cloudfoundry"},
	})
	require.NoError(t, err)

	b2 := New(catalog, s) // "restarted" broker
	resp, err := b2.GetInstance(context.Background(), "inst-9")
	require.NoError(t, err)
	assert.Equal(t, "service-1", resp.ServiceID)
}
