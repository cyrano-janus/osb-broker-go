package broker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	osbv1 "github.com/example/osb-broker/internal/apis/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const crdTestNamespace = "osb-state-test"

func newCRDTestClient(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, osbv1.AddToScheme(s))
	return fake.NewClientBuilder().WithScheme(s).Build()
}

func newCRDTestStore(t *testing.T) StateStore {
	t.Helper()
	return NewCRDStateStore(newCRDTestClient(t), crdTestNamespace)
}

// Derselbe Vertrag wie fuer den In-Memory-Store, unveraendert.
func TestStateStoreContract_CRD(t *testing.T) {
	runStateStoreContract(t, newCRDTestStore)
}

func TestCRDStore_ObjektnameIstDieIDWennSieGueltigIst(t *testing.T) {
	// Der haeufige Fall: Cloud Foundry schickt UUIDs, die bereits gueltige
	// DNS-1123-Namen sind. Dann soll der Objektname die ID sein, damit
	// `kubectl get osbserviceinstances` ohne Uebersetzungstabelle lesbar ist.
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	id := "930fca69-63a2-45db-abee-46770af47008"
	require.NoError(t, s.PutInstance(context.Background(), newTestInstance(id)))

	var cr osbv1.OSBServiceInstance
	err := c.Get(context.Background(), types.NamespacedName{Namespace: crdTestNamespace, Name: id}, &cr)
	require.NoError(t, err, "eine UUID muss unveraendert als Objektname dienen")
	assert.Equal(t, id, cr.Spec.ID)
}

func TestCRDStore_UngueltigeIDBekommtAbgeleitetenNamen(t *testing.T) {
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	id := "Instance_With_UPPER.and.dots"
	require.NoError(t, s.PutInstance(context.Background(), newTestInstance(id)))

	var list osbv1.OSBServiceInstanceList
	require.NoError(t, c.List(context.Background(), &list))
	require.Len(t, list.Items, 1)

	name := list.Items[0].Name
	assert.NotEqual(t, id, name)
	assert.LessOrEqual(t, len(name), 63, "DNS-1123-Label")
	assert.Equal(t, strings.ToLower(name), name)
	assert.Equal(t, id, list.Items[0].Spec.ID, "die ID bleibt im Spec erhalten")
}

func TestCRDStore_CredentialsLiegenImSecretNichtImCR(t *testing.T) {
	// FINDINGS #19: in der ConfigMap standen die Credentials im Klartext
	// neben allem anderen. Ein CR haette das nur wiederholt.
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	ctx := context.Background()

	b := newTestBinding("bind-secret", "inst-1")
	b.Credentials = map[string]interface{}{"password": "geheim", "uri": "postgres://x"}
	require.NoError(t, s.PutBinding(ctx, b))

	var cr osbv1.OSBServiceBinding
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: crdTestNamespace, Name: "bind-secret"}, &cr))
	require.NotEmpty(t, cr.Spec.CredentialsSecret)

	// Nirgends im CR darf das Passwort auftauchen.
	crJSON, err := json.Marshal(cr)
	require.NoError(t, err)
	assert.NotContains(t, string(crJSON), "geheim")

	var sec corev1.Secret
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: crdTestNamespace, Name: cr.Spec.CredentialsSecret}, &sec))
	assert.Contains(t, string(sec.Data["credentials.json"]), "geheim")

	// Und der Weg zurueck muss die Credentials wieder liefern.
	got, err := s.GetBinding(ctx, "bind-secret")
	require.NoError(t, err)
	assert.Equal(t, "geheim", got.Credentials["password"])
}

func TestCRDStore_BindingLoeschenRaeumtDasSecretAb(t *testing.T) {
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	ctx := context.Background()

	b := newTestBinding("bind-gc", "inst-1")
	require.NoError(t, s.PutBinding(ctx, b))

	var cr osbv1.OSBServiceBinding
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: crdTestNamespace, Name: "bind-gc"}, &cr))
	secretName := cr.Spec.CredentialsSecret
	require.NotEmpty(t, secretName)

	require.NoError(t, s.DeleteBinding(ctx, "bind-gc"))

	// Explizit loeschen statt nur auf OwnerReferences zu vertrauen: die
	// Garbage Collection laeuft asynchron, und ein zurueckgebliebenes Secret
	// mit echten DB-Credentials ist kein Schoenheitsfehler.
	var sec corev1.Secret
	err := c.Get(ctx, types.NamespacedName{Namespace: crdTestNamespace, Name: secretName}, &sec)
	assert.Error(t, err, "das Credentials-Secret muss mit dem Binding verschwinden")
}

