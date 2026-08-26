package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 1.4: central OSB error mapping. All error responses share one
// JSON shape and carry the correlation ID.

type errResp struct {
	Error       string `json:"error"`
	Description string `json:"description"`
}

func parseErr(t *testing.T, w *httptest.ResponseRecorder) errResp {
	t.Helper()
	var r errResp
	require.NoError(t, json.Unmarshal([]byte(w.Body.String()), &r), "body: %s", w.Body.String())
	return r
}

func newErrTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	b := broker.New(store.NewInMemoryStore(), nil)
	h := New(b)
	return h.SetupRouter()
}

func TestErrorMapping_ProvisionUnknownService400(t *testing.T) {
	router := newErrTestRouter(t)
	body := `{"service_id":"nope","plan_id":"plan-free","context":{"platform":"cloudfoundry"}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/i-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	r := parseErr(t, w)
	assert.Equal(t, "BadRequest", r.Error)
	assert.NotEmpty(t, w.Header().Get("X-Correlation-ID"), "errors must carry correlation ID")
}

func TestErrorMapping_BindOnMissingInstance404(t *testing.T) {
	router := newErrTestRouter(t)
	body := `{"service_id":"service-1","plan_id":"plan-free","context":{"platform":"cloudfoundry"}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/ghost/service_bindings/b-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// OSB spec: binding against unknown instance is a client error (404
	// NotFound — the resource the request refers to does not exist).
	assert.Equal(t, http.StatusNotFound, w.Code)
	r := parseErr(t, w)
	assert.Equal(t, "NotFound", r.Error)
	assert.NotEmpty(t, r.Description)
}

func TestErrorMapping_UnbindUnknownBinding410Gone(t *testing.T) {
	router := newErrTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/v2/service_instances/i-x/service_bindings/b-x?service_id=service-1&plan_id=plan-free", nil)
	router.ServeHTTP(w, req)

	// OSB spec: DELETE on non-existent binding -> 410 Gone
	assert.Equal(t, http.StatusGone, w.Code)
}

func TestErrorMapping_DeprovisionUnknownInstance410Gone(t *testing.T) {
	router := newErrTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/v2/service_instances/i-x?service_id=service-1&plan_id=plan-free", nil)
	router.ServeHTTP(w, req)

	// OSB spec: DELETE on non-existent instance -> 410 Gone
	assert.Equal(t, http.StatusGone, w.Code)
}
