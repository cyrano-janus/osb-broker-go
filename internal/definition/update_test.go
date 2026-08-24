package definition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEngine_UpdateInstance_ChangesCRSpec(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	ctx := context.Background()

	// Provision mit small (1 Instanz)
	require.NoError(t, e.ProvisionInstance(ctx,
		"f48a9e21-cnpg-0000-0000-000000000001", "inst-upd", "default",
		"plan-small-0000-0000-000000000001", nil))

	cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "inst-upd")
	require.NoError(t, err)
	inst, _, err := unstructured.NestedInt64(cr.Object, "spec", "instances")
	require.NoError(t, err)
	assert.Equal(t, int64(1), inst)

	// Operator setzt Ready (realistisch vor dem Update)
	cr, err = oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "inst-upd")
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedSlice(cr.Object,
		[]interface{}{map[string]interface{}{"type": "Ready", "status": "True"}},
		"status", "conditions"))
	require.NoError(t, oc.Client.Update(ctx, cr))

	// UPDATE auf large (3 Instanzen)
	done, err := e.UpdateInstance(ctx,
		"f48a9e21-cnpg-0000-0000-000000000001", "inst-upd", "default",
		"plan-large-0000-0000-000000000002")
	require.NoError(t, err)

	cr, err = oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "inst-upd")
	require.NoError(t, err)
	inst, _, err = unstructured.NestedInt64(cr.Object, "spec", "instances")
	require.NoError(t, err)
	assert.Equal(t, int64(3), inst, "CR spec must reflect new plan")
	assert.True(t, done, "update applies synchronously when template renders")
}

func TestEngine_UpdateInstance_SamePlanIsNoOp(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx,
		"f48a9e21-cnpg-0000-0000-000000000001", "inst-same", "default",
		"plan-large-0000-0000-000000000002", nil))

	rvOf := func() string {
		cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "inst-same")
		require.NoError(t, err)
		return cr.GetResourceVersion()
	}
	before := rvOf()

	done, err := e.UpdateInstance(ctx,
		"f48a9e21-cnpg-0000-0000-000000000001", "inst-same", "default",
		"plan-large-0000-0000-000000000002")
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, before, rvOf(), "same plan must not touch the CR")
}

func TestEngine_UpdateInstance_UnknownPlanFails(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx,
		"f48a9e21-cnpg-0000-0000-000000000001", "inst-unk", "default",
		"plan-small-0000-0000-000000000001", nil))

	_, err := e.UpdateInstance(ctx,
		"f48a9e21-cnpg-0000-0000-000000000001", "inst-unk", "default",
		"plan-does-not-exist")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestValidatePlanParams_RejectsUnknownAndMissing(t *testing.T) {
	sd := testDefinition(t)
	plan, err := sd.PlanByID("plan-small-0000-0000-000000000001")
	require.NoError(t, err)

	// Unbekannter Parameter
	err = ValidatePlanParams(plan, map[string]interface{}{"bogus": 1})
	assert.ErrorContains(t, err, "bogus")

	// Erlaubter Parameter geht durch
	assert.NoError(t, ValidatePlanParams(plan, map[string]interface{}{"replicas": 1}))

	// nil Parameters ist erlaubt
	assert.NoError(t, ValidatePlanParams(plan, nil))
}
