package definition

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Definitionen werden beim Start gelesen. Eine geänderte Definition fasst
// bestehende Instanzen deshalb nicht an: wer den Plan "small" von 1Gi auf 2Gi
// hebt, hebt ihn für neue Instanzen und lässt die alten stehen. Nach einigen
// Änderungen weiß niemand mehr, welche Instanz welchen Stand hat.
//
// ReconcileInstance ist der Abgleich eines einzelnen Datensatzes gegen die
// Definition, die jetzt gilt. Er rendert aus dem gespeicherten Plan und den
// gespeicherten Parametern neu und schreibt nur bei echter Abweichung.
//
// **Was er ausdrücklich nicht tut, ist genauso wichtig wie was er tut.** Ein
// Abgleich, der aufräumt, was er nicht versteht, löscht bei einem Tippfehler
// in einer YAML-Datei die Datenbank eines Kunden.

func reconcileEngine(t *testing.T) (*Engine, *OperatorClient, *multiDocRegistry, *ServiceDefinition) {
	t.Helper()
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	reg := newMultiDocRegistry()
	e.SetInstanceRegistry(reg)
	return e, oc, reg, sd
}

// crStorage liest die Größe, die im CR wirklich steht.
func crStorage(t *testing.T, oc *OperatorClient, sd *ServiceDefinition, ns, name string) string {
	t.Helper()
	cr, err := oc.GetCR(context.Background(), sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, ns, name)
	require.NoError(t, err)
	spec := cr.Object["spec"].(map[string]interface{})
	storage, ok := spec["storage"].(map[string]interface{})
	require.True(t, ok, "das CR hat keinen storage-Block: %v", spec)
	return storage["size"].(string)
}

