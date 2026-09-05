package definition

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// FINDINGS #6: bricht das Anwenden zwischen zwei Dokumenten ab, blieb das
// erste stehen - ohne dass ein Datensatz darauf verwies. Der Operator lief
// damit fuer eine Instanz, die es fuer den Broker nie gab: kein Deprovision
// raeumte sie je ab, weil keines sie finden konnte.

var errApplyBoom = errors.New("der Operator-Webhook lehnt ab")

// newBrittleEngine baut dieselbe Zwei-Dokument-Engine wie newMultiDocEngine,
// aber mit einem Client, der bei bestimmten Objektnamen scheitert.
func newBrittleEngine(t *testing.T, failCreate, failDelete string) (*Engine, *OperatorClient, *multiDocRegistry) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	ctrl := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if failCreate != "" && obj.GetName() == failCreate {
				return errApplyBoom
			}
			return c.Create(ctx, obj, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if failDelete != "" && obj.GetName() == failDelete {
				return errApplyBoom
			}
			return c.Delete(ctx, obj, opts...)
		},
	}).Build()

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

func configMapExists(t *testing.T, oc *OperatorClient, namespace, name string) bool {
	t.Helper()
	cm := &corev1.ConfigMap{}
	err := oc.Client.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, cm)
	if apierrors.IsNotFound(err) {
		return false
	}
	require.NoError(t, err)
	return true
}

func TestProvision_AbbruchImZweitenDokumentRaeumtDasErsteAb(t *testing.T) {
	e, oc, reg := newBrittleEngine(t, "osb-inst-rb-replica", "")
	ctx := context.Background()
	sd := testDefinition(t)

	err := e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-rb", "default", mdPlanID, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, errApplyBoom, "der Abbruchgrund muss durchgereicht werden")

	assert.False(t, configMapExists(t, oc, "default", "osb-inst-rb-primary"),
		"das bereits angelegte Objekt darf nicht zurueckbleiben")
	_, gerr := reg.GetInstance(ctx, "inst-rb")
	assert.Error(t, gerr, "ein gescheitertes Provision darf keinen Datensatz hinterlassen")
}

// Objekte ohne Datensatz sind derselbe Fall: das Deprovision antwortet dann
// 410 und findet nie etwas zum Loeschen.
func TestProvision_FehlgeschlagenerDatensatzRaeumtDieObjekteAb(t *testing.T) {
	e, oc, reg := newBrittleEngine(t, "", "")
	reg.putErr = errors.New("Zustandsspeicher nicht erreichbar")
	ctx := context.Background()
	sd := testDefinition(t)

	err := e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-nr", "default", mdPlanID, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "record instance")
	for _, n := range []string{"osb-inst-nr-primary", "osb-inst-nr-replica"} {
		assert.False(t, configMapExists(t, oc, "default", n),
			"ohne Datensatz duerfen keine Objekte stehen bleiben: %s", n)
	}
}

// Der Abbruchgrund gewinnt. Schlaegt auch das Aufraeumen fehl, wird das
// angehaengt - wer die Meldung liest, soll zuerst erfahren, warum das
// Provision scheiterte, und danach, was liegen blieb.
func TestProvision_ScheiterndesAufraeumenVerdecktDenGrundNicht(t *testing.T) {
	e, oc, _ := newBrittleEngine(t, "osb-inst-rb2-replica", "osb-inst-rb2-primary")
	ctx := context.Background()
	sd := testDefinition(t)

	err := e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-rb2", "default", mdPlanID, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, errApplyBoom)
	assert.Contains(t, err.Error(), "Aufraeumen",
		"das gescheiterte Aufraeumen gehoert in die Meldung")
	assert.True(t, configMapExists(t, oc, "default", "osb-inst-rb2-primary"),
		"dieser Test lebt davon, dass das Objekt wirklich stehen bleibt")
}

// Scheitert schon das erste Dokument, gibt es nichts aufzuraeumen - und die
// Meldung darf davon nicht sprechen.
func TestProvision_AbbruchImErstenDokumentBrauchtKeinAufraeumen(t *testing.T) {
	e, oc, _ := newBrittleEngine(t, "osb-inst-rb3-primary", "")
	ctx := context.Background()
	sd := testDefinition(t)

	err := e.ProvisionInstance(ctx, sd.Spec.Offering.ID, "inst-rb3", "default", mdPlanID, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, errApplyBoom)
	assert.NotContains(t, err.Error(), "Aufraeumen")
	assert.False(t, configMapExists(t, oc, "default", "osb-inst-rb3-replica"))
}
