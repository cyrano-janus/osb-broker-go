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
	return e.op.ApplyCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, rendered)
}

// DeprovisionInstance deletes the instance's CR.
func (e *Engine) DeprovisionInstance(ctx context.Context, sd *ServiceDefinition, namespace, instanceID string) error {
	return e.op.DeleteCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, instanceID)
}

// LastOperation maps CR readiness to OSB operation state:
// succeeded / in progress.
func (e *Engine) LastOperation(ctx context.Context, sd *ServiceDefinition, namespace, instanceID string) (string, error) {
	cr, err := e.op.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, instanceID)
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
	secretName, err := RenderSecretName(sd, instanceID)
	if err != nil {
		return nil, "", err
	}
	data, err := e.op.ReadSecret(ctx, namespace, secretName)
	if err != nil {
		return nil, "", err
	}
	return ExtractCredentials(data, sd.Spec.Bind.CredentialKeys), secretName, nil
}

var _ = schema.GroupVersionKind{} // placeholder to keep k8s import if unused later
var _ = unstructured.Unstructured{}
