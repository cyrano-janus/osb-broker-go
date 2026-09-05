package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/reconcile"
)

// Der Abgleich Ende zu Ende: über HTTP provisioniert, die Definition geändert,
// abgeglichen, im CR nachgesehen. Die Einzelteile sind je für sich geprüft;
// hier geht es darum, dass sie zusammenpassen — Zustandsspeicher, Engine und
// Taktgeber sind drei Pakete, und die Übersetzung zwischen ihren Datensätzen
// ist genau die Stelle, an der ein Feld verlorengeht.

func TestAbgleichEndeZuEnde_EineGeaenderteDefinitionErreichtEineLaufendeInstanz(t *testing.T) {
	router, oc := newDefinitionRouter(t)
	ctx := context.Background()
	const instanceID = "rec-e2e-1"

	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{
			"service_id": "def-svc-0001", "plan_id": "def-plan-free",
			"parameters": map[string]interface{}{"size": "mittel"},
		}).Code)

	cr, err := oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "osb-"+instanceID)
	require.NoError(t, err)
	require.Equal(t, "mittel", cr.Object["spec"].(map[string]interface{})["size"])

	// Der Betreiber legt eine geänderte Definition nach. Der Broker liest
	// Definitionen beim Start; hier ist die geladene Fassung dieselbe, die
	// der Abgleich benutzt.
	sd, err := testEngine.DefinitionByServiceID("def-svc-0001")
	require.NoError(t, err)
	sd.Spec.Provision.Template = `apiVersion: test.example.com/v1
kind: Database
metadata:
  name: {{ .safeName }}
  labels:
    stufe: "zwei"
spec:
  size: {{ .plan.size }}
`

	r := reconcile.New(testStore.(broker.Lister), testEngine, reconcile.Options{Interval: time.Hour})
	res := r.Once(ctx)

	require.NoError(t, res.Err)
	assert.Equal(t, 1, res.Seen)
	assert.Equal(t, 1, res.Applied, "die geaenderte Definition muss ankommen: %v", res.Problems)

	cr, err = oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "osb-"+instanceID)
	require.NoError(t, err)
	assert.Equal(t, "zwei", cr.GetLabels()["stufe"])
	assert.Equal(t, "mittel", cr.Object["spec"].(map[string]interface{})["size"],
		"der Benutzerparameter muss den Abgleich ueberleben")
}

// Ein zweiter Durchlauf ohne Änderung darf nichts mehr tun.
func TestAbgleichEndeZuEnde_ZweiterDurchlaufIstStill(t *testing.T) {
	router, _ := newDefinitionRouter(t)
	ctx := context.Background()

	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/rec-e2e-2",
		map[string]interface{}{"service_id": "def-svc-0001", "plan_id": "def-plan-free"}).Code)

	r := reconcile.New(testStore.(broker.Lister), testEngine, reconcile.Options{Interval: time.Hour})
	require.NoError(t, r.Once(ctx).Err)

	res := r.Once(ctx)
	assert.Equal(t, 1, res.UpToDate)
	assert.Zero(t, res.Applied, "ohne Aenderung darf nichts geschrieben werden")
}

// Metriken ohne Metrics-Objekt dürfen nicht abstürzen: METRICS_ENABLED=0 ist
// eine gültige Konfiguration, und main reicht den Beobachter trotzdem durch.
func TestAbgleichMetriken_OhneMetrikenKeinAbsturz(t *testing.T) {
	var m *Metrics
	assert.NotPanics(t, func() { m.ObserveReconcile(reconcile.Result{Seen: 1}) })
}
