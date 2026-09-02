package broker

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FINDINGS #3: Der Broker las Space und Org ausschliesslich aus dem
// verschachtelten context-Objekt, Korifi sendet sie ausschliesslich als
// Top-Level-Felder. Beides ist OSB-konform - die Spezifikation kennt beide
// Varianten, Top-Level gilt als veraltet, wird von Cloud Foundry aber weiter
// gesendet. Die zwei Implementierungen hatten sich fuer verschiedene Haelften
// entschieden, und deshalb kam die Space-GUID nie an.

func TestProvisionRequest_TopLevelFelderWerdenGelesen(t *testing.T) {
	// Genau das schickt Korifi: kein context-Objekt, nur Top-Level.
	var req ProvisionRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"service_id": "svc-1",
		"plan_id": "plan-1",
		"organization_guid": "org-abc",
		"space_guid": "space-xyz"
	}`), &req))

	ctx := req.ResolvedContext()
	assert.Equal(t, "space-xyz", ctx.SpaceGUID)
	assert.Equal(t, "org-abc", ctx.OrganizationGUID)
}

func TestProvisionRequest_ContextObjektWirdWeiterGelesen(t *testing.T) {
	var req ProvisionRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"service_id": "svc-1",
		"context": {"platform": "cloudfoundry", "space_guid": "space-aus-context",
		            "organization_guid": "org-aus-context"}
	}`), &req))

	ctx := req.ResolvedContext()
	assert.Equal(t, "space-aus-context", ctx.SpaceGUID)
	assert.Equal(t, "org-aus-context", ctx.OrganizationGUID)
	assert.Equal(t, "cloudfoundry", ctx.Platform)
}

func TestProvisionRequest_ContextGewinntGegenTopLevel(t *testing.T) {
	// Die Spezifikation fuehrt die Top-Level-Felder als veraltet; wo beides
	// kommt, ist context die Wahrheit.
	var req ProvisionRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"organization_guid": "org-alt", "space_guid": "space-alt",
		"context": {"space_guid": "space-neu", "organization_guid": "org-neu"}
	}`), &req))

	ctx := req.ResolvedContext()
	assert.Equal(t, "space-neu", ctx.SpaceGUID)
	assert.Equal(t, "org-neu", ctx.OrganizationGUID)
}

func TestProvisionRequest_TeilweiseErgaenzung(t *testing.T) {
	// context da, aber ohne space_guid: dann traegt Top-Level bei, statt dass
	// die Information verloren geht.
	var req ProvisionRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"space_guid": "space-toplevel",
		"context": {"platform": "cloudfoundry", "organization_guid": "org-context"}
	}`), &req))

	ctx := req.ResolvedContext()
	assert.Equal(t, "space-toplevel", ctx.SpaceGUID)
	assert.Equal(t, "org-context", ctx.OrganizationGUID)
	assert.Equal(t, "cloudfoundry", ctx.Platform)
}

func TestProvisionRequest_OhneBeidesBleibtLeer(t *testing.T) {
	var req ProvisionRequest
	require.NoError(t, json.Unmarshal([]byte(`{"service_id":"svc-1"}`), &req))
	assert.Empty(t, req.ResolvedContext().SpaceGUID)
}
