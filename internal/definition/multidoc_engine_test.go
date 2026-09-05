package definition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// multiDocRegistry is an in-memory InstanceRegistry for engine tests.
type multiDocRegistry struct {
	records map[string]*InstanceRecord
	// putErr laesst PutInstance scheitern - fuer den Fall, dass Objekte
	// angelegt sind, der Datensatz aber nicht geschrieben werden kann.
	putErr error
}

func newMultiDocRegistry() *multiDocRegistry {
	return &multiDocRegistry{records: map[string]*InstanceRecord{}}
}

func (r *multiDocRegistry) PutInstance(_ context.Context, rec *InstanceRecord) error {
	if r.putErr != nil {
		return r.putErr
	}
	r.records[rec.ID] = rec
	return nil
}

func (r *multiDocRegistry) DeleteInstance(_ context.Context, instanceID string) error {
	delete(r.records, instanceID)
	return nil
}

func (r *multiDocRegistry) GetInstance(_ context.Context, instanceID string) (*InstanceRecord, error) {
	rec, ok := r.records[instanceID]
	if !ok {
		return nil, ErrNotFound
	}
	return rec, nil
}

// newMultiDocEngine builds an engine over a fake client with a ConfigMap-
// based composite template (2 docs: primary + replica service).
func newMultiDocEngine(t *testing.T) (*Engine, *OperatorClient, *multiDocRegistry) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ctrl := fake.NewClientBuilder().WithScheme(scheme).Build()
	oc := &OperatorClient{Client: ctrl, Scheme: scheme}

	sd := testDefinition(t)
	sd.Spec.Provision.Template = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .safeName }}-primary
data:
  role: primary
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .safeName }}-replica
data:
  source: {{ .safeName }}-primary
`
	reg := newMultiDocRegistry()
	e := NewEngine(oc, sd)
	e.SetInstanceRegistry(reg)
	return e, oc, reg
}

const mdPlanID = "plan-large-0000-0000-000000000002"

func TestEngine_MultiDoc_ProvisionAppliesAllDocs(t *testing.T) {
	e, oc, _ := newMultiDocEngine(t)
	ctx := context.Background()

	sd := testDefinition(t)
	require.NoError(t, e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-multi", "default", mdPlanID, nil))

	for _, name := range []string{"osb-inst-multi-primary", "osb-inst-multi-replica"} {
		cm := &corev1.ConfigMap{}
		err := oc.Client.Get(ctx, clientKey("default", name), cm)
		require.NoError(t, err, "ConfigMap %q muss existieren", name)
	}
}

func TestEngine_MultiDoc_RecordCarriesAppliedObjects(t *testing.T) {
	e, _, reg := newMultiDocEngine(t)
	ctx := context.Background()

	sd := testDefinition(t)
	require.NoError(t, e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-rec", "default", mdPlanID, nil))

	rec, err := reg.GetInstance(ctx, "inst-rec")
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"osb-inst-rec-primary", "osb-inst-rec-replica"},
		rec.AppliedObjects)
}

func TestEngine_MultiDoc_DeprovisionRemovesAllObjects(t *testing.T) {
	e, oc, _ := newMultiDocEngine(t)
	ctx := context.Background()

	sd := testDefinition(t)
	require.NoError(t, e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-gone", "default", mdPlanID, nil))
	require.NoError(t, e.DeprovisionInstance(ctx, sd, "default", "inst-gone"))

	for _, name := range []string{"osb-inst-gone-primary", "osb-inst-gone-replica"} {
		cm := &corev1.ConfigMap{}
		err := oc.Client.Get(ctx, clientKey("default", name), cm)
		assert.True(t, apierrors.IsNotFound(err), "%q sollte weg sein (err=%v)", name, err)
	}
}

func TestEngine_SingleDoc_DeprovisionLegacyPathStillWorks(t *testing.T) {
	// Alte Instanzen ohne AppliedObjects-Eintrag (Stand vor 4.6) werden über
	// den Legacy-Pfad (safeName CR delete) entfernt.
	e, oc, _ := newMultiDocEngine(t)
	ctx := context.Background()

	sd := testDefinition(t)
	sd.Spec.Provision.Template = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .safeName }}
`
	require.NoError(t, e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-legacy", "default", mdPlanID, nil))
	require.NoError(t, e.DeprovisionInstance(ctx, sd, "default", "inst-legacy"))

	cm := &corev1.ConfigMap{}
	err := oc.Client.Get(ctx, clientKey("default", "osb-inst-legacy"), cm)
	assert.True(t, apierrors.IsNotFound(err), "Legacy-CR sollte weg sein (err=%v)", err)
}

// clientKey builds an ObjectKey for lookups in tests.
func clientKey(ns, name string) client.ObjectKey {
	return client.ObjectKey{Namespace: ns, Name: name}
}
