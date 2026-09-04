package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Broker hat einen Pfad. Was hier geprueft wird, ist genau das: dass es
// keine zweite Implementierung mehr gibt, in die ein Request fallen kann.
//
// Frueher stand an dieser Stelle eine Testdatei, die durchgehend gegen
// "service-1" lief - einen Demo-Service aus einem statischen Katalog, der mit
// den ServiceDefinitions nichts zu tun hatte. Sie war gruen, waehrend die
// Engine ungeprueft blieb.

func TestKatalog_EnthaeltNurDefinitionen(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := getJSON(router, "/v2/catalog")
	require.Equal(t, http.StatusOK, w.Code)

	var catalog struct {
		Services []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Plans []struct {
				ID string `json:"id"`
			} `json:"plans"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalog))

	require.Len(t, catalog.Services, 1, "nur der Service aus der ServiceDefinition")
	assert.Equal(t, "def-svc-0001", catalog.Services[0].ID)
	assert.Len(t, catalog.Services[0].Plans, 2)

	// Der Demo-Katalog stand frueher in jeder Antwort - auch in einem
	// produktiven Marketplace, und es gab keinen Schalter dagegen.
	body := w.Body.String()
	assert.NotContains(t, body, "service-1")
	assert.NotContains(t, body, "service-2")
}

func TestKatalog_OhneDefinitionenIstLeerUndKeinDemoKatalog(t *testing.T) {
	// Ein Broker ohne geladene Definitionen hat nichts anzubieten. Das ist
	// eine leere Liste - nicht der Rueckfall auf zwei erfundene Services.
	gin.SetMode(gin.TestMode)
	h := New(nil)
	router := h.SetupRouter()

	w := getJSON(router, "/v2/catalog")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"services":[]}`, w.Body.String())
}

func TestProvision_UnbekannterServiceIst400UndKeinRueckfall(t *testing.T) {
	// Der Kern des Umbaus: eine service_id, die keiner Definition entspricht,
	// ist ein Fehler. Vorher lief der Request stumm in den zweiten Broker.
	router, _ := newDefinitionRouter(t)

	w := provisionJSON(router, "/v2/service_instances/unbekannt-1", map[string]interface{}{
		"service_id": "service-1", "plan_id": "plan-1",
	})

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "unknown service")
}

func TestProvision_UnbekannterPlanIst400(t *testing.T) {
	// Ein Plan, den es im Service nicht gibt, ist ebenfalls ein Katalogfehler
	// des Aufrufers - und ausdruecklich kein 410, obwohl der Fehlertext
	// "not found" enthaelt.
	router, _ := newDefinitionRouter(t)

	w := provisionJSON(router, "/v2/service_instances/unbekannt-2", map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "gibt-es-nicht",
	})

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "unknown plan")
}

func TestProvision_FehlendeFelderSind400(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	ohneService := provisionJSON(router, "/v2/service_instances/x", map[string]interface{}{
		"plan_id": "def-plan-free",
	})
	assert.Equal(t, http.StatusBadRequest, ohneService.Code)
	assert.Contains(t, ohneService.Body.String(), "service_id is required")

	ohnePlan := provisionJSON(router, "/v2/service_instances/x", map[string]interface{}{
		"service_id": "def-svc-0001",
	})
	assert.Equal(t, http.StatusBadRequest, ohnePlan.Code)
	assert.Contains(t, ohnePlan.Body.String(), "plan_id is required")
}

func TestGetInstance_UnbekannteIst404(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := getJSON(router, "/v2/service_instances/gibt-es-nicht")

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestGetInstance_LiefertServiceUndPlan(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	const instanceID = "get-inst-1"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	w := getJSON(router, "/v2/service_instances/"+instanceID)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "def-svc-0001", resp["service_id"])
	assert.Equal(t, "def-plan-free", resp["plan_id"])
}

func TestBindingLastOperation_UnbekannteIst410(t *testing.T) {
	// Frueher antwortete diese Stelle hart "succeeded" - fuer jede
	// Binding-ID, auch fuer eine, die nie existiert hat.
	router, _ := newDefinitionRouter(t)

	w := getJSON(router, "/v2/service_instances/i/service_bindings/gibt-es-nicht/last_operation")

	assert.Equal(t, http.StatusGone, w.Code, w.Body.String())
}
