package definition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8scorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEngine_CatalogFromDefinitions(t *testing.T) {
	sd := testDefinition(t)
	e := NewEngine(nil, sd)

	catalog := e.Catalog()
	require.Len(t, catalog, 1)
	svc := catalog[0]
	assert.Equal(t, "f48a9e21-cnpg-0000-0000-000000000001", svc.ID)
	assert.Equal(t, "cnpg-postgresql", svc.Name)
	assert.True(t, svc.Bindable)
	require.Len(t, svc.Plans, 2)
	assert.Equal(t, "plan-large-0000-0000-000000000002", svc.Plans[1].ID)
}

func TestEngine_DefinitionByServiceID(t *testing.T) {
	sd := testDefinition(t)
	e := NewEngine(nil, sd)

	got, err := e.DefinitionByServiceID("f48a9e21-cnpg-0000-0000-000000000001")
	require.NoError(t, err)
	assert.Equal(t, "cnpg-postgresql", got.Metadata.Name)

	_, err = e.DefinitionByServiceID("unknown-id")
	assert.Error(t, err)
}

func contextBackground() context.Context { return context.Background() }

// k8sSecret builds a corev1.Secret for bind tests.
func k8sSecret(namespace, name string, data map[string][]byte) *k8scorev1.Secret {
	s := &k8scorev1.Secret{}
	s.Name = name
	s.Namespace = namespace
	s.Data = data
	return s
}

func TestEngine_ProvisionInstance_RendersAndAppliesCR(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	ctx := contextBackground()

	err := e.ProvisionInstance(ctx, "f48a9e21-cnpg-0000-0000-000000000001", "inst-e2e", "default", "plan-large-0000-0000-000000000002", nil)
	require.NoError(t, err)

	// CR existiert mit gerenderten Werten
	cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-e2e")
	require.NoError(t, err)
	spec, found, err := unstructured.NestedMap(cr.Object, "spec")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(3), spec["instances"])
}

func TestEngine_DeprovisionInstance(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	ctx := contextBackground()

	require.NoError(t, e.ProvisionInstance(ctx, "f48a9e21-cnpg-0000-0000-000000000001", "inst-gone", "default", "plan-small-0000-0000-000000000001", nil))
	require.NoError(t, e.DeprovisionInstance(ctx, sd, "default", "inst-gone"))

	cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-gone")
	assert.Error(t, err)
	assert.Nil(t, cr)
}

func TestEngine_LastOperation_MapsReadiness(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	ctx := contextBackground()

	require.NoError(t, e.ProvisionInstance(ctx, "f48a9e21-cnpg-0000-0000-000000000001", "inst-ro", "default", "plan-small-0000-0000-000000000001", nil))

	// Noch nicht ready (keine Conditions im Fake-CR)
	state, err := e.LastOperation(ctx, sd, "default", "inst-ro")
	require.NoError(t, err)
	assert.Equal(t, "in progress", state)

	// Ready-Condition setzen
	cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-ro")
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedSlice(cr.Object,
		[]interface{}{map[string]interface{}{"type": "Ready", "status": "True"}},
		"status", "conditions"))
	require.NoError(t, oc.Client.Update(ctx, cr))

	state, err = e.LastOperation(ctx, sd, "default", "inst-ro")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", state)
}

func TestEngine_BindInstance_ReadsSecret(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	ctx := contextBackground()

	// Secret anlegen, wie es der Operator tun würde
	require.NoError(t, oc.Client.Create(ctx, k8sSecret("default", "osb-inst-bind-app", map[string][]byte{
		"user":     []byte("alice"),
		"password": []byte("s3cret"),
	})))

	creds, secretName, err := e.BindCredentials(ctx, sd, "default", "inst-bind")
	require.NoError(t, err)
	assert.Equal(t, "osb-inst-bind-app", secretName)
	assert.Equal(t, "alice", creds["user"])
	assert.Equal(t, "s3cret", creds["password"])
}
