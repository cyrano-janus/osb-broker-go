package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `osb_active_instances` sagte nur, wie viele Instanzen es gibt. Für einen
// Betreiber ist die Frage aber „wovon wie viele" — welches Angebot wird
// genutzt, welcher Plan, wo entstehen Kosten.
//
// **Was der Broker bewusst NICHT misst:** die Gesundheit der Dienste, die er
// herstellt. Ein Broker, der Postgres-Metriken nachbaut, überschreitet seine
// Aufgabe und dupliziert, was CloudNativePG und der RabbitMQ-Operator selbst
// exportieren. Zudem kostete es je Scrape einen API-Aufruf pro Instanz.
// Der Broker misst, was nur er weiß: den OSB-Bestand.

type inventoryStore struct {
	broker.StateStore
	instances map[broker.InstanceKey]int
	bindings  map[string]int
	err       error
}

func (i *inventoryStore) CountInstances(context.Context) (map[broker.InstanceKey]int, error) {
	return i.instances, i.err
}

func (i *inventoryStore) CountBindings(context.Context) (map[string]int, error) {
	return i.bindings, i.err
}

func TestBestand_WirdNachAngebotUndPlanAufgeschluesselt(t *testing.T) {
	store := &inventoryStore{
		StateStore: broker.NewInMemoryStateStore(),
		instances: map[broker.InstanceKey]int{
			{ServiceID: "pg", PlanID: "small"}: 3,
			{ServiceID: "pg", PlanID: "large"}: 1,
			{ServiceID: "mq", PlanID: "dev"}:   2,
		},
		bindings: map[string]int{"pg": 7},
	}
	body := scrape(t, gaugeRouter(t, store))

	assert.Contains(t, body, `osb_active_instances{plan_id="small",service_id="pg"} 3`)
	assert.Contains(t, body, `osb_active_instances{plan_id="large",service_id="pg"} 1`)
	assert.Contains(t, body, `osb_active_instances{plan_id="dev",service_id="mq"} 2`)
	assert.Contains(t, body, `osb_active_bindings{service_id="pg"} 7`)
}

// Ein Plan, der auf 0 faellt, muss verschwinden statt eine alte Zahl zu
// behalten - sonst zeigt ein Graph dauerhaft Instanzen, die es nicht gibt.
func TestBestand_LeererPlanVerschwindet(t *testing.T) {
	store := &inventoryStore{
		StateStore: broker.NewInMemoryStateStore(),
		instances:  map[broker.InstanceKey]int{{ServiceID: "pg", PlanID: "small"}: 2},
	}
	router := gaugeRouter(t, store)
	require.Contains(t, scrape(t, router), `osb_active_instances{plan_id="small",service_id="pg"} 2`)

	store.instances = map[broker.InstanceKey]int{}
	assert.NotContains(t, scrape(t, router), `plan_id="small"`,
		"beim Abholen gezaehlt heisst auch: was weg ist, ist weg")
}

func TestBestand_UnlesbarerZustandLiefertKeineZahl(t *testing.T) {
	store := &inventoryStore{
		StateStore: broker.NewInMemoryStateStore(),
		err:        errors.New("API-Server nicht erreichbar"),
	}
	body := scrape(t, gaugeRouter(t, store))

	assert.NotContains(t, body, "osb_active_instances{")
	assert.Contains(t, body, "osb_state_read_errors_total")
}

// Der mitgelieferte In-Memory-Speicher muss die Aufschluesselung koennen,
// sonst haetten alle Tests, die ihn benutzen, keine Bestandsmetriken.
func TestBestand_InMemorySpeicherSchluesseltAuf(t *testing.T) {
	store := broker.NewInMemoryStateStore()
	ctx := context.Background()
	require.NoError(t, store.PutInstance(ctx, &broker.Instance{ID: "a", ServiceID: "pg", PlanID: "small"}))
	require.NoError(t, store.PutInstance(ctx, &broker.Instance{ID: "b", ServiceID: "pg", PlanID: "small"}))
	require.NoError(t, store.PutInstance(ctx, &broker.Instance{ID: "c", ServiceID: "mq", PlanID: "dev"}))

	counts, err := store.(broker.Counter).CountInstances(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, counts[broker.InstanceKey{ServiceID: "pg", PlanID: "small"}])
	assert.Equal(t, 1, counts[broker.InstanceKey{ServiceID: "mq", PlanID: "dev"}])
}
