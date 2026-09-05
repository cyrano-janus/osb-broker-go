package handlers

import (
	"context"
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
	instances map[broker.InstanceKey]int
	err       error
	calls     int
}

func (c *countingStore) CountInstances(context.Context) (map[broker.InstanceKey]int, error) {
	c.calls++
	return c.instances, c.err
}

func (c *countingStore) CountBindings(context.Context) (map[string]int, error) {
	return nil, c.err
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

// Beim Abholen gezaehlt heisst: ein zweiter Scrape sieht einen neuen Wert,
// ohne dass zwischendurch ein Request durch den Broker lief.
func TestAktiveGauges_WerdenBeimAbholenGezaehlt(t *testing.T) {
	key := broker.InstanceKey{ServiceID: "pg", PlanID: "small"}
	store := &countingStore{
		StateStore: broker.NewInMemoryStateStore(),
		instances:  map[broker.InstanceKey]int{key: 1},
	}
	router := gaugeRouter(t, store)

	assert.Contains(t, scrape(t, router), `osb_active_instances{plan_id="small",service_id="pg"} 1`)
	store.instances = map[broker.InstanceKey]int{key: 42}
	assert.Contains(t, scrape(t, router), `osb_active_instances{plan_id="small",service_id="pg"} 42`)
	assert.GreaterOrEqual(t, store.calls, 2, "jeder Scrape muss neu zaehlen")
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
