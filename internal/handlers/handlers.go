package handlers

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"

	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
)

// Handlers contains HTTP handlers for the OSB API
type Handlers struct {
	broker *broker.Broker
	// engine provides definition-based services (Phase 2); nil = disabled.
	engine *EngineHolder
	// Basic Auth credentials (Basic Auth user / password). When both are
	// empty, authentication is disabled (backwards compatibility). In
	// Kubernetes these values are injected from a Secret via main().
	authUser string
	authPass string
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

// SetBasicAuthCredentials configures the Basic Auth credentials required on
// all OSB endpoints. Empty user AND password disable authentication.
func (h *Handlers) SetBasicAuthCredentials(user, pass string) {
	h.authUser = user
	h.authPass = pass
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

	// Basic Auth for all OSB endpoints (healthz exempt). No-op when no
	// credentials are configured.
	router.Use(h.basicAuthMiddleware)

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

// basicAuthMiddleware enforces HTTP Basic Auth on all OSB endpoints when
// credentials are configured. Returns 401 with WWW-Authenticate per RFC 7617.
func (h *Handlers) basicAuthMiddleware(c *gin.Context) {
	// Auth disabled when no credentials configured.
	if h.authUser == "" && h.authPass == "" {
		c.Next()
		return
	}

	header := c.GetHeader("Authorization")
	const prefix = "Basic "
	valid := false
	if len(header) > len(prefix) && header[:len(prefix)] == prefix {
		payload, err := base64.StdEncoding.DecodeString(header[len(prefix):])
		if err == nil {
			userPass := string(payload)
			for i := 0; i < len(userPass); i++ {
				if userPass[i] == ':' {
					user, pass := userPass[:i], userPass[i+1:]
					// constant-time compare to avoid timing oracles
					userOK := subtle.ConstantTimeCompare([]byte(user), []byte(h.authUser)) == 1
					passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(h.authPass)) == 1
					valid = userOK && passOK
					break
				}
			}
		}
	}

	if !valid {
		c.Header("WWW-Authenticate", `Basic realm="osb-broker"`)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":       "Unauthorized",
			"description": "Invalid or missing Basic Auth credentials",
		})
		return
	}
	c.Next()
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