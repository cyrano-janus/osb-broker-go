package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

	// Die Versionsaushandlung haengt an der API-Gruppe, nicht global. Global
	// traefe sie auch jeden Pfad, den es gar nicht gibt - eine Anfrage an
	// /metrics bei abgeschalteten Metriken bekaeme dann 412 statt 404, und
	// der Aufrufer suchte den Fehler bei seinem Header statt bei der URL.
	//
	// Die Authentifizierung bleibt bewusst global: eine unbekannte URL soll
	// ohne Zugangsdaten mit 401 antworten und nicht verraten, welche Pfade es
	// gibt.
	api := router.Group("", h.apiVersionMiddleware)

	// Catalog endpoint
	api.GET("/v2/catalog", h.GetCatalog)

	// Service Instance endpoints
	api.PUT("/v2/service_instances/:instance_id", h.ProvisionServiceInstance)
	api.DELETE("/v2/service_instances/:instance_id", h.DeprovisionServiceInstance)
	api.PATCH("/v2/service_instances/:instance_id", h.UpdateServiceInstance)
	api.GET("/v2/service_instances/:instance_id", h.GetServiceInstance)
	api.GET("/v2/service_instances/:instance_id/last_operation", h.GetLastOperation)

	// Service Binding endpoints
	api.PUT("/v2/service_instances/:instance_id/service_bindings/:binding_id", h.BindServiceInstance)
	api.DELETE("/v2/service_instances/:instance_id/service_bindings/:binding_id", h.UnbindServiceInstance)
	api.GET("/v2/service_instances/:instance_id/service_bindings/:binding_id", h.GetBinding)
	api.GET("/v2/service_instances/:instance_id/service_bindings/:binding_id/last_operation", h.GetLastBindingOperation)

	return router
}

// apiVersionMiddleware setzt die Versionsaushandlung aus OSB 2.17 durch.
//
// Der Header ist Pflicht, und ein Broker, der die genannte Version nicht
// bedienen kann, antwortet mit `412 Precondition Failed`. Vorher wurde ein
// fehlender Header still durch die eigene Version ersetzt: ein Aufrufer, der
// ihn vergisst, bekam damit Antworten nach einer Version, auf die er sich nie
// geeinigt hat - und haette es erst gemerkt, wenn sich eine Bedeutung aendert.
//
// Was durchgeht, richtet sich nach der Hauptversion. Eine andere Hauptversion
// ist eine andere Schnittstelle; eine neuere Nebenversion nicht: die Plattform
// nennt, was sie zu sprechen bereit ist, und ein Broker, der weniger kann,
// antwortet mit dem, was er kann. Ein 412 waere dort eine Ablehnung, die die
// Spezifikation nicht verlangt.
//
// Die freien Pfade - /healthz, /metrics, /openapi.yaml, /schemas - haengen
// bewusst vor dieser Middleware: ein Liveness-Probe schickt keinen OSB-Header.
func (h *Handlers) apiVersionMiddleware(c *gin.Context) {
	version := c.GetHeader("X-Broker-API-Version")
	if version == "" {
		abortPreconditionFailed(c, "X-Broker-API-Version header is required")
		return
	}
	major, ok := majorVersion(version)
	if !ok {
		abortPreconditionFailed(c,
			"X-Broker-API-Version must be of the form <major>.<minor>, got "+strconv.Quote(version))
		return
	}
	if major != supportedMajorVersion {
		abortPreconditionFailed(c, fmt.Sprintf(
			"X-Broker-API-Version %s is not supported; this broker implements %s",
			version, broker.APIVersion))
		return
	}
	c.Set("api-version", version)
	c.Next()
}

// supportedMajorVersion ist die Hauptversion, die dieser Broker bedient.
const supportedMajorVersion = 2

// majorVersion liest die Hauptversion aus "major.minor". Alles andere ist
// keine Versionsangabe - auch nicht "2" allein oder "v2.17".
func majorVersion(v string) (int, bool) {
	major, minor, found := strings.Cut(v, ".")
	if !found || major == "" || minor == "" {
		return 0, false
	}
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0, false
	}
	if _, err := strconv.Atoi(minor); err != nil {
		return 0, false
	}
	return n, true
}

func abortPreconditionFailed(c *gin.Context, description string) {
	c.AbortWithStatusJSON(http.StatusPreconditionFailed, gin.H{
		"error":       "PreconditionFailed",
		"description": description,
	})
}
