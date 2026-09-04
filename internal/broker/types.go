package broker

import "github.com/example/osb-broker/internal/store"

// OSB API Version
const APIVersion = "2.17"

// Context represents platform context
type Context struct {
	Platform            string `json:"platform"`
	OrganizationGUID    string `json:"organization_guid,omitempty"`
	SpaceGUID           string `json:"space_guid,omitempty"`
	ClusterID           string `json:"cluster_id,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	OriginatingIdentity string `json:"originating_identity,omitempty"`
}

// ProvisionRequest represents a provision request
type ProvisionRequest struct {
	ServiceID  string                 `json:"service_id"`
	PlanID     string                 `json:"plan_id"`
	Context    Context                `json:"context"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`

	// OrganizationGUID und SpaceGUID sind die Top-Level-Felder aus OSB <= 2.12.
	// Die Spezifikation fuehrt sie als veraltet, Cloud Foundry sendet sie aber
	// weiter - Korifi sogar ausschliesslich, ohne context-Objekt. Wer nur
	// context liest, bekommt von dort nie eine Space-GUID (FINDINGS #3).
	OrganizationGUID string `json:"organization_guid,omitempty"`
	SpaceGUID        string `json:"space_guid,omitempty"`
}

// ResolvedContext fuehrt beide erlaubten Quellen zusammen.
//
// context hat Vorrang, weil die Spezifikation die Top-Level-Felder als
// veraltet fuehrt; wo context ein Feld nicht setzt, traegt Top-Level bei,
// statt dass die Information verloren geht.
func (r *ProvisionRequest) ResolvedContext() Context {
	ctx := r.Context
	if ctx.SpaceGUID == "" {
		ctx.SpaceGUID = r.SpaceGUID
	}
	if ctx.OrganizationGUID == "" {
		ctx.OrganizationGUID = r.OrganizationGUID
	}
	return ctx
}

// ProvisionResponse represents a provision response
type ProvisionResponse struct {
	DashboardURL string `json:"dashboard_url,omitempty"`
	Operation    string `json:"operation,omitempty"`
}

// DeprovisionRequest represents a deprovision request
type DeprovisionRequest struct {
	ServiceID string `json:"service_id"`
	PlanID    string `json:"plan_id"`
}

// DeprovisionResponse represents a deprovision response
type DeprovisionResponse struct {
	Operation string `json:"operation,omitempty"`
}

// BindRequest represents a bind request
type BindRequest struct {
	ServiceID    string                 `json:"service_id"`
	PlanID       string                 `json:"plan_id"`
	AppGUID      string                 `json:"app_guid"`
	Context      Context                `json:"context"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	BindResource *BindResource          `json:"bind_resource,omitempty"`
}

// BindResource represents bind resource information
type BindResource struct {
	AppGUID        string `json:"app_guid,omitempty"`
	Route          string `json:"route,omitempty"`
	CredentialName string `json:"credential_name,omitempty"`
}

// BindResponse represents a bind response
type BindResponse struct {
	Credentials     map[string]interface{} `json:"credentials"`
	SyslogDrainURL  string                 `json:"syslog_drain_url,omitempty"`
	RouteServiceURL string                 `json:"route_service_url,omitempty"`
	VolumeMounts    []interface{}          `json:"volume_mounts,omitempty"`
	Operation       string                 `json:"operation,omitempty"`
}

// UnbindRequest represents an unbind request
type UnbindRequest struct {
	ServiceID string `json:"service_id"`
	PlanID    string `json:"plan_id"`
}

// UnbindResponse represents an unbind response
type UnbindResponse struct {
	Operation string `json:"operation,omitempty"`
}

// UpdateInstanceRequest represents an update instance request
type UpdateInstanceRequest struct {
	ServiceID      string                 `json:"service_id"`
	PlanID         string                 `json:"plan_id"`
	Context        Context                `json:"context"`
	Parameters     map[string]interface{} `json:"parameters,omitempty"`
	PreviousValues PreviousValues         `json:"previous_values"`
}

// PreviousValues represents previous state values
type PreviousValues struct {
	ServiceID string `json:"service_id,omitempty"`
	PlanID    string `json:"plan_id,omitempty"`
}

// UpdateInstanceResponse represents an update instance response
type UpdateInstanceResponse struct {
	Operation string `json:"operation,omitempty"`
}

// LastOperationResponse represents a last operation response
type LastOperationResponse struct {
	State       string `json:"state"`
	Description string `json:"description"`
	Operation   string `json:"operation,omitempty"`
}

// GetInstanceResponse represents a get instance response
type GetInstanceResponse struct {
	ServiceID    string                 `json:"service_id"`
	PlanID       string                 `json:"plan_id"`
	DashboardURL string                 `json:"dashboard_url,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
}

// GetBindingResponse represents a get binding response
type GetBindingResponse struct {
	Credentials     map[string]interface{} `json:"credentials"`
	SyslogDrainURL  string                 `json:"syslog_drain_url,omitempty"`
	RouteServiceURL string                 `json:"route_service_url,omitempty"`
	VolumeMounts    []interface{}          `json:"volume_mounts,omitempty"`
}

// OperationState represents the state of an asynchronous operation
type OperationState string

const (
	OperationStateInProgress OperationState = "in progress"
	OperationStateSucceeded  OperationState = "succeeded"
	OperationStateFailed     OperationState = "failed"
)

// Operation represents an asynchronous operation
type Operation struct {
	ID          string
	State       OperationState
	Description string
	Type        string // "provision", "update", "deprovision", "bind", "unbind"
}

// Type aliases from store package (to avoid import cycle)
type Catalog = store.Catalog
type Service = store.Service
type ServicePlan = store.ServicePlan
type ServiceMetadata = store.ServiceMetadata
