package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDocsTestRouter(t *testing.T, withAuth bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	b := broker.New(nil)
	h := New(b)
	if withAuth {
		h.SetBasicAuthCredentials("u", "p")
	}
	return h.SetupRouter()
}

func TestDocsRoutes_ServeOpenAPISpec(t *testing.T) {
	router := newDocsTestRouter(t, false)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "yaml")
	body := w.Body.String()
	assert.Contains(t, body, "openapi: 3.0.3")
	assert.Contains(t, body, "/v2/service_instances/{instance_id}")
}

func TestDocsRoutes_ServeServiceDefinitionSchema(t *testing.T) {
	router := newDocsTestRouter(t, false)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/schemas/service-definition.schema.json", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "ServiceDefinition")
	assert.Contains(t, body, "draft-07")
}

func TestDocsRoutes_UnauthenticatedButOSBAuthUntouched(t *testing.T) {
	router := newDocsTestRouter(t, true)

	getCode := func(path string, withAuth bool) int {
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Broker-API-Version", "2.17")
		if withAuth {
			req.SetBasicAuth("u", "p")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	// Docs ohne Auth erreichbar
	assert.Equal(t, http.StatusOK, getCode("/openapi.yaml", false))
	// /v2/catalog ohne Auth weiterhin 401
	assert.Equal(t, http.StatusUnauthorized, getCode("/v2/catalog", false))
	// /v2/catalog mit Auth 200
	assert.Equal(t, http.StatusOK, getCode("/v2/catalog", true))
}
