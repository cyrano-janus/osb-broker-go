package store

// Catalog represents the service catalog
type Catalog struct {
	Services []Service `json:"services"`
}

// Service represents a service offering
type Service struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Bindable    bool             `json:"bindable"`
	Plans       []ServicePlan    `json:"plans"`
	Metadata    *ServiceMetadata `json:"metadata,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
	Requires    []string         `json:"requires,omitempty"`
}

// ServiceMetadata contains optional metadata for a service
type ServiceMetadata struct {
	DisplayName         string `json:"displayName,omitempty"`
	ImageURL            string `json:"imageUrl,omitempty"`
	LongDescription     string `json:"longDescription,omitempty"`
	ProviderDisplayName string `json:"providerDisplayName,omitempty"`
	DocumentationURL    string `json:"documentationUrl,omitempty"`
	SupportURL          string `json:"supportUrl,omitempty"`
}

// ServicePlan represents a service plan
type ServicePlan struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Free        *bool                  `json:"free,omitempty"`
	Bindable    *bool                  `json:"bindable,omitempty"`
}

// ServiceStore defines the interface for storing and retrieving service catalog data
type ServiceStore interface {
	GetCatalog() (*Catalog, error)
}

// InMemoryStore implements ServiceStore with in-memory storage
type InMemoryStore struct {
	catalog *Catalog
}

// NewInMemoryStore creates a new in-memory store with default catalog
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		catalog: createDefaultCatalog(),
	}
}

// GetCatalog returns the service catalog
func (s *InMemoryStore) GetCatalog() (*Catalog, error) {
	return s.catalog, nil
}

// createDefaultCatalog creates a sample catalog for testing
func createDefaultCatalog() *Catalog {
	free := true
	bindable := true

	return &Catalog{
		Services: []Service{
			{
				ID:          "service-1",
				Name:        "example-service",
				Description: "Example Service for OSB API Reference Implementation",
				Bindable:    true,
				Plans: []ServicePlan{
					{
						ID:          "plan-free",
						Name:        "free",
						Description: "Free plan with limited resources",
						Free:        &free,
						Bindable:    &bindable,
						Metadata: map[string]interface{}{
							"cost": "0",
							"tier": "basic",
						},
					},
					{
						ID:          "plan-premium",
						Name:        "premium",
						Description: "Premium plan with full resources",
						Free:        func() *bool { b := false; return &b }(),
						Bindable:    &bindable,
						Metadata: map[string]interface{}{
							"cost": "99",
							"tier": "advanced",
						},
					},
				},
				Metadata: &ServiceMetadata{
					DisplayName:         "Example Service",
					ImageURL:            "https://example.com/icon.png",
					LongDescription:     "A comprehensive example service for demonstrating OSB API compliance",
					ProviderDisplayName: "Example Corp",
					DocumentationURL:    "https://docs.example.com",
					SupportURL:          "https://support.example.com",
				},
				Tags: []string{"example", "reference", "osb"},
			},
			{
				ID:          "service-2",
				Name:        "database-service",
				Description: "Example Database Service",
				Bindable:    true,
				Plans: []ServicePlan{
					{
						ID:          "plan-small",
						Name:        "small",
						Description: "Small database instance",
						Free:        &free,
						Bindable:    &bindable,
					},
					{
						ID:          "plan-large",
						Name:        "large",
						Description: "Large database instance",
						Free:        func() *bool { b := false; return &b }(),
						Bindable:    &bindable,
					},
				},
			},
		},
	}
}
