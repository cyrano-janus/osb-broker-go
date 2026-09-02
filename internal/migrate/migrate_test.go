package migrate

import (
	"context"
	"testing"

	osbv1 "github.com/example/osb-broker/internal/apis/v1alpha1"
	"github.com/example/osb-broker/internal/broker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const ns = "osb"

// So sah state.json wirklich aus: die alten Structs Instance und Binding
// hatten KEINE json-Tags, also stehen dort die Go-Feldnamen in PascalCase -
// waehrend das eingebettete Context sehr wohl Tags hatte und deshalb in
// snake_case steht. Wer die Migration gegen die heutigen Typen schreibt,
// liest lauter Nullwerte und merkt es nicht.
const legacyStateJSON = `{
  "instances": {
    "930fca69-63a2-45db-abee-46770af47008": {
      "ID": "930fca69-63a2-45db-abee-46770af47008",
      "ServiceID": "f48a9e21-cnpg-0000-0000-000000000001",
      "PlanID": "plan-small-0000-0000-000000000001",
      "Context": {"platform": "cloudfoundry", "space_guid": "space-1", "organization_guid": "org-1"},
      "Parameters": {"storageSize": "1Gi"},
      "DashboardURL": "https://dashboard.example.com/inst",
      "Ready": true,
      "AppliedObjects": ["osb-930fca69"],
      "AppliedRefs": [
        {"APIVersion": "postgresql.cnpg.io/v1", "Kind": "Cluster", "Namespace": "default", "Name": "osb-930fca69"}
      ]
    }
  },
  "bindings": {
    "b1111111-1111-1111-1111-111111111111": {
      "ID": "b1111111-1111-1111-1111-111111111111",
      "InstanceID": "930fca69-63a2-45db-abee-46770af47008",
      "ServiceID": "f48a9e21-cnpg-0000-0000-000000000001",
      "PlanID": "plan-small-0000-0000-000000000001",
      "AppGUID": "app-1",
      "Context": {"platform": "cloudfoundry", "space_guid": "space-1"},
      "Credentials": {"username": "app", "password": "geheim"},
      "Ready": true
    }
  }
}`

func newClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, osbv1.AddToScheme(s))
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func legacyConfigMap(data string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: DefaultConfigMapName, Namespace: ns},
		Data:       map[string]string{"state.json": data},
	}
}

func TestRun_UebertraegtInstanzenUndBindings(t *testing.T) {
	c := newClient(t, legacyConfigMap(legacyStateJSON))
	ctx := context.Background()

	report, err := Run(ctx, c, ns, DefaultConfigMapName, false)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Instances)
	assert.Equal(t, 1, report.Bindings)

	store := broker.NewCRDStateStore(c, ns)
	inst, err := store.GetInstance(ctx, "930fca69-63a2-45db-abee-46770af47008")
	require.NoError(t, err)
	assert.Equal(t, "f48a9e21-cnpg-0000-0000-000000000001", inst.ServiceID)
	assert.Equal(t, "https://dashboard.example.com/inst", inst.DashboardURL)
	assert.True(t, inst.Ready)
	assert.Equal(t, "1Gi", inst.Parameters["storageSize"])
	// Das eingebettete Context hatte json-Tags, der Rest nicht - genau hier
	// geht eine naiv geschriebene Migration schief.
	assert.Equal(t, "space-1", inst.Context.SpaceGUID)
	assert.Equal(t, "org-1", inst.Context.OrganizationGUID)
	require.Len(t, inst.AppliedRefs, 1)
	assert.Equal(t, "Cluster", inst.AppliedRefs[0].Kind)
	assert.Equal(t, "osb-930fca69", inst.AppliedObjects[0])

	bind, err := store.GetBinding(ctx, "b1111111-1111-1111-1111-111111111111")
	require.NoError(t, err)
	assert.Equal(t, "app-1", bind.AppGUID)
	assert.Equal(t, "geheim", bind.Credentials["password"])
	assert.Equal(t, "space-1", bind.Context.SpaceGUID)
}

func TestRun_CredentialsLandenImSecret(t *testing.T) {
	c := newClient(t, legacyConfigMap(legacyStateJSON))
	_, err := Run(context.Background(), c, ns, DefaultConfigMapName, false)
	require.NoError(t, err)

	var cr osbv1.OSBServiceBinding
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{
		Namespace: ns, Name: "b1111111-1111-1111-1111-111111111111"}, &cr))
	require.NotEmpty(t, cr.Spec.CredentialsSecret)

	var sec corev1.Secret
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{
		Namespace: ns, Name: cr.Spec.CredentialsSecret}, &sec))
	assert.Contains(t, string(sec.Data["credentials.json"]), "geheim")
}

func TestRun_FehlendeConfigMapIstKeinFehler(t *testing.T) {
	// Eine Neuinstallation hat keine alte ConfigMap. Das Werkzeug muss
	// dann ruhig durchlaufen, nicht scheitern - sonst kann es niemand
	// bedenkenlos in ein Deployment-Skript haengen.
	c := newClient(t)
	report, err := Run(context.Background(), c, ns, DefaultConfigMapName, false)
	require.NoError(t, err)
	assert.True(t, report.SourceMissing)
	assert.Zero(t, report.Instances)
}

func TestRun_TrockenlaufSchreibtNichts(t *testing.T) {
	c := newClient(t, legacyConfigMap(legacyStateJSON))
	ctx := context.Background()

	report, err := Run(ctx, c, ns, DefaultConfigMapName, true)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Instances, "der Trockenlauf muss zaehlen, was er tun wuerde")

	var list osbv1.OSBServiceInstanceList
	require.NoError(t, c.List(ctx, &list))
	assert.Empty(t, list.Items, "im Trockenlauf darf nichts entstehen")
}

func TestRun_IstIdempotent(t *testing.T) {
	c := newClient(t, legacyConfigMap(legacyStateJSON))
	ctx := context.Background()

	_, err := Run(ctx, c, ns, DefaultConfigMapName, false)
	require.NoError(t, err)
	_, err = Run(ctx, c, ns, DefaultConfigMapName, false)
	require.NoError(t, err)

	var list osbv1.OSBServiceInstanceList
	require.NoError(t, c.List(ctx, &list))
	assert.Len(t, list.Items, 1, "ein zweiter Lauf darf nichts verdoppeln")
}

func TestRun_LaesstDieAlteConfigMapStehen(t *testing.T) {
	// Absichtlich: solange die Migration nicht geprueft ist, ist die alte
	// ConfigMap die einzige Rueckfallebene. Wer sie loeschen will, tut das
	// bewusst und selbst.
	c := newClient(t, legacyConfigMap(legacyStateJSON))
	_, err := Run(context.Background(), c, ns, DefaultConfigMapName, false)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	assert.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: DefaultConfigMapName}, &cm))
}

func TestRun_KaputtesJSONMeldetEinenFehler(t *testing.T) {
	c := newClient(t, legacyConfigMap("{kein json"))
	_, err := Run(context.Background(), c, ns, DefaultConfigMapName, false)
	require.Error(t, err)
}

func TestRun_LeererStateIstKeinFehler(t *testing.T) {
	c := newClient(t, legacyConfigMap(`{"instances":{},"bindings":{}}`))
	report, err := Run(context.Background(), c, ns, DefaultConfigMapName, false)
	require.NoError(t, err)
	assert.Zero(t, report.Instances)
	assert.Zero(t, report.Bindings)
}
