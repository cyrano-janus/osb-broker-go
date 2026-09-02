package handlers

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
