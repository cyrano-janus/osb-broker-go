package definition

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Engine drives the generic lifecycle: it owns the ServiceDefinitions and
// applies them through an OperatorClient. It is storage-agnostic — the
// broker keeps its own bookkeeping via its StateStore.
type Engine struct {
	op           *OperatorClient
	definitions  []*ServiceDefinition
	byServiceID  map[string]*ServiceDefinition
	// InstanceRegistry lets the engine record provisioned/deprovisioned
	// instances in the broker's persistent state (decoupled via interface).
	reg InstanceRegistry
}

// InstanceRegistry is the subset of the broker's StateStore the engine needs
// to keep its bookkeeping consistent with CR lifecycle.
type InstanceRegistry interface {
	PutInstance(ctx context.Context, inst *InstanceRecord) error
	DeleteInstance(ctx context.Context, instanceID string) error
	// GetInstance returns the record for the instance (registry lookup).
	GetInstance(ctx context.Context, instanceID string) (*InstanceRecord, error)
}

// InstanceRecord is a minimal instance record persisted by the registry.
type InstanceRecord struct {
	ID        string `json:"id"`
	ServiceID string `json:"serviceId"`
	PlanID    string `json:"planId"`
	// AppliedObjects lists the K8s object names created for this instance
	// (multi-doc, 4.6). Single-doc templates produce exactly one entry.
	// Deprovision uses it to remove every object; empty = legacy behaviour
	// (delete the single sanitized safeName CR).
	AppliedObjects []string `json:"appliedObjects,omitempty"`
	// AppliedRefs carries the same objects including their own GVK. A
	// multi-doc template may mix kinds, so deprovision cannot fall back to
	// the definition's provision apiVersion/kind for every name.
	AppliedRefs []ObjectRef `json:"appliedRefs,omitempty"`
}

// SetInstanceRegistry attaches an instance registry (broker state store
// adapter). Optional — without it the engine does no bookkeeping.
func (e *Engine) SetInstanceRegistry(reg InstanceRegistry) {
	e.reg = reg
}

// regGet looks up the instance record; error when no registry is wired or
// the record does not exist.
func (e *Engine) regGet(ctx context.Context, instanceID string) (*InstanceRecord, error) {
	if e.reg == nil {
		return nil, ErrNotFound
	}
	return e.reg.GetInstance(ctx, instanceID)
}

// NewEngine wires definitions with the operator client (nil client is only
// valid for catalog-only usage).
func NewEngine(op *OperatorClient, defs ...*ServiceDefinition) *Engine {
	e := &Engine{op: op, byServiceID: map[string]*ServiceDefinition{}}
	for _, d := range defs {
		e.definitions = append(e.definitions, d)
		e.byServiceID[d.Spec.Offering.ID] = d
	}
	return e
}

// CatalogEntry is a minimal view for building the OSB catalog.
type CatalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Bindable    bool     `json:"bindable"`
	Tags        []string `json:"tags,omitempty"`
	Plans       []CatalogPlan `json:"plans"`
}

// CatalogPlan is the plan entry inside a CatalogEntry.
type CatalogPlan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Catalog converts all definitions into catalog entries.
func (e *Engine) Catalog() []CatalogEntry {
	out := make([]CatalogEntry, 0, len(e.definitions))
	for _, d := range e.definitions {
		entry := CatalogEntry{
			ID:          d.Spec.Offering.ID,
			Name:        d.Spec.Offering.Name,
			Description: d.Spec.Offering.Description,
			Bindable:    d.Spec.Offering.Bindable == nil || *d.Spec.Offering.Bindable,
			Tags:        d.Spec.Offering.Tags,
		}
		for _, p := range d.Spec.Offering.Plans {
			entry.Plans = append(entry.Plans, CatalogPlan{
				ID:          p.ID,
				Name:        p.Name,
				Description: p.Description,
			})
		}
		out = append(out, entry)
	}
	return out
}

// DefinitionByServiceID resolves a definition via its offering id.
func (e *Engine) DefinitionByServiceID(serviceID string) (*ServiceDefinition, error) {
	d, ok := e.byServiceID[serviceID]
	if !ok {
		return nil, fmt.Errorf("%w: service %q", ErrNotFound, serviceID)
	}
	return d, nil
}

