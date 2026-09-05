package definition

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// `readiness.timeoutSeconds` stand in jeder Definition, wurde gelesen und nie
// ausgewertet. Ein Operator, der seine Bedingung nie erfuellt, liess
// last_operation unbegrenzt `in progress` melden - die Plattform pollte bis in
// ihr eigenes Zeitlimit und meldete dem Benutzer am Ende nichts Brauchbares.

func TestReadinessTimeout_VorgabewertWennNichtsAngegeben(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Readiness.TimeoutSeconds = 0

	assert.Equal(t, 600*time.Second, ReadinessTimeout(sd),
		"ohne Angabe gilt der im Schema dokumentierte Vorgabewert, nicht 'kein Limit'")
}

func TestReadinessTimeout_AngabeGiltUndNegativSchaltetAb(t *testing.T) {
	sd := testDefinition(t)

	sd.Spec.Readiness.TimeoutSeconds = 42
	assert.Equal(t, 42*time.Second, ReadinessTimeout(sd))

	sd.Spec.Readiness.TimeoutSeconds = -1
	assert.Equal(t, time.Duration(0), ReadinessTimeout(sd),
		"ein negativer Wert ist die ausdrueckliche Abschaltung des Limits")
}

// Gemessen wird ab metadata.creationTimestamp des CR. Das ist der Zeitpunkt,
// ab dem der Operator die Aufgabe hatte, und er ueberlebt einen Neustart des
// Brokers - anders als jede Uhr im Prozess.
func TestReadinessDeadline_AbCreationTimestampDesCR(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Readiness.TimeoutSeconds = 300

	created := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cr := &unstructured.Unstructured{Object: map[string]interface{}{}}
	cr.SetCreationTimestamp(metav1.NewTime(created))

	exceeded, waited := readinessDeadlineExceeded(sd, cr, created.Add(299*time.Second))
	assert.False(t, exceeded, "eine Sekunde vor Ablauf ist nicht abgelaufen")
	assert.Equal(t, 299*time.Second, waited)

	exceeded, waited = readinessDeadlineExceeded(sd, cr, created.Add(301*time.Second))
	assert.True(t, exceeded)
	assert.Equal(t, 301*time.Second, waited)
}

func TestReadinessDeadline_OhneLimitNie(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Readiness.TimeoutSeconds = -1

	created := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cr := &unstructured.Unstructured{Object: map[string]interface{}{}}
	cr.SetCreationTimestamp(metav1.NewTime(created))

	exceeded, _ := readinessDeadlineExceeded(sd, cr, created.Add(10*time.Hour))
	assert.False(t, exceeded)
}

// Ohne creationTimestamp gibt es keine Grundlage fuer eine Frist. Dann lieber
// weiter warten als eine Instanz faelschlich als gescheitert melden.
func TestReadinessDeadline_OhneZeitstempelKeinAbbruch(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Readiness.TimeoutSeconds = 1

	cr := &unstructured.Unstructured{Object: map[string]interface{}{}}
	exceeded, _ := readinessDeadlineExceeded(sd, cr, time.Now())
	assert.False(t, exceeded)
}

// Der ganze Weg: ein CR, das die Bedingung nicht erfuellt und zu alt ist, muss
// last_operation auf `failed` bringen - nicht auf `in progress`.
func TestLastOperation_ZeitlimitUeberschrittenIstFailed(t *testing.T) {
	e, oc, _ := engineWithRegistry(t)
	ctx := context.Background()
	sd := e.definitions[0]
	sd.Spec.Readiness.TimeoutSeconds = 60

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-to", "default", planSmall, nil))

	// Das CR um zwei Stunden altern lassen. Der Operator hat nie einen Status
	// geschrieben, die Bedingung ist also unerfuellt.
	cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-to")
	require.NoError(t, err)
	cr.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-2 * time.Hour)))
	require.NoError(t, oc.Client.Update(ctx, cr))

	state, reason, err := e.LastOperation(ctx, sd, "default", "inst-to")
	require.NoError(t, err)
	assert.Equal(t, "failed", state,
		"nach Ablauf des Zeitlimits ist der Vorgang gescheitert, nicht unterwegs")
	assert.Contains(t, reason, "Zeitlimit")
	assert.Contains(t, reason, "der Operator hat noch keinen Status geschrieben",
		"der urspruengliche Grund darf nicht verlorengehen")
}

// Ein fertiger Dienst ist fertig, auch wenn er lange gebraucht hat.
func TestLastOperation_FertigSchlaegtZeitlimit(t *testing.T) {
	e, oc, _ := engineWithRegistry(t)
	ctx := context.Background()
	sd := e.definitions[0]
	sd.Spec.Readiness.TimeoutSeconds = 60

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-ok", "default", planSmall, nil))

	cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-ok")
	require.NoError(t, err)
	cr.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-2 * time.Hour)))
	require.NoError(t, unstructured.SetNestedSlice(cr.Object,
		[]interface{}{map[string]interface{}{"type": "Ready", "status": "True"}},
		"status", "conditions"))
	require.NoError(t, oc.Client.Update(ctx, cr))

	state, _, err := e.LastOperation(ctx, sd, "default", "inst-ok")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", state)
}

// Innerhalb der Frist bleibt es beim bisherigen Verhalten.
func TestLastOperation_InnerhalbDerFristBleibtInProgress(t *testing.T) {
	e, _, _ := engineWithRegistry(t)
	ctx := context.Background()
	sd := e.definitions[0]
	sd.Spec.Readiness.TimeoutSeconds = 600

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-young", "default", planSmall, nil))

	state, reason, err := e.LastOperation(ctx, sd, "default", "inst-young")
	require.NoError(t, err)
	assert.Equal(t, "in progress", state)
	assert.NotContains(t, reason, "Zeitlimit")
}
