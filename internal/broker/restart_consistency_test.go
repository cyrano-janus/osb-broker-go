package broker

import (
	"context"

	"testing"

	"github.com/example/osb-broker/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 1.1: after a restart (fresh broker on the same store) ALL read and
// write paths must behave consistently — not just GetInstance.

func restartedBroker(t *testing.T, s StateStore) *Broker {
	t.Helper()
	return New(store.NewInMemoryStore(), s)
}

func provisionVia(t *testing.T, b *Broker, instanceID string) {
	t.Helper()
	_, err := b.Provision(context.Background(), instanceID, &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context:   Context{Platform: "cloudfoundry"},
	})
	require.NoError(t, err)
}

func TestRestart_GetBindingWorks(t *testing.T) {
	s := NewInMemoryStateStore()
	b1 := New(store.NewInMemoryStore(), s)
	provisionVia(t, b1, "inst-1")
	_, err := b1.Bind(context.Background(), "inst-1", "bind-1", &BindRequest{
		ServiceID: "service-1", PlanID: "plan-free", AppGUID: "app-1",
	})
	require.NoError(t, err)

	b2 := restartedBroker(t, s)
	resp, err := b2.GetBinding(context.Background(), "inst-1", "bind-1")
	require.NoError(t, err)
	assert.NotNil(t, resp.Credentials)
}

func TestRestart_ProvisionIdempotent(t *testing.T) {
	s := NewInMemoryStateStore()
	b1 := New(store.NewInMemoryStore(), s)
	provisionVia(t, b1, "inst-1")

	// Re-provisioning the same instance via the restarted broker must be
	// idempotent, NOT a conflict or duplicate.
	b2 := restartedBroker(t, s)
	_, err := b2.Provision(context.Background(), "inst-1", &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context:   Context{Platform: "cloudfoundry"},
	})
	require.NoError(t, err)
}

func TestRestart_DeprovisionBlockedByPersistedBinding(t *testing.T) {
	s := NewInMemoryStateStore()
	b1 := New(store.NewInMemoryStore(), s)
	provisionVia(t, b1, "inst-1")
	_, err := b1.Bind(context.Background(), "inst-1", "bind-1", &BindRequest{
		ServiceID: "service-1", PlanID: "plan-free", AppGUID: "app-1",
	})
	require.NoError(t, err)

	// The restarted broker must still see the binding and refuse deprovision.
	b2 := restartedBroker(t, s)
	_, err = b2.Deprovision(context.Background(), "inst-1", &DeprovisionRequest{ServiceID: "service-1", PlanID: "plan-free"})
	assert.Error(t, err) // "instance has existing bindings"

	// After unbinding via the new broker, deprovision must succeed.
	_, err = b2.Unbind(context.Background(), "inst-1", "bind-1", &UnbindRequest{ServiceID: "service-1", PlanID: "plan-free"})
	require.NoError(t, err)
	_, err = b2.Deprovision(context.Background(), "inst-1", &DeprovisionRequest{ServiceID: "service-1", PlanID: "plan-free"})
	require.NoError(t, err)

	// And nothing survives in the store.
	_, err = s.GetInstance(context.Background(), "inst-1")
	assert.Error(t, err)
	_, err = s.GetBinding(context.Background(), "bind-1")
	assert.Error(t, err)
}
