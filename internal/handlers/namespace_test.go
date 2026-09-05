package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/osb-broker/internal/definition"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8scorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FINDINGS #3/#16/#7: Instanzen landeten unabhaengig vom Cloud-Foundry-Space
// alle im Namespace "default" - es gab also keine Mandantentrennung bei den
// Backing-Ressourcen. Ursache war eine Kette: die Space-GUID kam nicht an (#3),
// drei Codepfade setzten den Namespace hart (#16), und deprovision leitete ihn
// gar nicht erst ab (#7).

const spaceNS = "space-11111111-2222-3333-4444-555555555555"

func getJSON(router *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	// Wie eine echte Plattform: ohne den Header ist die Antwort 412.
	req.Header.Set("X-Broker-API-Version", "2.17")
	router.ServeHTTP(w, req)
	return w
}

func crIn(t *testing.T, oc *definition.OperatorClient, namespace, instanceID string) (*unstructured.Unstructured, error) {
	t.Helper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "test.example.com", Version: "v1", Kind: "Database"})
	u.SetNamespace(namespace)
	u.SetName(definition.SanitizeInstanceName(instanceID))
	err := oc.Client.Get(context.Background(), client.ObjectKeyFromObject(u), u)
	return u, err
}

func TestNamespace_ProvisionLandetImSpaceNamespace(t *testing.T) {
	// Der Kern von #3: Korifi schickt space_guid ausschliesslich Top-Level.
	router, oc := newDefinitionRouter(t)
	const instanceID = "ns-inst-1"

	w := provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
		"organization_guid": "org-1", "space_guid": spaceNS,
	})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	_, err := crIn(t, oc, spaceNS, instanceID)
	require.NoError(t, err, "die Operator-Ressource gehoert in den Space-Namespace")

	_, err = crIn(t, oc, "default", instanceID)
	assert.Error(t, err, "und nicht mehr nach default")
}

func TestNamespace_OhneSpaceGUIDBleibtEsDefault(t *testing.T) {
	// Rueckwaertskompatibilitaet: Plattformen ohne Space-Begriff.
	router, oc := newDefinitionRouter(t)
	const instanceID = "ns-inst-2"

	w := provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
	})
	require.Equal(t, http.StatusAccepted, w.Code)

	_, err := crIn(t, oc, "default", instanceID)
	assert.NoError(t, err)
}

func TestNamespace_DeprovisionLoeschtImRichtigenNamespace(t *testing.T) {
	// #7: der DELETE-Request enthaelt weder context noch space_guid. Wurde der
	// Namespace daraus abgeleitet, loeschte der Broker in "default" - und weil
	// OperatorClient.Delete IsNotFound ignoriert, meldete er Erfolg, waehrend
	// die Datenbank weiterlief.
	router, oc := newDefinitionRouter(t)
	const instanceID = "ns-inst-3"

	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)
	_, err := crIn(t, oc, spaceNS, instanceID)
	require.NoError(t, err)

	w := deleteJSON(router, "/v2/service_instances/"+instanceID+"?service_id=def-svc-0001&plan_id=def-plan-free")
	require.Equal(t, http.StatusOK, w.Code)

	_, err = crIn(t, oc, spaceNS, instanceID)
	assert.Error(t, err, "die Ressource muss im Space-Namespace wirklich verschwinden")
}

func TestNamespace_LastOperationFindetDieInstanzImSpaceNamespace(t *testing.T) {
	// #16: last_operation suchte in "default", fand nichts und fiel auf den
	// Legacy-Pfad zurueck, der hart "succeeded" meldet - Erfolg fuer eine
	// Instanz, die der Broker gar nicht gefunden hat. Erkennbar an der
	// Beschreibung.
	router, oc := newDefinitionRouter(t)
	const instanceID = "ns-inst-4"

	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	// Das CR ist noch nicht ready - der Definitions-Pfad muss das sehen.
	w := getJSON(router, "/v2/service_instances/"+instanceID+"/last_operation?service_id=def-svc-0001")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		State       string `json:"state"`
		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "in progress", resp.State,
		"ohne Ready-Status darf nicht 'succeeded' gemeldet werden")

	// Jetzt auf ready setzen - derselbe Aufruf muss umschlagen.
	cr, err := crIn(t, oc, spaceNS, instanceID)
	require.NoError(t, err)
	cr.Object["status"] = map[string]interface{}{"phase": "Running"}
	require.NoError(t, oc.Client.Update(context.Background(), cr))

	w = getJSON(router, "/v2/service_instances/"+instanceID+"/last_operation?service_id=def-svc-0001")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "succeeded", resp.State)
}

func TestNamespace_BindLiestDasSecretImSpaceNamespace(t *testing.T) {
	// Der Bind-Request traegt ebenfalls keine Space-GUID; der Namespace muss
	// aus der Instanz kommen.
	router, oc := newDefinitionRouter(t)
	const instanceID = "ns-inst-5"

	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	require.NoError(t, oc.Client.Create(context.Background(), &k8scorev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      definition.SanitizeInstanceName(instanceID) + "-creds",
			Namespace: spaceNS,
		},
		Data: map[string][]byte{"username": []byte("app")},
	}))

	w := putJSON(router, "/v2/service_instances/"+instanceID+"/service_bindings/ns-bind-5",
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-free"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "app")
}

func TestNamespace_UnbekannteInstanzFaelltAufDefaultZurueck(t *testing.T) {
	// Kein Datensatz, kein Namespace - dann bleibt es beim bisherigen
	// Verhalten, statt mit einem leeren Namespace zu arbeiten.
	router, _ := newDefinitionRouter(t)
	w := deleteJSON(router, "/v2/service_instances/gibt-es-nicht?service_id=def-svc-0001&plan_id=def-plan-free")
	assert.Equal(t, http.StatusGone, w.Code)
}

func TestNamespace_WirdAmDatensatzGespeichert(t *testing.T) {
	// Der AppliedRefs-Fallback funktioniert, aber er ist die zweite Reihe:
	// er greift nur, solange ueberhaupt Objekte angelegt wurden. Der
	// Namespace gehoert als eigenes Feld an den Datensatz, sonst haengt die
	// Zuordnung an einem Nebeneffekt.
	router, _ := newDefinitionRouter(t)
	const instanceID = "ns-inst-6"

	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	inst, err := testBroker.StoredInstance(context.Background(), instanceID)
	require.NoError(t, err)
	assert.Equal(t, spaceNS, inst.Namespace, "der Namespace muss ohne Umweg ueber AppliedRefs auffindbar sein")
}
