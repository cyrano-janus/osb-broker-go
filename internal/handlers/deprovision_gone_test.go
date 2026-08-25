package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const legacyServiceIDFixture = "service-1"
const legacyPlanIDFixture = "plan-free"

// TDD: Definition-basiertes Deprovision einer nicht existierenden Instanz
// muss 410 Gone liefern (OSB-Spec), nicht 200.
func TestDeprovisionNonexistentDefinitionInstanceGone(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/v2/service_instances/no-such-inst?service_id=def-svc-0001&plan_id=def-plan-free", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusGone, w.Code, "body: %s", w.Body.String())
}

// Der Legacy-Pfad (example-service) muss unverändert 410 liefern.
func TestDeprovisionNonexistentLegacyInstanceGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	b := broker.New(store.NewInMemoryStore(), nil)
	h := New(b)
	router := h.SetupRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/v2/service_instances/no-such-legacy?service_id="+legacyServiceIDFixture+"&plan_id="+legacyPlanIDFixture, nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusGone, w.Code)
}
