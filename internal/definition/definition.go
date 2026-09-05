// Package definition loads and validates declarative ServiceDefinitions:
// YAML documents that map an OSB service offering onto a Kubernetes
// custom resource managed by an external operator (Phase 2.1).
package definition

import (
	"fmt"
	"regexp"
	"text/template"

	"sigs.k8s.io/yaml"
)

const (
	expectedAPIVersion = "broker.osb.io/v1alpha1"
	expectedKind       = "ServiceDefinition"
)

// ServiceDefinition is the top-level document.
type ServiceDefinition struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
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
	// AllowedParameters lists the parameter keys a consumer may supply in
	// the provision/update request body. Empty = no user parameters.
	AllowedParameters []string `json:"allowedParameters,omitempty"`
	// ParameterLimits begrenzt die Werte, die ein Konsument setzen darf.
	//
	// Ohne sie beschreibt ein Plan zwar Groessen, erzwingt sie aber nicht:
	// steht ein Schluessel in allowedParameters, ist jeder Wert dafuer
	// erlaubt. Ein Plan "small" mit 1Gi liesse sich mit 10Ti provisionieren,
	// und der Betreiber saehe es an der Rechnung.
	//
	// Die Grenzen gelten fuer Provision und Update gleichermassen und werden
	// zusaetzlich als OSB-Plan-Schema im Katalog veroeffentlicht.
	ParameterLimits map[string]ParameterLimit `json:"parameterLimits,omitempty"`
	Free            *bool                     `json:"free,omitempty"`
}

// ParameterLimit begrenzt einen einzelnen Benutzerparameter.
//
// Max und Min sind Zeichenketten, damit sowohl Zahlen ("3") als auch
// Kubernetes-Mengenangaben ("10Gi") in derselben Form stehen koennen; beide
// werden ueber resource.Quantity verglichen. OneOf schliesst alles aus, was
// nicht aufgezaehlt ist.
type ParameterLimit struct {
	Max   string   `json:"max,omitempty"`
	Min   string   `json:"min,omitempty"`
	OneOf []string `json:"oneOf,omitempty"`
}

// HasBounds meldet, ob die Grenze ueberhaupt etwas einschraenkt.
func (l ParameterLimit) HasBounds() bool {
	return l.Max != "" || l.Min != "" || len(l.OneOf) > 0
}

// ValidatePlanParameters validates user-supplied parameters against the
// target plan's allowedParameters whitelist. planID may be empty to check
// against all plans (provision without explicit plan is rejected anyway).
func (sd *ServiceDefinition) ValidatePlanParameters(planID string, parameters map[string]interface{}) error {
	if len(parameters) == 0 {
		return nil
	}
	plan, err := sd.PlanByID(planID)
	if err != nil {
		return err
	}
	return ValidatePlanParams(plan, parameters)
}

