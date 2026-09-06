package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `maintenance_info` ist der Weg, auf dem ein neuer Stand ANGEBOTEN wird statt
// verhaengt zu werden: die Plattform haelt den Stand der Instanz gegen den des
// Plans, zeigt die Differenz als `upgrade available`, und der Besitzer loest
// `cf upgrade-service` aus.
//
// Damit das traegt, muessen drei Dinge stimmen: der Katalog nennt den Stand,
// die Instanz merkt sich den ANGEWENDETEN, und ein Request mit veraltetem
// Stand wird abgelehnt statt auf gut Glueck ausgefuehrt.

const planStand = "1.2.0"

// newMaintenanceRouter gibt dem freien Plan einen Stand; der bezahlte bleibt
// ohne - beide Faelle werden gebraucht.
func newMaintenanceRouter(t *testing.T) *gin.Engine {
	t.Helper()
	defYAML := strings.Replace(testDefYAML,
		"      - id: def-plan-free\n        name: free\n",
		"      - id: def-plan-free\n        name: free\n        maintenanceInfo:\n"+
			"          version: \""+planStand+"\"\n          description: \"Testbank 18.6\"\n", 1)
	require.Contains(t, defYAML, "maintenanceInfo", "die Testdefinition wurde nicht umgebaut")
	router, _ := routerForYAML(t, defYAML)
	return router
}

// getBody liest die JSON-Antwort eines GET.
func getBody(t *testing.T, router *gin.Engine, path string) map[string]interface{} {
	t.Helper()
	w := getJSON(router, path)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func provisionFree(t *testing.T, router *gin.Engine, id string, body map[string]interface{}) int {
	t.Helper()
	body["service_id"] = "def-svc-0001"
	body["plan_id"] = "def-plan-free"
	return provisionJSON(router, "/v2/service_instances/"+id, body).Code
}

func TestWartung_KatalogNenntDenStandDesPlans(t *testing.T) {
	svc := catalogService(t, newMaintenanceRouter(t))

	stand := map[string]interface{}{}
	for _, raw := range svc["plans"].([]interface{}) {
		p := raw.(map[string]interface{})
		stand[p["name"].(string)] = p["maintenance_info"]
	}
	require.NotNil(t, stand["free"], "der Plan mit Stand nennt ihn nicht")
	assert.Equal(t, planStand, stand["free"].(map[string]interface{})["version"])
	assert.Nil(t, stand["paid"], "ein Plan ohne Stand darf kein leeres Objekt tragen")
}

// Ohne Angabe wird nicht geprueft: die Plattform MUSS das Feld nicht senden.
func TestWartung_ProvisionOhneAngabeBleibtErlaubt(t *testing.T) {
	assert.Equal(t, http.StatusAccepted,
		provisionFree(t, newMaintenanceRouter(t), "wartung-1", map[string]interface{}{}))
}

func TestWartung_ProvisionMitPassendemStandGehtDurch(t *testing.T) {
	assert.Equal(t, http.StatusAccepted, provisionFree(t, newMaintenanceRouter(t), "wartung-2",
		map[string]interface{}{"maintenance_info": map[string]interface{}{"version": planStand}}))
}

// Der teure Fall: die Katalogkopie der Plattform ist veraltet. Ein Provision
// auf gut Glueck legte eine Instanz an, deren Stand niemand kennt.
func TestWartung_ProvisionMitVeraltetemStandIstEinKonflikt(t *testing.T) {
	w := provisionJSON(newMaintenanceRouter(t), "/v2/service_instances/wartung-3",
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-free",
			"maintenance_info": map[string]interface{}{"version": "1.0.0"}})

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "MaintenanceInfoConflict")
}

func TestWartung_StandFuerEinenPlanDerKeinenFuehrtIstEinKonflikt(t *testing.T) {
	w := provisionJSON(newMaintenanceRouter(t), "/v2/service_instances/wartung-4",
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-paid",
			"maintenance_info": map[string]interface{}{"version": "1.0.0"}})

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code,
		"die Plattform glaubt an einen Stand, den dieser Broker nicht fuehrt: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "MaintenanceInfoConflict")
}

// Gespeichert wird der ANGEWENDETE Stand, nicht der behauptete - auch wenn die
// Plattform gar keinen mitschickt.
func TestWartung_InstanzMerktSichDenStandDesPlans(t *testing.T) {
	router := newMaintenanceRouter(t)
	require.Equal(t, http.StatusAccepted, provisionFree(t, router, "wartung-5", map[string]interface{}{}))

	got := getBody(t, router, "/v2/service_instances/wartung-5")
	mi, ok := got["maintenance_info"].(map[string]interface{})
	require.True(t, ok, "GET meldet keinen Stand: %v", got)
	assert.Equal(t, planStand, mi["version"])
}

func TestWartung_InstanzOhneStandMeldetKeinen(t *testing.T) {
	router := newMaintenanceRouter(t)
	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/wartung-6",
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-paid"}).Code)

	_, vorhanden := getBody(t, router, "/v2/service_instances/wartung-6")["maintenance_info"]
	assert.False(t, vorhanden, "ohne Stand darf das Feld nicht in der Antwort stehen")
}

// Der ganze Vorgang, den CF `cf upgrade-service` nennt: die Instanz steht auf
// dem alten Stand, der Katalog hebt ihn, der Besitzer loest aus - und erst
// danach traegt die Instanz den neuen.
func TestWartung_UpgradeHebtDenStandDerInstanz(t *testing.T) {
	router := newMaintenanceRouter(t)
	require.Equal(t, http.StatusAccepted, provisionFree(t, router, "wartung-7", map[string]interface{}{}))

	// Der Betreiber hebt den Stand des Plans.
	sd, err := testEngine.DefinitionByServiceID("def-svc-0001")
	require.NoError(t, err)
	sd.Spec.Offering.Plans[0].MaintenanceInfo.Version = "1.3.0"

	// Die Instanz steht weiterhin auf dem alten - genau das meldet die
	// Plattform als `upgrade available`.
	mi := getBody(t, router, "/v2/service_instances/wartung-7")["maintenance_info"].(map[string]interface{})
	require.Equal(t, planStand, mi["version"], "die Instanz darf nicht ungefragt nachgezogen sein")

	// Ein Upgrade auf den alten Stand ist jetzt ein Konflikt.
	w := sendJSON(router, "PATCH", "/v2/service_instances/wartung-7",
		map[string]interface{}{"service_id": "def-svc-0001",
			"maintenance_info": map[string]interface{}{"version": planStand}})
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())

	// Und auf den neuen geht durch.
	w = sendJSON(router, "PATCH", "/v2/service_instances/wartung-7",
		map[string]interface{}{"service_id": "def-svc-0001",
			"maintenance_info": map[string]interface{}{"version": "1.3.0"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	mi = getBody(t, router, "/v2/service_instances/wartung-7")["maintenance_info"].(map[string]interface{})
	assert.Equal(t, "1.3.0", mi["version"], "nach dem Upgrade traegt die Instanz den neuen Stand")
}
