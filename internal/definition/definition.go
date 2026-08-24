// Package definition loads and validates declarative ServiceDefinitions:
// YAML documents that map an OSB service offering onto a Kubernetes
// custom resource managed by an external operator (Phase 2.1).
package definition

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

const (
	expectedAPIVersion = "broker.osb.io/v1alpha1"
	expectedKind       = "ServiceDefinition"
)

// ServiceDefinition is the top-level document.
type ServiceDefinition struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Metadata   Metadata     `json:"metadata"`
	Spec       Spec         `json:"spec"`
}

// Metadata identifies the definition.
type Metadata struct {
	Name string `json:"name"`
}

// Spec contains offering and operator mapping.
type Spec struct {
	Offering  Offering  `json:"offering"`
	Provision Provision `json:"provision"`
	Readiness Readiness `json:"readiness"`
	Bind      Bind      `json:"bind"`
}

// Offering is the OSB catalog entry.
type Offering struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Bindable    *bool    `json:"bindable,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Plans       []Plan   `json:"plans"`
}

// Plan is one OSB plan with its render parameters.
type Plan struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
	Free        *bool                  `json:"free,omitempty"`
}

// Provision describes the custom resource to create per instance.
type Provision struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// Template is a Go template rendering to the full CR manifest.
	Template string `json:"template"`
}

// Readiness defines how instance readiness is derived from CR status.
type Readiness struct {
	StatusJSONPath string `json:"statusJSONPath"`
	ExpectedValue  string `json:"expectedValue,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// Bind defines how binding credentials are extracted.
type Bind struct {
	// CredentialsFromSecret is a Go template for the secret name.
	CredentialsFromSecret string            `json:"credentialsFromSecret"`
	CredentialKeys        []string          `json:"credentialKeys,omitempty"` // filter; empty = all keys
	ExtraLabels           map[string]string `json:"extraLabels,omitempty"`
}

// Parse decodes and validates a ServiceDefinition YAML document.
func Parse(data []byte) (*ServiceDefinition, error) {
	sd := &ServiceDefinition{}
	if err := yaml.Unmarshal(data, sd); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if err := sd.Validate(); err != nil {
		return nil, err
	}
	return sd, nil
}

// Validate enforces the minimal contract needed by the engine.
func (sd *ServiceDefinition) Validate() error {
	if sd.APIVersion != expectedAPIVersion {
		return fmt.Errorf("apiVersion must be %q, got %q", expectedAPIVersion, sd.APIVersion)
	}
	if sd.Kind != expectedKind {
		return fmt.Errorf("kind must be %q, got %q", expectedKind, sd.Kind)
	}
	if sd.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	o := sd.Spec.Offering
	if o.ID == "" {
		return fmt.Errorf("spec.offering.id is required")
	}
	if o.Name == "" {
		return fmt.Errorf("spec.offering.name is required")
	}
	if len(o.Plans) == 0 {
		return fmt.Errorf("spec.offering.plans: at least one plan is required")
	}
	seenPlanIDs := map[string]bool{}
	for i, p := range o.Plans {
		if p.ID == "" {
			return fmt.Errorf("spec.offering.plans[%d].id is required", i)
		}
		if p.Name == "" {
			return fmt.Errorf("spec.offering.plans[%d].name is required", i)
		}
		if seenPlanIDs[p.ID] {
			return fmt.Errorf("spec.offering.plans[%d]: duplicate id %q", i, p.ID)
		}
		seenPlanIDs[p.ID] = true
	}
	if sd.Spec.Provision.APIVersion == "" || sd.Spec.Provision.Kind == "" {
		return fmt.Errorf("spec.provision.apiVersion and kind are required")
	}
	if sd.Spec.Provision.Template == "" {
		return fmt.Errorf("spec.provision.template is required")
	}
	if sd.Spec.Readiness.StatusJSONPath == "" {
		return fmt.Errorf("spec.readiness.statusJSONPath is required")
	}
	if sd.Spec.Bind.CredentialsFromSecret == "" {
		return fmt.Errorf("spec.bind.credentialsFromSecret is required")
	}
	return nil
}

// PlanByParams finds the plan whose ID matches the OSB request plan_id.
func (sd *ServiceDefinition) PlanByID(planID string) (*Plan, error) {
	for i := range sd.Spec.Offering.Plans {
		if sd.Spec.Offering.Plans[i].ID == planID {
			return &sd.Spec.Offering.Plans[i], nil
		}
	}
	return nil, fmt.Errorf("plan %q not found in service %q", planID, sd.Spec.Offering.Name)
}
