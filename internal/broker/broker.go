package broker

import (
	"fmt"
	"sync"
	"time"

	"github.com/example/osb-broker/internal/store"
)

// Broker implements the Open Service Broker API
type Broker struct {
	store       store.ServiceStore
	state       StateStore
	instances   map[string]*Instance
	bindings    map[string]*Binding
	operations  map[string]*Operation
	mu          sync.RWMutex
}

// Instance represents a service instance
type Instance struct {
	ID           string
	ServiceID    string
	PlanID       string
	Context      Context
	Parameters   map[string]interface{}
	DashboardURL string
	Ready        bool
}

// Binding represents a service binding
type Binding struct {
	ID              string
	InstanceID      string
	ServiceID       string
	PlanID          string
	AppGUID         string
	Context         Context
	Parameters      map[string]interface{}
	Credentials     map[string]interface{}
	SyslogDrainURL  string
	RouteServiceURL string
	VolumeMounts    []interface{}
	Ready           bool
}

// New creates a new broker instance. catalog provides the service catalog;
// state persists instances/bindings across restarts (Phase 1.1).
func New(catalog store.ServiceStore, state StateStore) *Broker {
	if state == nil {
		state = NewInMemoryStateStore()
	}
	return &Broker{
		store:      catalog,
		state:      state,
		instances:  make(map[string]*Instance),
		bindings:   make(map[string]*Binding),
		operations: make(map[string]*Operation),
	}
}

// GetCatalog returns the service catalog
func (b *Broker) GetCatalog() (*Catalog, error) {
	return b.store.GetCatalog()
}

// Provision creates or updates a service instance
func (b *Broker) Provision(instanceID string, req *ProvisionRequest) (*ProvisionResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Validate service and plan exist
	catalog, err := b.store.GetCatalog()
	if err != nil {
		return nil, err
	}

	service, _, err := findServiceAndPlan(catalog, req.ServiceID, req.PlanID)
	if err != nil {
		return nil, err
	}

	// Check if instance already exists (persistent store: idempotency must
	// survive restarts)
	existing, err := b.state.GetInstance(instanceID)
	if err == nil {
		// Check for conflicts
		if existing.ServiceID != req.ServiceID || existing.PlanID != req.PlanID {
			return nil, fmt.Errorf("instance already exists with different service/plan")
		}
		// Return success for idempotent provision
		return &ProvisionResponse{}, nil
	}

	// Create new instance
	instance := &Instance{
		ID:         instanceID,
		ServiceID:  req.ServiceID,
		PlanID:     req.PlanID,
		Context:    req.Context,
		Parameters: req.Parameters,
		Ready:      true,
	}

	// Generate dashboard URL if available
	if service.Metadata != nil && service.Metadata.DisplayName != "" {
		instance.DashboardURL = fmt.Sprintf("https://dashboard.example.com/instances/%s", instanceID)
	}

	b.instances[instanceID] = instance
	if err := b.state.PutInstance(instance); err != nil {
		return nil, fmt.Errorf("persist instance: %w", err)
	}

	response := &ProvisionResponse{}
	if instance.DashboardURL != "" {
		response.DashboardURL = instance.DashboardURL
	}

	return response, nil
}

// Deprovision removes a service instance
func (b *Broker) Deprovision(instanceID string, req *DeprovisionRequest) (*DeprovisionResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.state.GetInstance(instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found")
	}

	// Check for existing bindings (from the persistent store, so restarts
	// don't orphan bindings)
	bindings, listErr := b.state.ListBindingsByInstance(instanceID)
	if listErr != nil {
		return nil, fmt.Errorf("list bindings: %w", listErr)
	}
	for _, binding := range bindings {
		if binding.Ready {
			return nil, fmt.Errorf("instance has existing bindings")
		}
	}

	delete(b.instances, instanceID)
	if err := b.state.DeleteInstance(instanceID); err != nil {
		return nil, fmt.Errorf("delete persisted instance: %w", err)
	}

	return &DeprovisionResponse{}, nil
}

// Bind creates a binding between an app and a service instance
func (b *Broker) Bind(instanceID, bindingID string, req *BindRequest) (*BindResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.state.GetInstance(instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found")
	}

	// Check if binding already exists (persistent: idempotent across restarts)
	existing, bindErr := b.state.GetBinding(bindingID)
	if bindErr == nil {
		if existing.InstanceID == instanceID && existing.Ready {
			// Return existing binding credentials (idempotent)
			return &BindResponse{
				Credentials:     existing.Credentials,
				SyslogDrainURL:  existing.SyslogDrainURL,
				RouteServiceURL: existing.RouteServiceURL,
				VolumeMounts:    existing.VolumeMounts,
			}, nil
		}
	}

	// Create new binding
	binding := &Binding{
		ID:         bindingID,
		InstanceID: instanceID,
		ServiceID:  req.ServiceID,
		PlanID:     req.PlanID,
		AppGUID:    req.AppGUID,
		Context:    req.Context,
		Parameters: req.Parameters,
		Ready:      true,
	}

	// Generate credentials
	binding.Credentials = generateCredentials(instanceID, bindingID)

	b.bindings[bindingID] = binding
	if err := b.state.PutBinding(binding); err != nil {
		return nil, fmt.Errorf("persist binding: %w", err)
	}

	return &BindResponse{
		Credentials: binding.Credentials,
	}, nil
}

