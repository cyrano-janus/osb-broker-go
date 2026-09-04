package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Zwei Luecken auf dem Definitions-Pfad, die lange unentdeckt blieben, weil die
// Konformitaetssuite den Fallback-Pfad geprueft hat: ein wiederholtes Provision
// antwortete 201 statt 200, und ein PATCH auf eine unbekannte Instanz
// antwortete 200 und legte sie an - im Rueckfall-Namespace, an der
// Mandantentrennung vorbei.

func TestProvision_WiederholungIstIdempotent(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	const instanceID = "idem-inst-1"
	body := map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
		"organization_guid": "org-1", "space_guid": spaceNS,
	}

	first := provisionJSON(router, "/v2/service_instances/"+instanceID, body)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())

	second := provisionJSON(router, "/v2/service_instances/"+instanceID, body)

	assert.Equal(t, http.StatusOK, second.Code,
		"OSB 2.17: dieselbe Instanz mit denselben Parametern ist 200, nicht 201 - die Plattform wiederholt Requests")
	assert.Contains(t, second.Body.String(), "dashboard_url")
}

func TestProvision_AndereParameterSindEinKonflikt(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	const instanceID = "idem-inst-2"

	first := provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
		"organization_guid": "org-1", "space_guid": spaceNS,
	})
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())

	second := provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-paid",
		"organization_guid": "org-1", "space_guid": spaceNS,
	})

	assert.Equal(t, http.StatusConflict, second.Code,
		"eine bekannte Instanz mit anderem Plan ist 409, kein stilles Ueberschreiben")
}

func TestUpdate_UnbekannteInstanzIstNichtGefunden(t *testing.T) {
	router, oc := newDefinitionRouter(t)
	const instanceID = "gibt-es-nicht"

	w := patchJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
	})

	assert.Equal(t, http.StatusNotFound, w.Code,
		"ein Update auf eine unbekannte Instanz ist 404")

	// Der eigentliche Schaden war nicht der Statuscode, sondern dass die
	// Engine dabei ein CR angelegt hat.
	_, err := crIn(t, oc, defaultNamespace, instanceID)
	assert.Error(t, err, "ein Update darf nichts provisionieren")
}
