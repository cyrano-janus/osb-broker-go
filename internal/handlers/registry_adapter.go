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
		ID:             rec.ID,
		ServiceID:      rec.ServiceID,
		PlanID:         rec.PlanID,
		Namespace:      rec.Namespace,
		Ready:          true,
		AppliedObjects: rec.AppliedObjects,
		AppliedRefs:    toBrokerRefs(rec.AppliedRefs),
		Parameters:     rec.Parameters,
	})
}

func (r *stateStoreRegistry) DeleteInstance(ctx context.Context, instanceID string) error {
	return r.store.DeleteInstance(ctx, instanceID)
}

// GetInstance maps the persisted broker.Instance back to an InstanceRecord
// so the engine can recover the applied-object list (multi-doc deprovision).
func (r *stateStoreRegistry) GetInstance(ctx context.Context, instanceID string) (*definition.InstanceRecord, error) {
	inst, err := r.store.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, definition.ErrNotFound
	}
	return &definition.InstanceRecord{
		ID:             inst.ID,
		ServiceID:      inst.ServiceID,
		PlanID:         inst.PlanID,
		Namespace:      inst.Namespace,
		AppliedObjects: inst.AppliedObjects,
		AppliedRefs:    toDefinitionRefs(inst.AppliedRefs),
		Parameters:     inst.Parameters,
	}, nil
}

func toBrokerRefs(in []definition.ObjectRef) []broker.AppliedObjectRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]broker.AppliedObjectRef, 0, len(in))
	for _, r := range in {
		out = append(out, broker.AppliedObjectRef{
			APIVersion: r.APIVersion, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
		})
	}
	return out
}

func toDefinitionRefs(in []broker.AppliedObjectRef) []definition.ObjectRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]definition.ObjectRef, 0, len(in))
	for _, r := range in {
		out = append(out, definition.ObjectRef{
			APIVersion: r.APIVersion, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
		})
	}
	return out
}
