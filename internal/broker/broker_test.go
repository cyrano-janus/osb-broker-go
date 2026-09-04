package broker

import (
	"context"

	"testing"

	"github.com/example/osb-broker/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBroker(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	require.NotNil(t, broker)
	assert.NotNil(t, broker.store)
	assert.NotNil(t, broker.instances)
	assert.NotNil(t, broker.bindings)
	assert.NotNil(t, broker.operations)
}

func TestGetCatalog(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	catalog, err := broker.GetCatalog()

	require.NoError(t, err)
	require.NotNil(t, catalog)
	assert.Len(t, catalog.Services, 2)
}

func TestProvision(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	req := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform:         "cloudfoundry",
			OrganizationGUID: "org-123",
			SpaceGUID:        "space-456",
		},
	}

	response, err := broker.Provision(context.Background(), "instance-1", req)

	require.NoError(t, err)
	require.NotNil(t, response)

	// Verify instance was created
	instance, exists := broker.instances["instance-1"]
	assert.True(t, exists)
	assert.Equal(t, "service-1", instance.ServiceID)
	assert.Equal(t, "plan-free", instance.PlanID)
	assert.Equal(t, "org-123", instance.Context.OrganizationGUID)
}

func TestProvisionIdempotent(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	req := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}

	// First provision
	response1, err := broker.Provision(context.Background(), "instance-1", req)
	require.NoError(t, err)
	require.NotNil(t, response1)

	// Second provision (idempotent)
	response2, err := broker.Provision(context.Background(), "instance-1", req)
	require.NoError(t, err)
	require.NotNil(t, response2)
}

func TestProvisionInvalidService(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	req := &ProvisionRequest{
		ServiceID: "invalid-service",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}

	response, err := broker.Provision(context.Background(), "instance-1", req)

	assert.Error(t, err)
	assert.Nil(t, response)
}

func TestDeprovision(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// First provision an instance
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	// Then deprovision
	deprovReq := &DeprovisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
	}
	response, err := broker.Deprovision(context.Background(), "instance-1", deprovReq)

	require.NoError(t, err)
	require.NotNil(t, response)

	// Verify instance was deleted
	_, exists := broker.instances["instance-1"]
	assert.False(t, exists)
}

func TestDeprovisionWithBinding(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision instance
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	// Create binding
	bindReq := &BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err = broker.Bind(context.Background(), "instance-1", "binding-1", bindReq)
	require.NoError(t, err)

	// Try to deprovision (should fail)
	deprovReq := &DeprovisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
	}
	response, err := broker.Deprovision(context.Background(), "instance-1", deprovReq)

	assert.Error(t, err)
	assert.Nil(t, response)
}

func TestBind(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision instance first
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	// Bind
	bindReq := &BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	response, err := broker.Bind(context.Background(), "instance-1", "binding-1", bindReq)

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.NotNil(t, response.Credentials)
	assert.Contains(t, response.Credentials, "uri")
	assert.Contains(t, response.Credentials, "username")
	assert.Contains(t, response.Credentials, "password")

	// Verify binding was created
	binding, exists := broker.bindings["binding-1"]
	assert.True(t, exists)
	assert.Equal(t, "instance-1", binding.InstanceID)
	assert.Equal(t, "app-123", binding.AppGUID)
}

func TestBindIdempotent(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision and bind
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	bindReq := &BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}

	// First bind
	response1, err := broker.Bind(context.Background(), "instance-1", "binding-1", bindReq)
	require.NoError(t, err)

	// Second bind (idempotent)
	response2, err := broker.Bind(context.Background(), "instance-1", "binding-1", bindReq)
	require.NoError(t, err)

	// Credentials should be the same
	assert.Equal(t, response1.Credentials, response2.Credentials)
}

func TestUnbind(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision and bind
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	bindReq := &BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err = broker.Bind(context.Background(), "instance-1", "binding-1", bindReq)
	require.NoError(t, err)

	// Unbind
	unbindReq := &UnbindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
	}
	response, err := broker.Unbind(context.Background(), "instance-1", "binding-1", unbindReq)

	require.NoError(t, err)
	require.NotNil(t, response)

	// Verify binding was deleted
	_, exists := broker.bindings["binding-1"]
	assert.False(t, exists)
}

func TestGetInstance(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision instance
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	// Get instance
	response, err := broker.GetInstance(context.Background(), "instance-1")

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, "service-1", response.ServiceID)
	assert.Equal(t, "plan-free", response.PlanID)
}

func TestGetInstanceNotFound(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	response, err := broker.GetInstance(context.Background(), "nonexistent")

	assert.Error(t, err)
	assert.Nil(t, response)
}

func TestGetBinding(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision and bind
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	bindReq := &BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err = broker.Bind(context.Background(), "instance-1", "binding-1", bindReq)
	require.NoError(t, err)

	// Get binding
	response, err := broker.GetBinding(context.Background(), "instance-1", "binding-1")

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.NotNil(t, response.Credentials)
}

func TestUpdateInstance(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision instance
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	// Update instance
	updateReq := &UpdateInstanceRequest{
		ServiceID: "service-1",
		PlanID:    "plan-premium",
		Context: Context{
			Platform: "cloudfoundry",
		},
		PreviousValues: PreviousValues{
			PlanID: "plan-free",
		},
	}
	response, err := broker.UpdateInstance(context.Background(), "instance-1", updateReq)

	require.NoError(t, err)
	require.NotNil(t, response)

	// Verify plan was updated (read back through the persistent store)
	updated, err := broker.GetInstance(context.Background(), "instance-1")
	require.NoError(t, err)
	assert.Equal(t, "plan-premium", updated.PlanID)
}

func TestGetLastOperation(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision instance
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	// Get last operation
	response, err := broker.GetLastOperation("instance-1", "provision")

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, string(OperationStateSucceeded), response.State)
}

func TestGetLastBindingOperation(t *testing.T) {
	store := store.NewInMemoryStore()
	broker := New(store, nil)

	// Provision and bind
	provReq := &ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err := broker.Provision(context.Background(), "instance-1", provReq)
	require.NoError(t, err)

	bindReq := &BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: Context{
			Platform: "cloudfoundry",
		},
	}
	_, err = broker.Bind(context.Background(), "instance-1", "binding-1", bindReq)
	require.NoError(t, err)

	// Get last binding operation
	response, err := broker.GetLastBindingOperation("instance-1", "binding-1", "bind")

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, string(OperationStateSucceeded), response.State)
}
