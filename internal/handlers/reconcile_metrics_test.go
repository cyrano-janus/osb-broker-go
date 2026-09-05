package handlers

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/reconcile"
)

func metricsRouter(t *testing.T, m *Metrics) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := New(broker.New(broker.NewInMemoryStateStore()))
	h.SetMetrics(m)
	return h.SetupRouter()
}

// Ohne Metriken ist ein Abgleich, der still scheitert, von einem, der nichts
// zu tun hatte, nicht zu unterscheiden — beide Male passiert nichts.
//
// Zwei Zahlen tragen den Betrieb: **wann lief er zuletzt durch** (bleibt sie
// stehen, läuft er nicht mehr, und niemand merkt es), und **wie viele
// Datensätze sind gerade nicht abgleichbar** — verwaiste Instanzen und
// Instanzen ohne Objekte fallen sonst nirgends auf.

func TestAbgleichMetriken_EinDurchlaufWirdSichtbar(t *testing.T) {
	m := NewMetrics()
	m.ObserveReconcile(reconcile.Result{
		Seen: 5, UpToDate: 3, Applied: 1, Unresolvable: 1,
	})

	body := scrape(t, metricsRouter(t, m))

	assert.Contains(t, body, `osb_reconcile_runs_total{result="ok"} 1`)
	assert.Contains(t, body, `osb_reconcile_instances_total{outcome="applied"} 1`)
	assert.Contains(t, body, `osb_reconcile_instances_total{outcome="up-to-date"} 3`)
	assert.Contains(t, body, "osb_reconcile_last_run_timestamp_seconds")
}

// Ein Durchlauf, der gar nicht stattfinden konnte, darf nicht als erfolgreich
// zählen - sonst sieht ein Graph aus, als liefe alles.
func TestAbgleichMetriken_EinGescheiterterDurchlaufZaehltNichtAlsErfolg(t *testing.T) {
	m := NewMetrics()
	m.ObserveReconcile(reconcile.Result{Err: assert.AnError})

	body := scrape(t, metricsRouter(t, m))

	assert.Contains(t, body, `osb_reconcile_runs_total{result="error"} 1`)
	assert.NotContains(t, body, `osb_reconcile_runs_total{result="ok"}`)
}

// Die beiden Zustände, die sonst niemand sieht: ein Datensatz ohne Definition
// und ein Datensatz ohne Objekte. Als Gauge, weil die Frage "wie viele sind es
// gerade" lautet und nicht "wie oft war das schon so".
func TestAbgleichMetriken_UnauflösbareUndVerschwundeneStehenAlsBestand(t *testing.T) {
	m := NewMetrics()
	m.ObserveReconcile(reconcile.Result{Seen: 9, Unresolvable: 2, ObjectsMissing: 3, UpToDate: 4})

	router := metricsRouter(t, m)
	body := scrape(t, router)
	assert.Contains(t, body, "osb_reconcile_unresolvable_instances 2")
	assert.Contains(t, body, "osb_reconcile_missing_objects 3")

	// Sind sie behoben, muss die Zahl fallen. Ein Gauge, der nur steigt, ist
	// ein Zaehler mit falschem Namen.
	m.ObserveReconcile(reconcile.Result{Seen: 9, UpToDate: 9})
	body = scrape(t, router)
	assert.Contains(t, body, "osb_reconcile_unresolvable_instances 0")
	assert.Contains(t, body, "osb_reconcile_missing_objects 0")
}
