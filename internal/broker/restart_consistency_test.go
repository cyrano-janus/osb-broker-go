package broker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ein neuer Prozess auf demselben Zustandsspeicher muss dieselben Antworten
// geben wie der alte. Das ist der Grund, warum der Zustand nicht im Speicher
// des Brokers liegt: ein Rescheduling darf keine Instanz und keine Binding
// vergessen.
//
// Frueher lief dieser Nachweis ueber Provision und Bind des zweiten Brokers,
// den es nicht mehr gibt. Er laeuft jetzt ueber die Buchfuehrung selbst -
// naeher an dem, was tatsaechlich ueberleben muss.

func recordInstance(t *testing.T, b *Broker, instanceID string) {
	t.Helper()
	require.NoError(t, b.RecordInstance(context.Background(), &Instance{
		ID:        instanceID,
		ServiceID: "def-svc-0001",
		PlanID:    "def-plan-free",
		Namespace: "space-1",
		Context:   Context{Platform: "cloudfoundry"},
		Ready:     true,
	}))
}

func recordBinding(t *testing.T, b *Broker, instanceID, bindingID string) {
	t.Helper()
	require.NoError(t, b.RecordBinding(context.Background(), &Binding{
		ID:          bindingID,
		InstanceID:  instanceID,
		ServiceID:   "def-svc-0001",
		PlanID:      "def-plan-free",
		Credentials: map[string]interface{}{"username": "app"},
		Ready:       true,
	}))
}

func TestRestart_BindingUeberlebt(t *testing.T) {
	s := NewInMemoryStateStore()
	b1 := New(s)
	recordInstance(t, b1, "inst-1")
	recordBinding(t, b1, "inst-1", "bind-1")

	b2 := New(s)

	resp, err := b2.GetBinding(context.Background(), "inst-1", "bind-1")
	require.NoError(t, err)
	assert.Equal(t, "app", resp.Credentials["username"])
}

func TestRestart_InstanzUeberlebtMitNamespace(t *testing.T) {
	// Der Namespace ist der Teil, der aus spaeteren Requests nicht
	// herleitbar ist: ein Deprovision traegt weder context noch space_guid.
	// Geht er verloren, sucht der Broker am falschen Ort und meldet Erfolg,
	// waehrend die Datenbank weiterlaeuft (FINDINGS #7).
	s := NewInMemoryStateStore()
	recordInstance(t, New(s), "inst-1")

	inst, err := New(s).StoredInstance(context.Background(), "inst-1")

	require.NoError(t, err)
	assert.Equal(t, "space-1", inst.Namespace)
}

func TestRestart_BindingBleibtDerInstanzZugeordnet(t *testing.T) {
	// Ein Deprovision muss bestehende Bindings erkennen koennen, auch nach
	// einem Neustart - sonst verschwindet der Dienst unter einer Anwendung,
	// die noch daran gebunden ist.
	s := NewInMemoryStateStore()
	b1 := New(s)
	recordInstance(t, b1, "inst-1")
	recordBinding(t, b1, "inst-1", "bind-1")

	bindings, err := New(s).BindingsOfInstance(context.Background(), "inst-1")

	require.NoError(t, err)
	require.Len(t, bindings, 1)
	assert.Equal(t, "bind-1", bindings[0].ID)
	assert.True(t, bindings[0].Ready)
}

func TestRestart_VergesseneBindingIstWirklichWeg(t *testing.T) {
	s := NewInMemoryStateStore()
	b1 := New(s)
	recordInstance(t, b1, "inst-1")
	recordBinding(t, b1, "inst-1", "bind-1")
	require.NoError(t, b1.ForgetBinding(context.Background(), "bind-1"))

	_, err := New(s).GetBinding(context.Background(), "inst-1", "bind-1")

	require.Error(t, err, "sonst bekaeme ein erneutes Bind derselben ID die alten Zugangsdaten")
}

func TestRestart_BindingEinerFremdenInstanzWirdNichtHerausgegeben(t *testing.T) {
	s := NewInMemoryStateStore()
	b := New(s)
	recordInstance(t, b, "inst-1")
	recordBinding(t, b, "inst-1", "bind-1")

	_, err := b.GetBinding(context.Background(), "inst-2", "bind-1")

	require.Error(t, err)
}
