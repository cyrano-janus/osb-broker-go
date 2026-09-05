package definition

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OSB 2.17 kennt Plan-Schemas: ein Plan darf im Katalog beschreiben, welche
// Parameter er annimmt. Eine Plattform kann damit ablehnen, bevor der Broker
// ueberhaupt gefragt wird, und eine Oberflaeche kann daraus ein Formular
// bauen.
//
// Der Broker leitet sie aus dem ab, was er ohnehin durchsetzt -
// `allowedParameters` und `parameterLimits`. Zwei Quellen fuer dieselbe
// Aussage wuerden auseinanderlaufen; hier ist es eine.

func TestPlanSchema_OhneAllowlistNimmtDerPlanKeineParameter(t *testing.T) {
	p := &Plan{ID: "p", Name: "small"}

	got := p.ParameterSchema()

	require.NotNil(t, got, "auch 'nimmt nichts an' ist eine Aussage und gehoert in den Katalog")
	assert.Equal(t, "object", got["type"])
	assert.Equal(t, false, got["additionalProperties"],
		"was der Broker ablehnt, muss das Schema ebenfalls ablehnen")
	assert.Empty(t, got["properties"])
}

func TestPlanSchema_AllowlistWirdZuProperties(t *testing.T) {
	p := &Plan{ID: "p", Name: "small", AllowedParameters: []string{"storageSize", "instances"}}

	props, ok := p.ParameterSchema()["properties"].(map[string]interface{})
	require.True(t, ok)

	assert.Contains(t, props, "storageSize")
	assert.Contains(t, props, "instances")
	assert.Len(t, props, 2)
}

func TestPlanSchema_AufzaehlungWirdZuEnum(t *testing.T) {
	p := &Plan{
		ID: "p", Name: "small",
		AllowedParameters: []string{"tier"},
		ParameterLimits:   map[string]ParameterLimit{"tier": {OneOf: []string{"bronze", "silber"}}},
	}

	props := p.ParameterSchema()["properties"].(map[string]interface{})
	tier := props["tier"].(map[string]interface{})

	assert.Equal(t, []interface{}{"bronze", "silber"}, tier["enum"])
}

// Eine reine Zahl laesst sich als maximum/minimum ausdruecken; JSON Schema
// versteht das und eine Plattform kann danach pruefen.
func TestPlanSchema_ZahlengrenzeWirdZuMaximum(t *testing.T) {
	p := &Plan{
		ID: "p", Name: "small",
		AllowedParameters: []string{"instances"},
		ParameterLimits:   map[string]ParameterLimit{"instances": {Min: "1", Max: "3"}},
	}

	inst := p.ParameterSchema()["properties"].(map[string]interface{})["instances"].(map[string]interface{})

	assert.Equal(t, float64(3), inst["maximum"])
	assert.Equal(t, float64(1), inst["minimum"])
}

// Eine Mengenangabe wie 10Gi ist in JSON Schema keine Zahl. Sie darf deshalb
// nicht als maximum auftauchen - das waere schlicht falsch -, muss aber
// sichtbar bleiben, sonst sieht die Plattform eine Grenze nicht, die der
// Broker durchsetzt.
func TestPlanSchema_MengenangabeStehtInDerBeschreibung(t *testing.T) {
	p := &Plan{
		ID: "p", Name: "small",
		AllowedParameters: []string{"storageSize"},
		ParameterLimits:   map[string]ParameterLimit{"storageSize": {Max: "10Gi"}},
	}

	size := p.ParameterSchema()["properties"].(map[string]interface{})["storageSize"].(map[string]interface{})

	assert.NotContains(t, size, "maximum", "10Gi ist keine JSON-Schema-Zahl")
	assert.Contains(t, size["description"], "10Gi")
}

// Das Schema muss dasselbe sagen wie die Durchsetzung. Was ValidatePlanParams
// ablehnt, darf das Schema nicht erlauben.
func TestPlanSchema_SagtDasselbeWieDieDurchsetzung(t *testing.T) {
	p := &Plan{
		ID: "p", Name: "small",
		AllowedParameters: []string{"instances"},
		ParameterLimits:   map[string]ParameterLimit{"instances": {Max: "3"}},
	}
	schema := p.ParameterSchema()

	// nicht aufgefuehrter Schluessel: beide lehnen ab
	assert.Error(t, ValidatePlanParams(p, map[string]interface{}{"fremd": 1}))
	assert.Equal(t, false, schema["additionalProperties"])

	// Grenze: beide kennen dieselbe Zahl
	assert.Error(t, ValidatePlanParams(p, map[string]interface{}{"instances": 4}))
	props := schema["properties"].(map[string]interface{})
	assert.Equal(t, float64(3), props["instances"].(map[string]interface{})["maximum"])
}

func TestCatalog_PlanTraegtSeinSchema(t *testing.T) {
	sd := testDefinition(t)
	e := NewEngine(nil, sd)

	entries := e.Catalog()
	require.NotEmpty(t, entries)

	var small *CatalogPlan
	for i := range entries[0].Plans {
		if entries[0].Plans[i].Name == "small" {
			small = &entries[0].Plans[i]
		}
	}
	require.NotNil(t, small, "der Testplan small muss im Katalog stehen")
	require.NotNil(t, small.Schemas, "ein Plan traegt sein Parameterschema in den Katalog")

	create := small.Schemas.ServiceInstance.Create.Parameters
	assert.Equal(t, "object", create["type"])
	props := create["properties"].(map[string]interface{})
	assert.Contains(t, props, "storageSize", "der Testplan erlaubt storageSize")
}