func TestCRDStore_SecretHaengtAmBindingAlsOwner(t *testing.T) {
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	ctx := context.Background()
	require.NoError(t, s.PutBinding(ctx, newTestBinding("bind-owner", "inst-1")))

	var sec corev1.Secret
	require.NoError(t, c.Get(ctx, types.NamespacedName{
		Namespace: crdTestNamespace, Name: "bind-owner-credentials"}, &sec))
	require.Len(t, sec.OwnerReferences, 1)
	assert.Equal(t, "OSBServiceBinding", sec.OwnerReferences[0].Kind)
	assert.Equal(t, "bind-owner", sec.OwnerReferences[0].Name)
}

func TestCRDStore_BindingsTragenDasInstanzLabel(t *testing.T) {
	// ListBindingsByInstance darf nicht alle Bindings laden und im Speicher
	// filtern - bei ueber 1000 Instanzen waere das genau die Sorte Aufwand,
	// wegen der wir den ConfigMap-Store verlassen.
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	ctx := context.Background()
	require.NoError(t, s.PutBinding(ctx, newTestBinding("b1", "inst-a")))

	var list osbv1.OSBServiceBindingList
	require.NoError(t, c.List(ctx, &list, client.MatchingLabels{osbv1.LabelInstance: "inst-a"}))
	require.Len(t, list.Items, 1)
	assert.Equal(t, "b1", list.Items[0].Spec.ID)
}

func TestCRDStore_ZahlenInParameternKommenAlsFloat64Zurueck(t *testing.T) {
	// Dokumentiert, statt zu ueberraschen: der Store serialisiert nach JSON,
	// und JSON kennt nur eine Zahlenform. In der Praxis kommen Parameter
	// ohnehin aus einem JSON-Request, sind also schon float64 - der
	// In-Memory-Store ist hier der Sonderfall, nicht dieser.
	s := newCRDTestStore(t)
	ctx := context.Background()

	in := newTestInstance("inst-zahl")
	in.Parameters = map[string]interface{}{"instances": 3}
	require.NoError(t, s.PutInstance(ctx, in))

	got, err := s.GetInstance(ctx, "inst-zahl")
	require.NoError(t, err)
	assert.Equal(t, float64(3), got.Parameters["instances"])
}

func TestCRDStore_LeereCredentialsErzeugenKeinSecret(t *testing.T) {
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	ctx := context.Background()

	b := newTestBinding("bind-leer", "inst-1")
	b.Credentials = nil
	require.NoError(t, s.PutBinding(ctx, b))

	var secrets corev1.SecretList
	require.NoError(t, c.List(ctx, &secrets, client.InNamespace(crdTestNamespace)))
	assert.Empty(t, secrets.Items, "ohne Credentials braucht es kein Secret")

	var cr osbv1.OSBServiceBinding
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: crdTestNamespace, Name: "bind-leer"}, &cr))
	assert.Empty(t, cr.Spec.CredentialsSecret)
}

func TestCRDStore_PutIstIdempotentUndAktualisiertBestehendeObjekte(t *testing.T) {
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	ctx := context.Background()

	require.NoError(t, s.PutInstance(ctx, newTestInstance("inst-idem")))
	require.NoError(t, s.PutInstance(ctx, newTestInstance("inst-idem")))

	var list osbv1.OSBServiceInstanceList
	require.NoError(t, c.List(ctx, &list))
	assert.Len(t, list.Items, 1, "ein zweiter Put darf kein zweites Objekt anlegen")
}

func TestCRDStore_ObjekteTragenVerwaltungsLabels(t *testing.T) {
	c := newCRDTestClient(t)
	s := NewCRDStateStore(c, crdTestNamespace)
	require.NoError(t, s.PutInstance(context.Background(), newTestInstance("inst-label")))

	var cr osbv1.OSBServiceInstance
	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Namespace: crdTestNamespace, Name: "inst-label"}, &cr))
	assert.Equal(t, "osb-broker-go", cr.Labels["app.kubernetes.io/managed-by"])
}