// Unbind removes a binding
func (b *Broker) Unbind(instanceID, bindingID string, req *UnbindRequest) (*UnbindResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	binding, err := b.state.GetBinding(bindingID)
	if err != nil {
		return nil, fmt.Errorf("binding not found")
	}

	if binding.InstanceID != instanceID {
		return nil, fmt.Errorf("binding does not belong to instance")
	}

	delete(b.bindings, bindingID)
	if err := b.state.DeleteBinding(bindingID); err != nil {
		return nil, fmt.Errorf("delete persisted binding: %w", err)
	}

	return &UnbindResponse{}, nil
}

// GetInstance retrieves instance details
func (b *Broker) GetInstance(instanceID string) (*GetInstanceResponse, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	instance, err := b.state.GetInstance(instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found")
	}

	return &GetInstanceResponse{
		ServiceID:    instance.ServiceID,
		PlanID:       instance.PlanID,
		DashboardURL: instance.DashboardURL,
		Parameters:   instance.Parameters,
	}, nil
}

// GetBinding retrieves binding details
func (b *Broker) GetBinding(instanceID, bindingID string) (*GetBindingResponse, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	binding, err := b.state.GetBinding(bindingID)
	if err != nil {
		return nil, fmt.Errorf("binding not found")
	}

	if binding.InstanceID != instanceID {
		return nil, fmt.Errorf("binding does not belong to instance")
	}

	return &GetBindingResponse{
		Credentials:     binding.Credentials,
		SyslogDrainURL:  binding.SyslogDrainURL,
		RouteServiceURL: binding.RouteServiceURL,
		VolumeMounts:    binding.VolumeMounts,
	}, nil
}

// UpdateInstance updates a service instance
func (b *Broker) UpdateInstance(instanceID string, req *UpdateInstanceRequest) (*UpdateInstanceResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	instance, err := b.state.GetInstance(instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found")
	}

	// Update plan if changed
	if req.PlanID != "" {
		instance.PlanID = req.PlanID
	}

	// Update parameters if provided
	if req.Parameters != nil {
		instance.Parameters = req.Parameters
	}

	if err := b.state.PutInstance(instance); err != nil {
		return nil, fmt.Errorf("persist instance update: %w", err)
	}

	return &UpdateInstanceResponse{}, nil
}

// GetLastOperation returns the state of the last operation
func (b *Broker) GetLastOperation(instanceID string, operation string) (*LastOperationResponse, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// For this reference implementation, all operations are synchronous
	// Return succeeded state
	return &LastOperationResponse{
		State:       string(OperationStateSucceeded),
		Description: "Operation completed successfully",
		Operation:   operation,
	}, nil
}

// GetLastBindingOperation returns the state of the last binding operation
func (b *Broker) GetLastBindingOperation(instanceID, bindingID, operation string) (*LastOperationResponse, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// For this reference implementation, all operations are synchronous
	return &LastOperationResponse{
		State:       string(OperationStateSucceeded),
		Description: "Operation completed successfully",
		Operation:   operation,
	}, nil
}

// Helper functions

func findServiceAndPlan(catalog *Catalog, serviceID, planID string) (*Service, *ServicePlan, error) {
	for _, service := range catalog.Services {
		if service.ID == serviceID {
			for _, plan := range service.Plans {
				if plan.ID == planID {
					return &service, &plan, nil
				}
			}
			return nil, nil, fmt.Errorf("plan not found")
		}
	}
	return nil, nil, fmt.Errorf("service not found")
}

func generateCredentials(instanceID, bindingID string) map[string]interface{} {
	// Truncate safely: OSB allows arbitrary binding/instance IDs, and a
	// short ID must not panic the broker.
	trunc := func(s string) string {
		if len(s) > 8 {
			return s[:8]
		}
		return s
	}
	return map[string]interface{}{
		"uri":      fmt.Sprintf("https://service.example.com/instances/%s", instanceID),
		"username": fmt.Sprintf("user_%s", trunc(bindingID)),
		"password": fmt.Sprintf("pass_%s_%s", trunc(instanceID), trunc(bindingID)),
		"host":     "service.example.com",
		"port":     5432,
		"database": fmt.Sprintf("db_%s", trunc(instanceID)),
	}
}

// Operation management for async operations (future enhancement)
func (b *Broker) createOperation(instanceID, opType string) string {
	opID := fmt.Sprintf("op_%s_%d", instanceID, time.Now().UnixNano())
	b.operations[opID] = &Operation{
		ID:          opID,
		State:       OperationStateInProgress,
		Description: "Operation in progress",
		Type:        opType,
	}
	return opID
}