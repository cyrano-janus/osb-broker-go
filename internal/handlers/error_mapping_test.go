package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Zuordnung Fehler -> Statuscode haengt am Fehlerwert, nicht am
// Fehlertext. Vorher entschied strings.Contains: jeder Fehler mit "not found"
// wurde auf einem DELETE zu 410 Gone. Die Plattform leitet aus dem Statuscode
// ihr Retry-Verhalten ab.

type errResp struct {
	Error       string `json:"error"`
	Description string `json:"description"`
}

func parseErr(t *testing.T, w *httptest.ResponseRecorder) errResp {
	t.Helper()
	var r errResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r), "body: %s", w.Body.String())
	return r
}

func TestErrorMapping_UnbekannterServiceIst400(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := provisionJSON(router, "/v2/service_instances/i-1", map[string]interface{}{
		"service_id": "nope", "plan_id": "def-plan-free",
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "BadRequest", parseErr(t, w).Error)
	assert.NotEmpty(t, w.Header().Get("X-Correlation-ID"), "errors must carry correlation ID")
}

func TestErrorMapping_BindAufUnbekannteInstanzIst404(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := putJSON(router, "/v2/service_instances/ghost/service_bindings/b-1", map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
	})

	// Die Instanz, auf die sich der Request bezieht, gibt es nicht.
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	r := parseErr(t, w)
	assert.Equal(t, "NotFound", r.Error)
	assert.NotEmpty(t, r.Description)
}

func TestErrorMapping_UnbindUnbekannterBindingIst410(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := deleteJSON(router, "/v2/service_instances/i-x/service_bindings/b-x?service_id=def-svc-0001")

	assert.Equal(t, http.StatusGone, w.Code)
}

func TestErrorMapping_DeprovisionUnbekannterInstanzIst410(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := deleteJSON(router, "/v2/service_instances/i-x?service_id=def-svc-0001")

	assert.Equal(t, http.StatusGone, w.Code)
}

func TestErrorMapping_UnbekannterPlanIstAuchBeimDeleteKein410(t *testing.T) {
	// Der Fall, den die Textsuche nicht unterscheiden konnte: der Fehlertext
	// eines unbekannten Plans enthaelt "not found", und auf einem DELETE
	// wurde daraus 410 Gone - die Plattform las das als "Instanz ist weg" und
	// buchte sie ab, obwohl nie etwas geloescht wurde.
	router, _ := newDefinitionRouter(t)
	const instanceID = "plan-fehler-1"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	w := patchJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "gibt-es-nicht",
	})

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, parseErr(t, w).Description, "unknown plan")
}
