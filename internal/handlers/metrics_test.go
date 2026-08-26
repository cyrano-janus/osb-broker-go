package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMetricsTestRouter(t *testing.T) (*gin.Engine, *Metrics) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	b := broker.New(store.NewInMemoryStore(), nil)
	h := New(b)
	m := NewMetrics()
	h.SetMetrics(m)
	return h.SetupRouter(), m
}

func TestMetrics_EndpointServesPrometheusText(t *testing.T) {
	router, _ := newMetricsTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}

func TestMetrics_RequestsCounted(t *testing.T) {
	router, _ := newMetricsTestRouter(t)

	// Auth is disabled in this fixture; hit the catalog twice.
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/v2/catalog", nil)
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, req)

	body := w.Body.String()
	assert.Contains(t, body, `osb_requests_total{method="GET",path="/v2/catalog",status="200"} 2`)
}

func TestMetrics_UnauthenticatedRequestsCounted(t *testing.T) {
	// The middleware must run BEFORE auth so 401s are visible.
	gin.SetMode(gin.TestMode)
	b := broker.New(store.NewInMemoryStore(), nil)
	h := New(b)
	m := NewMetrics()
	h.SetMetrics(m)
	h.SetBasicAuthCredentials("u", "p")
	router := h.SetupRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, w.Body.String(),
		`osb_requests_total{method="GET",path="/v2/catalog",status="401"} 1`)
}

func TestMetrics_RoutePatternLowCardinality(t *testing.T) {
	router, _ := newMetricsTestRouter(t)

	// Two different instance ids must map to the SAME route pattern.
	for _, id := range []string{"inst-aaa", "inst-bbb"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodDelete,
			"/v2/service_instances/"+id+"?service_id=service-1&plan_id=plan-free", nil)
		router.ServeHTTP(w, req)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, req)
	body := w.Body.String()

	assert.NotContains(t, body, "inst-aaa", "instance ids must not leak into metrics")
	assert.Contains(t, body, `osb_requests_total{method="DELETE",path="/v2/service_instances/:instance_id"`)
}

func TestMetrics_DisabledWhenNotSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	b := broker.New(store.NewInMemoryStore(), nil)
	h := New(b)
	assert.False(t, h.metricsEnabled())
	router := h.SetupRouter()

	// No /metrics route registered.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
