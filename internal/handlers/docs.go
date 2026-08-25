package handlers

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Die Specs werden zur Compile-Zeit eingebettet — das Binary ist damit
// selbst-dokumentierend, ohne Dateisystem-Abhängigkeit (distroless).
var (
	//go:embed docs/openapi.yaml
	openAPISpec []byte

	//go:embed docs/service-definition.schema.json
	serviceDefSchema []byte
)

// ServeOpenAPISpec handles GET /openapi.yaml (unauthenticated).
func (h *Handlers) ServeOpenAPISpec(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml", openAPISpec)
}

// ServeServiceDefinitionSchema handles GET /schemas/service-definition.schema.json.
func (h *Handlers) ServeServiceDefinitionSchema(c *gin.Context) {
	c.Data(http.StatusOK, "application/schema+json", serviceDefSchema)
}

// DocsRoutes registers the documentation endpoints (unauthenticated,
// outside the /v2 API surface and Basic Auth).
func (h *Handlers) DocsRoutes(router *gin.Engine) {
	router.GET("/openapi.yaml", h.ServeOpenAPISpec)
	router.GET("/schemas/service-definition.schema.json", h.ServeServiceDefinitionSchema)
}
