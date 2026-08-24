package handlers

import (
	"net/http"

	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
)

// Handlers contains HTTP handlers for the OSB API
type Handlers struct {
	broker *broker.Broker
}

// New creates a new Handlers instance
func New(b *broker.Broker) *Handlers {
	return &Handlers{
		broker: b,
	}
}

// Healthz handles GET /healthz for Kubernetes liveness/readiness probes
func (h *Handlers) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// SetupRouter configures the Gin router with all OSB API routes
func (h *Handlers) SetupRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// Health check (outside API version middleware, no X-Broker-API-Version required)
	router.GET("/healthz", h.Healthz)

	// Middleware to check API version
	router.Use(h.apiVersionMiddleware)

	// Catalog endpoint
	router.GET("/v2/catalog", h.GetCatalog)

	// Service Instance endpoints
	router.PUT("/v2/service_instances/:instance_id", h.ProvisionServiceInstance)
	router.DELETE("/v2/service_instances/:instance_id", h.DeprovisionServiceInstance)
	router.PATCH("/v2/service_instances/:instance_id", h.UpdateServiceInstance)
	router.GET("/v2/service_instances/:instance_id", h.GetServiceInstance)
	router.GET("/v2/service_instances/:instance_id/last_operation", h.GetLastOperation)

	// Service Binding endpoints
	router.PUT("/v2/service_instances/:instance_id/service_bindings/:binding_id", h.BindServiceInstance)
	router.DELETE("/v2/service_instances/:instance_id/service_bindings/:binding_id", h.UnbindServiceInstance)
	router.GET("/v2/service_instances/:instance_id/service_bindings/:binding_id", h.GetBinding)
	router.GET("/v2/service_instances/:instance_id/service_bindings/:binding_id/last_operation", h.GetLastBindingOperation)

	return router
}

// apiVersionMiddleware checks for required API version header
func (h *Handlers) apiVersionMiddleware(c *gin.Context) {
	version := c.GetHeader("X-Broker-API-Version")
	if version == "" {
		// Allow requests without version header for backwards compatibility
		// In production, you might want to enforce this
		c.Set("api-version", broker.APIVersion)
	} else {
		c.Set("api-version", version)
	}
	c.Next()
}