package handlers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// The specs exist twice: the source of truth under docs/ and schemas/, and
// the copy under internal/handlers/docs/ that go:embed compiles into the
// binary. Nothing but discipline kept them in sync, and Phase 4.5 edits
// both. These guards turn a silent drift into a failing test.

func TestDocsSync_OpenAPISpecMatchesEmbeddedCopy(t *testing.T) {
	onDisk, err := os.ReadFile("../../docs/openapi.yaml")
	require.NoError(t, err)

	assert.Equal(t, string(onDisk), string(openAPISpec),
		"docs/openapi.yaml and internal/handlers/docs/openapi.yaml have drifted apart - copy the file, do not edit one of them")
}

func TestDocsSync_ServiceDefinitionSchemaMatchesEmbeddedCopy(t *testing.T) {
	onDisk, err := os.ReadFile("../../schemas/service-definition.schema.json")
	require.NoError(t, err)

	assert.Equal(t, string(onDisk), string(serviceDefSchema),
		"schemas/service-definition.schema.json and its embedded copy have drifted apart")
}

// Die ausgelieferte OpenAPI-Beschreibung muss sich lesen lassen.
//
// Sie wird unter `/openapi.yaml` angeboten, damit ein Konsument daraus einen
// Client erzeugen kann. Bislang wurde nur geprueft, dass die eingebettete
// Kopie zeichengleich mit der Datei ist - zwei identische Kopien eines
// kaputten Dokuments bestehen diese Pruefung.
//
// Gefunden hat es genau ein Zeichen: in einem einfach quotierten YAML-Scalar
// beendet `broker's` die Zeichenkette, und alles danach ist Unsinn. Ein
// Apostroph muss dort verdoppelt werden.
func TestDocsSync_OpenAPISpecIstLesbaresYAML(t *testing.T) {
	for _, p := range []string{
		filepath.Join("..", "..", "docs", "openapi.yaml"),
		filepath.Join("docs", "openapi.yaml"),
	} {
		raw, err := os.ReadFile(p)
		require.NoError(t, err, "%s fehlt", p)

		var doc map[string]interface{}
		require.NoError(t, yaml.Unmarshal(raw, &doc), "%s laesst sich nicht lesen", p)

		// Nicht nur "irgendwas geparst": ein abgebrochener Scalar kann ein
		// Dokument liefern, dem der halbe Inhalt fehlt.
		assert.Equal(t, "3.0.3", doc["openapi"], "%s", p)
		paths, ok := doc["paths"].(map[string]interface{})
		require.True(t, ok, "%s hat keinen paths-Block", p)
		assert.Contains(t, paths, "/v2/catalog", "%s", p)
	}
}
