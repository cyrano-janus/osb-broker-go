package handlers

import (
	"net/http"

	"github.com/example/osb-broker/internal/auth"
	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
)

// Handlers contains HTTP handlers for the OSB API
type Handlers struct {
	broker *broker.Broker
	// engine provides definition-based services (Phase 2); nil = disabled.
	engine *EngineHolder
	// authChain holds the configured authentication methods (Phase 4.5).
	// nil or empty = authentication disabled (backwards compatibility).
	// main() builds it from the configuration; SetBasicAuthCredentials
	// remains as the shorthand for a basic-only chain.
	authChain *auth.Chain
	// metrics is the Prometheus collector set; nil = metrics disabled.
	metrics *Metrics
}

// SetEngine wires the Generic Engine (Phase 2). nil disables
// definition-based services.
func (h *Handlers) SetEngine(e *EngineHolder) {
	h.engine = e
}

// SetMetrics enables Prometheus metrics collection and registers the
// /metrics endpoint (unauthenticated).
func (h *Handlers) SetMetrics(m *Metrics) {
	h.metrics = m
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
	router.Use(gin.Recovery())
	router.Use(structuredLoggingMiddleware())

	// Health check (outside API version middleware, no X-Broker-API-Version required)
	router.GET("/healthz", h.Healthz)

	// Documentation endpoints (unauthenticated, outside /v2 and Basic Auth)
	h.DocsRoutes(router)

	// Prometheus metrics (unauthenticated, like healthz; scrape-internal).
	if h.metrics != nil {
		router.GET("/metrics", h.metrics.Handler())
		router.Use(h.metrics.MetricsMiddleware())
	}

	// Authentication for all OSB endpoints (healthz, docs and metrics are
	// exempt because they are registered above this line). No-op when no
	// method is configured.
	router.Use(h.authMiddleware)

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