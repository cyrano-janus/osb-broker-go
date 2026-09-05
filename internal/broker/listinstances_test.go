package broker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Abgleich braucht die Datensaetze selbst, nicht ihre Zahl. Counter sagt
// "wovon wie viele"; hier geht es darum, jeden einzelnen gegen die Definition
// zu halten, die jetzt gilt.
//
// Warum eine eigene Schnittstelle und nicht ein Feld an StateStore: ein
// Zustandsspeicher, der nicht aufzaehlen kann, soll den Abgleich abschalten
// koennen statt eine leere Liste zu behaupten. Eine leere Liste hiesse "nichts
// abzugleichen", und der Abgleich meldete jeden Durchlauf als erfolgreich.

func TestListInstances_LiefertJedenDatensatzMitPlanUndNamespace(t *testing.T) {
	store := NewInMemoryStateStore()
	ctx := context.Background()

	require.NoError(t, store.PutInstance(ctx, &Instance{
		ID: "a", ServiceID: "pg", PlanID: "small", Namespace: "space-1",
		Parameters: map[string]interface{}{"storageSize": "3Gi"},
	}))
	require.NoError(t, store.PutInstance(ctx, &Instance{
		ID: "b", ServiceID: "mq", PlanID: "dev", Namespace: "space-2",
	}))

	lister, ok := store.(Lister)
	require.True(t, ok, "der mitgelieferte Speicher muss aufzaehlen koennen")

	list, err := lister.ListInstances(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)

	byID := map[string]*Instance{}
	for _, i := range list {
		byID[i.ID] = i
	}
	require.Contains(t, byID, "a")
	assert.Equal(t, "small", byID["a"].PlanID, "ohne Plan laesst sich nicht rendern")
	assert.Equal(t, "space-1", byID["a"].Namespace, "ohne Namespace wird im falschen Space geschrieben")
	assert.Equal(t, "3Gi", byID["a"].Parameters["storageSize"],
		"ohne die Benutzerparameter setzte der Abgleich die Instanz auf den Planwert zurueck")
	assert.Equal(t, "dev", byID["b"].PlanID)
}

func TestListInstances_LeererSpeicherIstKeineFehlermeldung(t *testing.T) {
	list, err := NewInMemoryStateStore().(Lister).ListInstances(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list)
}