// ValidatePlanParams validates user-supplied parameters against the
// plan's allowedParameters whitelist.
func ValidatePlanParams(plan *Plan, parameters map[string]interface{}) error {
	if len(parameters) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(plan.AllowedParameters))
	for _, k := range plan.AllowedParameters {
		allowed[k] = true
	}
	for key := range parameters {
		if !allowed[key] {
			return fmt.Errorf("%w: parameter %q is not allowed in plan %q", ErrParameterNotAllowed, key, plan.Name)
		}
	}
	// Erlaubt heisst nicht unbegrenzt: der Plan darf Grenzen fuer die Werte
	// nennen, sonst beschreibt er Groessen, ohne sie zu erzwingen.
	return checkLimits(plan, parameters)
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
	//
	// Pflicht, solange ProvisionedService nicht gesetzt ist; daneben dient es
	// als Rueckfallebene, wenn ein Operator das Feld status.binding.name noch
	// nicht fuellt.
	CredentialsFromSecret string            `json:"credentialsFromSecret,omitempty"`
	CredentialKeys        []string          `json:"credentialKeys,omitempty"` // filter; empty = all keys
	ExtraLabels           map[string]string `json:"extraLabels,omitempty"`

	// ProvisionedService liest den Secret-Namen aus .status.binding.name des
	// provisionierten CR, statt ihn aus einem Namenstemplate zu raten
	// (CNCF Service Binding Specification, "Provisioned Service" duck type).
	//
	// Das ist der eigentliche Gewinn gegenueber der bisherigen Konvention:
	// der Operator sagt selbst, wo die Credentials liegen, statt dass der
	// Broker ein Namensschema nachbaut, das bei jedem Operator anders ist.
	ProvisionedService bool `json:"provisionedService,omitempty"`

	// Type ist der well-known Diensttyp der Spec (postgresql, redis, mysql,
	// rabbitmq, s3 ...). Er wird den Credentials als Feld "type" beigelegt -
	// die Spec verlangt es von jedem Binding-Secret.
	Type string `json:"type,omitempty"`
	// Provider benennt optional die Implementierung hinter dem Typ.
	Provider string `json:"provider,omitempty"`

	// Mapping formt die Keys des Operator-Secrets auf die Zielform um.
	//
	// Ist Mapping gesetzt, besteht das Ergebnis GENAU aus den hier genannten
	// Keys (plus type/provider). Das ist Absicht: ein Adapter, der zusaetzlich
	// noch alle Originalschluessel durchreicht, macht das Ergebnis
	// unvorhersehbar und den Zweck - eine definierte Zielform - zunichte.
	Mapping []CredentialMapping `json:"mapping,omitempty"`

	// ProjectSecret schreibt die Credentials zusaetzlich als spec-konformes
	// Secret in den Ziel-Namespace, fuer Konsumenten ausserhalb von Cloud
	// Foundry.
	ProjectSecret bool `json:"projectSecret,omitempty"`
}

// CredentialMapping beschreibt einen Zielschluessel: entweder uebernommen aus
// einem Schluessel des Operator-Secrets (From) oder zusammengesetzt (Value).
type CredentialMapping struct {
	// Name ist der Schluessel im Ergebnis.
	Name string `json:"name"`
	// From ist der Schluessel im Operator-Secret.
	From string `json:"from,omitempty"`
	// Value ist ein Go-Template ueber .credentials, etwa zum Zusammensetzen
	// einer URI aus mehreren Feldern.
	Value string `json:"value,omitempty"`
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
		// Grenzen beim Laden pruefen, nicht beim ersten Provision: eine
		// Grenze, die nie greifen kann, taeuscht Schutz vor.
		if err := p.validateLimits(); err != nil {
			return fmt.Errorf("spec.offering.plans[%d]: %w", i, err)
		}
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
	return sd.Spec.Bind.validate()
}

// wellKnownTypePattern beschreibt, was als Diensttyp taugt: der Wert landet
// als Key-Inhalt im projizierten Secret und in den Credentials.
var wellKnownTypePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func (b *Bind) validate() error {
	if b.CredentialsFromSecret == "" && !b.ProvisionedService {
		return fmt.Errorf("spec.bind: either credentialsFromSecret or provisionedService is required")
	}
	if b.Type != "" && !wellKnownTypePattern.MatchString(b.Type) {
		return fmt.Errorf("spec.bind.type %q must be lower-case alphanumeric with dashes (e.g. postgresql)", b.Type)
	}
	if b.ProjectSecret && b.Type == "" {
		return fmt.Errorf("spec.bind.projectSecret requires spec.bind.type (the specification requires a type on every binding secret)")
	}

	seen := map[string]bool{}
	for i, m := range b.Mapping {
		if m.Name == "" {
			return fmt.Errorf("spec.bind.mapping[%d]: name is required", i)
		}
		if seen[m.Name] {
			return fmt.Errorf("spec.bind.mapping[%d]: duplicate name %q", i, m.Name)
		}
		seen[m.Name] = true

		hasFrom, hasValue := m.From != "", m.Value != ""
		if hasFrom == hasValue {
			return fmt.Errorf("spec.bind.mapping[%d] %q: exactly one of from or value is required", i, m.Name)
		}
		if hasValue {
			if _, err := template.New("m").Option("missingkey=error").Parse(m.Value); err != nil {
				return fmt.Errorf("spec.bind.mapping[%d] %q: template: %w", i, m.Name, err)
			}
		}
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
	return nil, fmt.Errorf("%w: plan %q not found in service %q", ErrPlanUnknown, planID, sd.Spec.Offering.Name)
}
