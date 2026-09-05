package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Was der Broker an Parametern durchsetzt, soll im Katalog stehen: eine
// Plattform kann dann ablehnen, bevor sie ihn ueberhaupt fragt, und eine
// Oberflaeche daraus ein Formular bauen. OSB 2.17 sieht dafuer den
// `schemas`-Block je Plan vor.

func catalogPlans(t *testing.T) []map[string]interface{} {
	t.Helper()
	router, _ := newDefinitionRouter(t)
	w := perform(router, "/v2/catalog", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Services []struct {
			Plans []map[string]interface{} `json:"plans"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Services)
	return body.Services[0].Plans
}

func TestKatalog_PlanTraegtSeinParameterschema(t *testing.T) {
	plans := catalogPlans(t)
	require.NotEmpty(t, plans)

	schemas, ok := plans[0]["schemas"].(map[string]interface{})
	require.True(t, ok, "jeder Plan traegt einen schemas-Block: %v", plans[0])

	inst := schemas["service_instance"].(map[string]interface{})
	for _, weg := range []string{"create", "update"} {
		params := inst[weg].(map[string]interface{})["parameters"].(map[string]interface{})
		assert.Equal(t, "object", params["type"], "%s", weg)
		assert.Equal(t, false, params["additionalProperties"],
			"%s: was der Broker mit 400 ablehnt, darf das Schema nicht erlauben", weg)
	}
}

func TestKatalog_DieAllowlistStehtImSchema(t *testing.T) {
	plans := catalogPlans(t)

	var free map[string]interface{}
	for _, p := range plans {
		if p["name"] == "free" {
			free = p
		}
	}
	require.NotNil(t, free, "der Testplan free muss im Katalog stehen")

	params := free["schemas"].(map[string]interface{})["service_instance"].(map[string]interface{})["create"].(map[string]interface{})["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})

	assert.Contains(t, props, "size", "der Testplan erlaubt size")
}