// ProvisionInstance resolves the definition via serviceID and applies the CR.
func (e *Engine) ProvisionInstance(ctx context.Context, serviceID, instanceID, namespace, planID string, parameters map[string]interface{}) error {
	sd, err := e.DefinitionByServiceID(serviceID)
	if err != nil {
		return err
	}
	return e.provisionDefinition(ctx, sd, instanceID, namespace, planID, parameters)
}

func (e *Engine) provisionDefinition(ctx context.Context, sd *ServiceDefinition, instanceID, namespace, planID string, parameters map[string]interface{}) error {
	plan, err := sd.PlanByID(planID)
	if err != nil {
		return err
	}
	rendered, err := RenderProvision(sd, instanceID, plan.Params)
	if err != nil {
		return err
	}
	// Multi-doc aware apply: every document in the template is applied in
	// order. Single-doc templates produce exactly one object — identical
	// behaviour to the previous ApplyCR path.
	applied, err := e.op.ApplyManifestRefs(ctx,
		sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, rendered)
	if err != nil {
		return err
	}
	// Record the instance so deprovision/bind existence checks work. The
	// applied-object list enables multi-doc deprovision.
	if e.reg != nil {
		if err := e.reg.PutInstance(ctx, &InstanceRecord{
			ID:             instanceID,
			ServiceID:      sd.Spec.Offering.ID,
			PlanID:         planID,
			AppliedObjects: refNames(applied),
			AppliedRefs:    applied,
		}); err != nil {
			return fmt.Errorf("record instance: %w", err)
		}
	}
	return nil
}

// DeprovisionInstance removes all objects created for the instance and
// clears the instance record. For single-doc templates this degrades to
// deleting the one sanitized safeName CR (legacy behaviour).
func (e *Engine) DeprovisionInstance(ctx context.Context, sd *ServiceDefinition, namespace, instanceID string) error {
	safeName := SanitizeInstanceName(instanceID)

	var rec *InstanceRecord
	if r, err := e.regGet(ctx, instanceID); err == nil {
		rec = r
	}

	switch {
	case rec != nil && len(rec.AppliedRefs) > 0:
		// Multi-doc: delete every object by its own GVK.
		if _, err := e.op.DeleteManifestRefs(ctx, namespace, rec.AppliedRefs); err != nil {
			return err
		}
	case rec != nil && len(rec.AppliedObjects) > 0:
		// Record written before refs were tracked: all objects are assumed to
		// carry the definition's provision GVK.
		if _, err := e.op.DeleteManifestsByNames(ctx,
			sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, rec.AppliedObjects); err != nil {
			return err
		}
	default:
		// Legacy single-doc: nothing recorded (old records or registry off).
		if err := e.op.DeleteCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, safeName); err != nil {
			return err
		}
	}

	if e.reg != nil {
		if err := e.reg.DeleteInstance(ctx, instanceID); err != nil {
			return fmt.Errorf("remove instance record: %w", err)
		}
	}
	return nil
}

// LastOperation maps CR readiness to OSB operation state:
// succeeded / in progress.
func (e *Engine) LastOperation(ctx context.Context, sd *ServiceDefinition, namespace, instanceID string) (string, error) {
	cr, err := e.op.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, SanitizeInstanceName(instanceID))
	if err != nil {
		return "", err
	}
	done, err := EvaluateReadiness(sd, cr)
	if err != nil {
		return "", err
	}
	if done {
		return "succeeded", nil
	}
	return "in progress", nil
}

// BindCredentials renders the secret name from the definition, reads it and
// returns the credentials map plus the resolved secret name.
func (e *Engine) BindCredentials(ctx context.Context, sd *ServiceDefinition, namespace, instanceID string) (map[string]interface{}, string, error) {
	secretName, err := e.resolveSecretName(ctx, sd, namespace, instanceID)
	if err != nil {
		return nil, "", err
	}
	data, err := e.op.ReadSecret(ctx, namespace, secretName)
	if err != nil {
		return nil, "", err
	}
	creds, err := shapeCredentials(&sd.Spec.Bind, data)
	if err != nil {
		return nil, "", err
	}
	return creds, secretName, nil
}

var _ = schema.GroupVersionKind{} // placeholder to keep k8s import if unused later
var _ = unstructured.Unstructured{}
