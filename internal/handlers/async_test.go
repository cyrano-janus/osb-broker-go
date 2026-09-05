package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cyrano-janus/osb-broker-go/internal/definition"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Der Definitions-Pfad ist asynchron: er legt ein CR an, fertig ist der Dienst
// erst, wenn der Operator ihn hergestellt hat. Bei CloudNativePG sind das
// Minuten.
//
// Frueher antwortete Provision trotzdem synchron mit 201, weil
// accepts_incomplete als Body-Feld modelliert war und die Spezifikation es als
// Query-Parameter uebertraegt - der Zweig war unerreichbar, der gesamte
// last_operation-Apparat lief leer. Die Plattform hielt die Instanz sofort fuer
// fertig und band gegen ein Secret, das der Operator noch nicht geschrieben
// hatte.

func TestAsync_OhneEinverstaendnisIst422(t *testing.T) {
	// OSB 2.17: kann der Broker nur asynchron, und der Aufrufer hat es nicht
	// erlaubt, ist die Antwort 422 AsyncRequired - nicht ein 201, das
	// Fertigstellung behauptet.
	router, _ := newDefinitionRouter(t)

	w := putJSON(router, "/v2/service_instances/async-1", map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
	})

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "AsyncRequired")
}

func TestAsync_MitEinverstaendnisIst202MitOperation(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := provisionJSON(router, "/v2/service_instances/async-2", map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "provision", body["operation"],
		"die Plattform schickt die Kennung bei last_operation wieder mit")
}

func TestAsync_LastOperationMeldetDenEchtenZustand(t *testing.T) {
	// Der Kern der Sache: solange der Operator nicht fertig ist, muss der
	// Broker "in progress" melden - und erst dann "succeeded", wenn die
	// Readiness-Bedingung der Definition wirklich erfuellt ist.
	router, oc := newDefinitionRouter(t)
	const instanceID = "async-3"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	w := getJSON(router, "/v2/service_instances/"+instanceID+"/last_operation?service_id=def-svc-0001")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"state":"in progress"`,
		"das frisch angelegte CR traegt noch keinen Status")

	// Der Operator meldet Vollzug.
	setPhase(t, oc, instanceID, "Running")

	w = getJSON(router, "/v2/service_instances/"+instanceID+"/last_operation?service_id=def-svc-0001")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"state":"succeeded"`)
}

func TestAsync_LastOperationFindetDenServiceAuchOhneQuery(t *testing.T) {
	// service_id ist laut Spezifikation empfohlen, nicht Pflicht. Fehlte es,
	// fiel die Abfrage auf den Fallback-Pfad zurueck, der hart "succeeded"
	// meldet - Erfolg fuer eine Instanz, die gerade erst entsteht.
	router, _ := newDefinitionRouter(t)
	const instanceID = "async-4"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	w := getJSON(router, "/v2/service_instances/"+instanceID+"/last_operation")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"state":"in progress"`)
	assert.NotContains(t, w.Body.String(), `"state":"succeeded"`)
}

func TestAsync_LastOperationEinerUnbekanntenIst410(t *testing.T) {
	// Daran erkennt die Plattform, dass ein Deprovision durch ist: Korifi
	// liest 410 als Abschluss.
	router, _ := newDefinitionRouter(t)

	w := getJSON(router, "/v2/service_instances/gibt-es-nicht/last_operation?service_id=def-svc-0001")

	assert.Equal(t, http.StatusGone, w.Code, w.Body.String())
}

func TestAsync_VerschwundenesObjektIstFehlgeschlagen(t *testing.T) {
	// Datensatz da, Objekt weg: der Vorgang ist gescheitert, nicht "noch
	// unterwegs". Ohne diesen Zweig pollte die Plattform bis in ihr eigenes
	// Zeitlimit.
	router, oc := newDefinitionRouter(t)
	const instanceID = "async-5"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free", "space_guid": spaceNS,
		}).Code)

	cr, err := crIn(t, oc, spaceNS, instanceID)
	require.NoError(t, err)
	require.NoError(t, oc.Client.Delete(context.Background(), cr))

	w := getJSON(router, "/v2/service_instances/"+instanceID+"/last_operation?service_id=def-svc-0001")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"state":"failed"`)
}

// setPhase schreibt den Status, auf den die Test-Definition wartet
// (statusJSONPath: status.phase, expectedValue: Running) - der Operator, den
// es im Test nicht gibt.
func setPhase(t *testing.T, oc *definition.OperatorClient, instanceID, phase string) {
	t.Helper()
	cr, err := crIn(t, oc, spaceNS, instanceID)
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedField(cr.Object, phase, "status", "phase"))
	require.NoError(t, oc.Client.Update(context.Background(), cr))
}
