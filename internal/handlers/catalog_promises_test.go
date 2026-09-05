package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Eine Zusage im Katalog, die das Verhalten nicht haelt, scheitert erst beim
// Anwender - und zwar auf einer Plattform, die niemand hier betreibt. Diese
// Datei haelt jede Zusage gegen die Route, die sie einloesen muss.
//
// `instances_retrievable` und `bindings_retrievable` sind Aussagen ueber den
// Broker, nicht ueber den Operator: die GET-Endpunkte sind fuer jede Definition
// registriert. Deshalb stehen sie fest im Katalog - und deshalb braucht es
// hier den Gegenbeweis, dass sie stimmen.

func catalogService(t *testing.T, router *gin.Engine) map[string]interface{} {
	t.Helper()
	w := perform(router, "/v2/catalog", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Services []map[string]interface{} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Services)
	return body.Services[0]
}

func getWithVersion(router *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	req.Header.Set("X-Broker-API-Version", "2.17")
	router.ServeHTTP(w, req)
	return w
}

func TestKatalogzusage_AbrufbareInstanzenGibtEsWirklich(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	require.Equal(t, true, catalogService(t, router)["instances_retrievable"],
		"der Katalog sagt die Abrufbarkeit zu")

	const instanceID = "promise-inst-1"
	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-free"}).Code)

	w := getWithVersion(router, "/v2/service_instances/"+instanceID)

	assert.Equal(t, http.StatusOK, w.Code,
		"instances_retrievable zugesagt, GET liefert aber %d: %s", w.Code, w.Body.String())
}

func TestKatalogzusage_AbrufbareBindingsGibtEsWirklich(t *testing.T) {
	router, oc := newSpecBindingRouter(t)
	require.Equal(t, true, catalogService(t, router)["bindings_retrievable"])

	const instanceID = "promise-inst-2"
	const bindingID = "promise-bind-2"
	body := map[string]interface{}{"service_id": "spec-svc-0001", "plan_id": "spec-plan-free"}

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, body).Code)
	operatorSecret(t, oc, instanceID)
	require.Equal(t, http.StatusCreated, putJSON(router,
		"/v2/service_instances/"+instanceID+"/service_bindings/"+bindingID, body).Code)

	w := getWithVersion(router, "/v2/service_instances/"+instanceID+"/service_bindings/"+bindingID)

	assert.Equal(t, http.StatusOK, w.Code,
		"bindings_retrievable zugesagt, GET liefert aber %d: %s", w.Code, w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "credentials",
		"ein abrufbares Binding ohne credentials loest die Zusage nicht ein")
}

// Sagt der Katalog den Planwechsel zu, muss ein PATCH mit neuem plan_id ihn
// auch vollziehen. Die Testdefinition sagt ihn nicht zu - also darf sie es
// auch nicht behaupten. Beide Richtungen stehen hier, weil eine falsche
// Zusage genauso teuer ist wie eine fehlende.
func TestKatalogzusage_PlanwechselWirdNurZugesagtWennErGilt(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	svc := catalogService(t, router)

	zugesagt, ok := svc["plan_updateable"].(bool)
	require.True(t, ok, "plan_updateable muss im Katalog stehen, auch als false")
	if !zugesagt {
		t.Skip("die Testdefinition sagt keinen Planwechsel zu - nichts einzuloesen")
	}

	const instanceID = "promise-inst-3"
	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-free"}).Code)

	w := sendJSON(router, "PATCH", "/v2/service_instances/"+instanceID,
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-paid"})
	assert.Equal(t, http.StatusOK, w.Code, "Planwechsel zugesagt, PATCH scheitert aber: %s", w.Body.String())
}

// Der Anzeigeblock ist das, was ein Marktplatz rendert. Er muss unveraendert
// durchkommen - ein Broker, der ihn umformt, liefert eine Kachel, die der
// Betreiber so nicht geschrieben hat.
func TestKatalogzusage_AnzeigeblockKommtUnveraendertAn(t *testing.T) {
	svc := catalogService(t, mustRouter(t))

	meta, ok := svc["metadata"].(map[string]interface{})
	require.True(t, ok, "die Testdefinition traegt einen Anzeigeblock: %v", svc)
	assert.Equal(t, "Test-Datenbank", meta["displayName"])
}

// Der Handler darf den Katalog nicht ein zweites Mal bauen. Genau das tat er:
// eine handgeschriebene Map mit fest verdrahtetem "free": true und
// "plan_updateable": true - der Broker versprach jedem Marktplatz einen
// Planwechsel, den er fuer keinen Operator nachgewiesen hatte, und bewarb
// jeden Plan als kostenlos. Zwei Quellen fuer dieselbe Aussage laufen
// auseinander; hier ist es eine.
func TestKatalogzusage_DerHandlerBautDenKatalogNichtNach(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	var ueberDieLeitung struct {
		Services []map[string]interface{} `json:"services"`
	}
	w := perform(router, "/v2/catalog", nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ueberDieLeitung))

	entries := testEngine.Catalog()
	raw, err := json.Marshal(entries)
	require.NoError(t, err)
	var ausDerEngine []map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &ausDerEngine))

	assert.Equal(t, ausDerEngine, ueberDieLeitung.Services,
		"was ueber die Leitung geht, muss Zeichen fuer Zeichen der Katalog der Engine sein")
}

func mustRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router, _ := newDefinitionRouter(t)
	return router
}

// Jeder Plan sagt, ob er kostenlos ist - und zwar ausdruecklich. Fehlt das
// Feld, gilt laut OSB `true`, und ein kostenpflichtiger Plan bewirbt sich als
// kostenlos.
func TestKatalogzusage_JederPlanSagtObErKostenlosIst(t *testing.T) {
	svc := catalogService(t, mustRouter(t))
	plans, ok := svc["plans"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, plans)

	for _, raw := range plans {
		p := raw.(map[string]interface{})
		require.Contains(t, p, "free", "Plan %v laesst free weg", p["name"])
		assert.IsType(t, true, p["free"])
	}
}

// Der Broker gibt die Bereitschaftspruefung nach einer Frist auf. Fragt die
// Plattform danach weiter, wartet sie auf eine Antwort, die nicht mehr kommt.
func TestKatalogzusage_JederPlanNenntSeinePollfrist(t *testing.T) {
	svc := catalogService(t, mustRouter(t))

	for _, raw := range svc["plans"].([]interface{}) {
		p := raw.(map[string]interface{})
		d, ok := p["maximum_polling_duration"].(float64)
		require.True(t, ok, "Plan %v nennt keine Pollfrist", p["name"])
		assert.Greater(t, d, float64(0))
	}
}
