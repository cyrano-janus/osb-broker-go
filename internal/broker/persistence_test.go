package broker

import (
	"context"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 1.1: Persistence contract. Any Store implementation must survive
// "restarts" (i.e. a fresh Broker wired to the same store sees the data).

func newTestInstance(id string) *Instance {
	return &Instance{
		ID:        id,
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context:   Context{Platform: "cloudfoundry", SpaceGUID: "space-1"},
		// Der Namespace, in dem die Operator-Ressourcen dieser Instanz
		// liegen. DELETE- und last_operation-Requests tragen ihn nicht, er
		// muss also aus dem Store kommen (FINDINGS #7/#16).
		Namespace:    "space-1",
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
	// Der Vertrag, auf den es ankommt: der Zustand ueberlebt den Prozess.
	// Zwei Broker auf demselben Speicher, und der zweite sieht, was der erste
	// geschrieben hat.
	s := NewInMemoryStateStore()
	require.NoError(t, New(s).RecordInstance(context.Background(), &Instance{
		ID:        "inst-9",
		ServiceID: "def-svc-0001",
		PlanID:    "def-plan-free",
		Context:   Context{Platform: "cloudfoundry"},
	}))

	resp, err := New(s).GetInstance(context.Background(), "inst-9")

	require.NoError(t, err)
	assert.Equal(t, "def-svc-0001", resp.ServiceID)
}
