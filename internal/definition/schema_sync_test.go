package definition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Das mitgelieferte JSON-Schema ist das, womit Anwender ihre
// ServiceDefinitions offline pruefen, bevor sie sie ausrollen. Faellt ein Feld
// darin unter den Tisch, meldet der Validator einen gueltigen Wert als
// unbekannt - oder, schlimmer, laesst bei additionalProperties einen Tippfehler
// durchgehen. Beides faellt sonst erst im Cluster auf.

func loadDefinitionSchema(t *testing.T) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "service-definition.schema.json"))
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func schemaProperties(t *testing.T, schema map[string]interface{}, definition string) map[string]interface{} {
	t.Helper()
	defs, ok := schema["definitions"].(map[string]interface{})
	require.True(t, ok, "definitions fehlt im Schema")
	d, ok := defs[definition].(map[string]interface{})
	require.True(t, ok, "definitions.%s fehlt", definition)
	props, ok := d["properties"].(map[string]interface{})
	require.True(t, ok, "definitions.%s.properties fehlt", definition)
	return props
}

func structJSONFields(t *testing.T, v interface{}) []string {
	t.Helper()
	typ := reflect.TypeOf(v)
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if name := strings.Split(tag, ",")[0]; name != "" {
			out = append(out, name)
		}
	}
	return out
}

func TestSchema_BindDecktDenGoTyp(t *testing.T) {
	props := schemaProperties(t, loadDefinitionSchema(t), "bind")
	for _, field := range structJSONFields(t, Bind{}) {
		assert.Contains(t, props, field, "spec.bind.%s fehlt im JSON-Schema", field)
	}
}

func TestSchema_MappingDecktDenGoTyp(t *testing.T) {
	props := schemaProperties(t, loadDefinitionSchema(t), "credentialMapping")
	for _, field := range structJSONFields(t, CredentialMapping{}) {
		assert.Contains(t, props, field, "mapping[].%s fehlt im JSON-Schema", field)
	}
}

// Plan steckt nicht unter definitions/, sondern inline in
// offering.properties.plans.items. Ohne eigenen Waechter rutscht dort jedes
// neue Feld am Schema vorbei - `parameterLimits` hat genau das getan.
func TestSchema_PlanDecktDenGoTyp(t *testing.T) {
	schema := loadDefinitionSchema(t)
	defs := schema["definitions"].(map[string]interface{})
	offering := defs["offering"].(map[string]interface{})
	plans := offering["properties"].(map[string]interface{})["plans"].(map[string]interface{})
	props := plans["items"].(map[string]interface{})["properties"].(map[string]interface{})

	for _, field := range structJSONFields(t, Plan{}) {
		assert.Contains(t, props, field, "plans[].%s fehlt im JSON-Schema", field)
	}
}

// Und andersherum: ein Schluessel im Schema, den der Go-Typ nicht kennt, ist
// eine Zusage an Anwender, die der Broker nicht einloest.
func TestSchema_PlanVerspricht_NichtsUnbekanntes(t *testing.T) {
	schema := loadDefinitionSchema(t)
	defs := schema["definitions"].(map[string]interface{})
	offering := defs["offering"].(map[string]interface{})
	plans := offering["properties"].(map[string]interface{})["plans"].(map[string]interface{})
	props := plans["items"].(map[string]interface{})["properties"].(map[string]interface{})

	known := map[string]bool{}
	for _, field := range structJSONFields(t, Plan{}) {
		known[field] = true
	}
	for key := range props {
		assert.True(t, known[key], "das Schema nennt plans[].%s, der Go-Typ kennt es nicht", key)
	}
}

func TestSchema_CredentialsFromSecretIstNichtMehrPflicht(t *testing.T) {
	// Mit provisionedService gibt es einen zweiten, gleichwertigen Weg. Ein
	// Schema, das credentialsFromSecret weiter erzwingt, wuerde gueltige
	// Definitionen ablehnen.
	defs := loadDefinitionSchema(t)["definitions"].(map[string]interface{})
	bind := defs["bind"].(map[string]interface{})
	required, _ := bind["required"].([]interface{})
	for _, r := range required {
		assert.NotEqual(t, "credentialsFromSecret", r,
			"credentialsFromSecret darf nicht mehr unbedingt gefordert werden")
	}
}
