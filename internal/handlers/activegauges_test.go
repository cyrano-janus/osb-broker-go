package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `osb_active_instances` und `osb_active_bindings` waren registriert und
// meldeten dauerhaft 0. Eine Metrik, die immer denselben Wert hat, ist
// schlimmer als keine: sie sieht aus wie eine Messung.
//
// Gezaehlt wird beim Abholen, nicht mitgefuehrt. Ein Zaehler, den der Broker
// bei jedem Provision hochzaehlt, faellt beim Neustart auf 0 zurueck, waehrend
// die Instanzen weiterlaufen - und er verpasst jede Aenderung, die nicht durch
// diesen Prozess ging.

// countingStore zaehlt Instanzen und Bindings und kann dabei scheitern.
type countingStore struct {
	broker.StateStore
	instances int
	bindings  int
	err       error
	calls     int
}

func (c *countingStore) CountInstances(context.Context) (int, error) {
	c.calls++
	return c.instances, c.err
}

func (c *countingStore) CountBindings(context.Context) (int, error) {
	return c.bindings, c.err
}

func gaugeRouter(t *testing.T, store broker.StateStore) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := New(broker.New(store))
	h.SetMetrics(NewMetrics())
	return h.SetupRouter()
}

func scrape(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func TestAktiveGauges_ZaehlenDenEchtenBestand(t *testing.T) {
	store := &countingStore{StateStore: broker.NewInMemoryStateStore(), instances: 3, bindings: 7}
	body := scrape(t, gaugeRouter(t, store))

	assert.Contains(t, body, "osb_active_instances 3")
	assert.Contains(t, body, "osb_active_bindings 7")
}

// Beim Abholen gezaehlt heisst: ein zweiter Scrape sieht einen neuen Wert,
// ohne dass zwischendurch ein Request durch den Broker lief.
func TestAktiveGauges_WerdenBeimAbholenGezaehlt(t *testing.T) {
	store := &countingStore{StateStore: broker.NewInMemoryStateStore(), instances: 1}
	router := gaugeRouter(t, store)

	assert.Contains(t, scrape(t, router), "osb_active_instances 1")
	store.instances = 42
	assert.Contains(t, scrape(t, router), "osb_active_instances 42")
	assert.GreaterOrEqual(t, store.calls, 2, "jeder Scrape muss neu zaehlen")
}

// Ist der Zustandsspeicher nicht lesbar, wird die Metrik weggelassen und der
// Fehler gezaehlt. Eine Luecke im Graphen ist sichtbar; eine stehengebliebene
// Zahl ist es nicht - und wer danach handelt, handelt nach einer Erfindung.
func TestAktiveGauges_UnlesbarerZustandLiefertKeineZahl(t *testing.T) {
	store := &countingStore{StateStore: broker.NewInMemoryStateStore(), err: errors.New("API-Server nicht erreichbar")}
	body := scrape(t, gaugeRouter(t, store))

	assert.NotContains(t, body, "osb_active_instances ")
	assert.NotContains(t, body, "osb_active_bindings ")
	assert.Contains(t, body, "osb_state_read_errors_total")
}

// uncountableStore erfuellt StateStore, aber nicht Counter.
type uncountableStore struct{ broker.StateStore }

// Ein Zustandsspeicher ohne Zaehlung ist erlaubt - dann gibt es die Metrik
// nicht, statt eine 0 zu behaupten.
func TestAktiveGauges_OhneZaehlfaehigkeitKeineMetrik(t *testing.T) {
	body := scrape(t, gaugeRouter(t, uncountableStore{}))

	assert.NotContains(t, body, "osb_active_instances ")
	assert.NotContains(t, body, "osb_active_bindings ")
}

// Der mitgelieferte In-Memory-Speicher kann zaehlen - sonst haetten alle
// Tests, die ihn benutzen, keine Bestandsmetriken.
func TestAktiveGauges_InMemorySpeicherKannZaehlen(t *testing.T) {
	body := scrape(t, gaugeRouter(t, broker.NewInMemoryStateStore()))

	assert.Contains(t, body, "osb_active_instances 0")
}
