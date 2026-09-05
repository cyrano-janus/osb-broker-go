package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Der Weg, den ein `cf create-service -c '{...}'` nimmt: Query, Whitelist,
// Template, CR - und zurueck ueber GET /v2/service_instances.

func crSize(t *testing.T, u *unstructured.Unstructured) string {
	t.Helper()
	size, found, err := unstructured.NestedString(u.Object, "spec", "size")
	require.NoError(t, err)
	require.True(t, found, "spec.size fehlt im CR")
	return size
}

func TestProvision_BenutzerparameterErreichenDasCR(t *testing.T) {
	router, oc := newDefinitionRouter(t)
	const instanceID = "param-inst-1"

	w := provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
		"organization_guid": "org-1", "space_guid": spaceNS,
		"parameters": map[string]interface{}{"size": "xl"},
	})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	cr, err := crIn(t, oc, spaceNS, instanceID)
	require.NoError(t, err)
	assert.Equal(t, "xl", crSize(t, cr),
		"der Parameter muss den Planwert small ersetzen")
}

func TestProvision_GleicheParameterSind200_AndereSind409(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	const instanceID = "param-inst-2"
	body := func(size string) map[string]interface{} {
		return map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free",
			"organization_guid": "org-1", "space_guid": spaceNS,
			"parameters": map[string]interface{}{"size": size},
		}
	}

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, body("xl")).Code)

	assert.Equal(t, http.StatusOK,
		provisionJSON(router, "/v2/service_instances/"+instanceID, body("xl")).Code,
		"dieselbe Instanz mit denselben Parametern ist eine Wiederholung")

	assert.Equal(t, http.StatusConflict,
		provisionJSON(router, "/v2/service_instances/"+instanceID, body("s")).Code,
		"dieselbe Instanz mit anderen Parametern ist ein Konflikt")
}

// plan_id ist im PATCH optional. Vorher scheiterte ein reines
// Parameter-Update an einem Plan mit dem Namen "" und antwortete 400.
func TestUpdate_OhnePlanIDIstEinParameterUpdate(t *testing.T) {
	router, oc := newDefinitionRouter(t)
	const instanceID = "param-inst-3"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free",
			"organization_guid": "org-1", "space_guid": spaceNS,
		}).Code)

	w := patchJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"parameters": map[string]interface{}{"size": "xl"},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cr, err := crIn(t, oc, spaceNS, instanceID)
	require.NoError(t, err)
	assert.Equal(t, "xl", crSize(t, cr))
}

// GET /v2/service_instances muss den vollstaendigen Parametersatz melden,
// auch wenn der PATCH nur einen Teil davon geschickt hat.
func TestUpdate_GetMeldetDenVerschmolzenenSatz(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	const instanceID = "param-inst-4"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free",
			"organization_guid": "org-1", "space_guid": spaceNS,
			"parameters": map[string]interface{}{"size": "m"},
		}).Code)

	require.Equal(t, http.StatusOK,
		patchJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"parameters": map[string]interface{}{"size": "xl"},
		}).Code)

	w := perform(router, "/v2/service_instances/"+instanceID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		PlanID     string                 `json:"plan_id"`
		Parameters map[string]interface{} `json:"parameters"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "def-plan-free", got.PlanID, "der Plan bleibt ohne plan_id stehen")
	assert.Equal(t, map[string]interface{}{"size": "xl"}, got.Parameters)
}

func TestUpdate_UnerlaubterParameterBleibt400(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	const instanceID = "param-inst-5"

	require.Equal(t, http.StatusAccepted,
		provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free",
			"organization_guid": "org-1", "space_guid": spaceNS,
		}).Code)

	w := patchJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"parameters": map[string]interface{}{"evil_key": "x"},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
