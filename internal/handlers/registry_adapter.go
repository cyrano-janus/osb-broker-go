package handlers

import (
	"context"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/definition"
)

// stateStoreRegistry adapts the broker's StateStore to the engine's
// InstanceRegistry interface, so the Generic Engine can record provisioned
// instances for later existence checks (deprovision 410, bind validation).
type stateStoreRegistry struct {
	store broker.StateStore
}

func (r *stateStoreRegistry) PutInstance(ctx context.Context, rec *definition.InstanceRecord) error {
	return r.store.PutInstance(ctx, &broker.Instance{
		ID:        rec.ID,
		ServiceID: rec.ServiceID,
		PlanID:    rec.PlanID,
		Ready:     true,
	})
}

func (r *stateStoreRegistry) DeleteInstance(ctx context.Context, instanceID string) error {
	return r.store.DeleteInstance(ctx, instanceID)
}