func TestReconcile_EineGeaenderteDefinitionErreichtBestehendeInstanzen(t *testing.T) {
	e, oc, _, sd := reconcileEngine(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-up", "default", planSmall, nil))
	require.Equal(t, "1Gi", crStorage(t, oc, sd, "default", "osb-inst-up"))

	// Der Betreiber hebt den Plan an - so, wie es eine neue YAML-Datei täte.
	for i := range sd.Spec.Offering.Plans {
		if sd.Spec.Offering.Plans[i].ID == planSmall {
			sd.Spec.Offering.Plans[i].Params["storageSize"] = "2Gi"
		}
	}

	rec, err := e.regGet(ctx, "inst-up")
	require.NoError(t, err)
	res, err := e.ReconcileInstance(ctx, rec)
	require.NoError(t, err)

	assert.Equal(t, ReconcileApplied, res, "die Abweichung muss angewendet werden")
	assert.Equal(t, "2Gi", crStorage(t, oc, sd, "default", "osb-inst-up"))
}

// Ein Abgleich ohne Abweichung darf nichts schreiben. Ein No-op-Write erhöht
// die resourceVersion und weckt den Operator - bei jedem Durchlauf, für jede
// Instanz.
func TestReconcile_OhneAbweichungWirdNichtsGeschrieben(t *testing.T) {
	e, oc, _, sd := reconcileEngine(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-same", "default", planSmall, nil))
	before, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-same")
	require.NoError(t, err)

	rec, err := e.regGet(ctx, "inst-same")
	require.NoError(t, err)
	res, err := e.ReconcileInstance(ctx, rec)
	require.NoError(t, err)

	assert.Equal(t, ReconcileUpToDate, res)
	after, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-same")
	require.NoError(t, err)
	assert.Equal(t, before.GetResourceVersion(), after.GetResourceVersion(),
		"ein No-op-Write weckt den Operator ohne Grund")
}

// Benutzerparameter überleben den Abgleich. Sie liegen im Datensatz, nicht im
// Plan; würde der Abgleich nur den Plan rendern, setzte er jede Instanz auf
// die Plangröße zurück - eine stille Verkleinerung fremder Datenbanken.
func TestReconcile_BenutzerparameterUeberlebenDenAbgleich(t *testing.T) {
	e, oc, _, sd := reconcileEngine(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-par", "default", planSmall,
		map[string]interface{}{"storageSize": "3Gi"}))
	require.Equal(t, "3Gi", crStorage(t, oc, sd, "default", "osb-inst-par"))

	rec, err := e.regGet(ctx, "inst-par")
	require.NoError(t, err)
	_, err = e.ReconcileInstance(ctx, rec)
	require.NoError(t, err)

	assert.Equal(t, "3Gi", crStorage(t, oc, sd, "default", "osb-inst-par"),
		"der Abgleich darf den Benutzerparameter nicht auf den Planwert zuruecksetzen")
}

// Verschwindet eine Definition aus dem Verzeichnis, meldet der Abgleich das
// und rührt nichts an. Löschen wäre die Katastrophe: eine vergessene Datei
// oder ein Tippfehler im Dateinamen kostete sonst die Datenbank eines Kunden.
func TestReconcile_UnbekannterServiceWirdGemeldetUndNichtAngefasst(t *testing.T) {
	e, oc, _, sd := reconcileEngine(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-orph", "default", planSmall, nil))

	rec, err := e.regGet(ctx, "inst-orph")
	require.NoError(t, err)
	rec.ServiceID = "es-gibt-mich-nicht"

	res, err := e.ReconcileInstance(ctx, rec)

	assert.Equal(t, ReconcileUnresolvable, res)
	assert.True(t, errors.Is(err, ErrServiceUnknown), "der Grund muss erkennbar sein, bekommen: %v", err)
	assert.True(t, crExists(t, oc, sd, "default", "osb-inst-orph"),
		"eine verschwundene Definition darf keine Ressource loeschen")
}

// Dasselbe für einen Plan, den es nicht mehr gibt.
func TestReconcile_UnbekannterPlanWirdGemeldetUndNichtAngefasst(t *testing.T) {
	e, oc, _, sd := reconcileEngine(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-noplan", "default", planSmall, nil))

	rec, err := e.regGet(ctx, "inst-noplan")
	require.NoError(t, err)
	rec.PlanID = "plan-gibt-es-nicht"

	res, err := e.ReconcileInstance(ctx, rec)

	assert.Equal(t, ReconcileUnresolvable, res)
	require.Error(t, err)
	assert.True(t, crExists(t, oc, sd, "default", "osb-inst-noplan"))
}

// Der gefährlichste Fall. Ist die Ressource weg, der Datensatz aber da, darf
// der Abgleich sie NICHT neu anlegen: bei einer Datenbank entstünde eine
// leere, die gesund aussieht. Ein sichtbares Loch ist besser als ein stiller
// Datenverlust, der wie Erfolg aussieht.
func TestReconcile_VerschwundeneRessourceWirdNichtNeuAngelegt(t *testing.T) {
	e, oc, _, sd := reconcileEngine(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-gone", "default", planSmall, nil))
	require.NoError(t, oc.DeleteCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-gone"))

	rec, err := e.regGet(ctx, "inst-gone")
	require.NoError(t, err)
	res, err := e.ReconcileInstance(ctx, rec)

	assert.Equal(t, ReconcileObjectsMissing, res)
	require.Error(t, err, "das Loch muss gemeldet werden, nicht stillschweigend gefuellt")
	assert.False(t, crExists(t, oc, sd, "default", "osb-inst-gone"),
		"der Abgleich darf keine leere Ressource an die Stelle einer geloeschten setzen")
}

// Ein Datensatz ohne Namespace ist nicht abgleichbar - er im Rückfall-Namespace
// zu rendern hieße, im falschen Space zu schreiben.
func TestReconcile_DatensatzOhneNamespaceWirdUebersprungen(t *testing.T) {
	e, _, _, _ := reconcileEngine(t)

	res, err := e.ReconcileInstance(context.Background(), &InstanceRecord{
		ID: "inst-nons", ServiceID: cnpgServiceID, PlanID: planSmall,
	})

	assert.Equal(t, ReconcileUnresolvable, res)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}
