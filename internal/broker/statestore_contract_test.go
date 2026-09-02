package broker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Vertrag, den JEDE StateStore-Implementierung erfuellen muss.
//
// Vorher gab es zwei getrennte Testdateien, die sich nur teilweise
// ueberschnitten - der ConfigMap-Store wurde auf Dinge geprueft, die der
// In-Memory-Store nie sehen musste, und umgekehrt. Genau so entstehen
// Unterschiede, die erst im Cluster auffallen: ein Test, der gegen den
// Speicher gruen ist, sagt dann nichts ueber den echten Store.
//
// Neue Implementierungen rufen runStateStoreContract auf und muessen die
// Suite unveraendert bestehen.
func runStateStoreContract(t *testing.T, newStore func(t *testing.T) StateStore) {
	t.Helper()

	t.Run("Instanz schreiben und vollstaendig zurueckbekommen", func(t *testing.T) {
		s := newStore(t)
		in := newTestInstance("inst-roundtrip")
		in.AppliedObjects = []string{"osb-inst-roundtrip"}
		in.AppliedRefs = []AppliedObjectRef{{
			APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster",
			Namespace: "team-a", Name: "osb-inst-roundtrip",
		}}
		require.NoError(t, s.PutInstance(context.Background(), in))

		got, err := s.GetInstance(context.Background(), "inst-roundtrip")
		require.NoError(t, err)
		assert.Equal(t, in, got, "jedes Feld muss die Persistenz ueberleben")
	})

	t.Run("unbekannte Instanz meldet ErrNotFound", func(t *testing.T) {
		s := newStore(t)
		_, err := s.GetInstance(context.Background(), "gibt-es-nicht")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotFound), "Aufrufer unterscheiden 404 von 500 daran")
	})

	t.Run("Instanz ueberschreiben ersetzt den Datensatz", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		require.NoError(t, s.PutInstance(ctx, newTestInstance("inst-update")))

		updated := newTestInstance("inst-update")
		updated.PlanID = "plan-large"
		updated.Ready = false
		require.NoError(t, s.PutInstance(ctx, updated))

		got, err := s.GetInstance(ctx, "inst-update")
		require.NoError(t, err)
		assert.Equal(t, "plan-large", got.PlanID)
		assert.False(t, got.Ready)
	})

	t.Run("Instanz loeschen", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		require.NoError(t, s.PutInstance(ctx, newTestInstance("inst-del")))
		require.NoError(t, s.DeleteInstance(ctx, "inst-del"))

		_, err := s.GetInstance(ctx, "inst-del")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("unbekannte Instanz loeschen ist kein Fehler", func(t *testing.T) {
		// Deprovision ist idempotent: ein zweiter Aufruf darf nicht 500 werden.
		s := newStore(t)
		assert.NoError(t, s.DeleteInstance(context.Background(), "nie-dagewesen"))
	})

	t.Run("Binding schreiben und vollstaendig zurueckbekommen", func(t *testing.T) {
		s := newStore(t)
		b := newTestBinding("bind-roundtrip", "inst-1")
		b.SyslogDrainURL = "syslog://drain.example.com"
		b.RouteServiceURL = "https://route.example.com"
		require.NoError(t, s.PutBinding(context.Background(), b))

		got, err := s.GetBinding(context.Background(), "bind-roundtrip")
		require.NoError(t, err)
		assert.Equal(t, b, got)
	})

	t.Run("unbekanntes Binding meldet ErrNotFound", func(t *testing.T) {
		s := newStore(t)
		_, err := s.GetBinding(context.Background(), "gibt-es-nicht")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("Binding loeschen", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		require.NoError(t, s.PutBinding(ctx, newTestBinding("bind-del", "inst-1")))
		require.NoError(t, s.DeleteBinding(ctx, "bind-del"))

		_, err := s.GetBinding(ctx, "bind-del")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("unbekanntes Binding loeschen ist kein Fehler", func(t *testing.T) {
		s := newStore(t)
		assert.NoError(t, s.DeleteBinding(context.Background(), "nie-dagewesen"))
	})

	t.Run("Bindings nach Instanz filtern", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		require.NoError(t, s.PutBinding(ctx, newTestBinding("b1", "inst-a")))
		require.NoError(t, s.PutBinding(ctx, newTestBinding("b2", "inst-a")))
		require.NoError(t, s.PutBinding(ctx, newTestBinding("b3", "inst-b")))

		got, err := s.ListBindingsByInstance(ctx, "inst-a")
		require.NoError(t, err)
		require.Len(t, got, 2, "Deprovision haengt daran: eine Instanz mit Bindings darf nicht weg")
		ids := []string{got[0].ID, got[1].ID}
		assert.ElementsMatch(t, []string{"b1", "b2"}, ids)
	})

	t.Run("Bindings einer unbekannten Instanz sind leer, kein Fehler", func(t *testing.T) {
		s := newStore(t)
		got, err := s.ListBindingsByInstance(context.Background(), "inst-ohne-bindings")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("gespeicherte Daten sind vom Aufrufer entkoppelt", func(t *testing.T) {
		// Ein persistenter Store serialisiert und ist damit automatisch
		// entkoppelt. Ein In-Memory-Store, der nur die Struktur flach kopiert,
		// teilt Maps und Slices weiter mit dem Aufrufer - ein Test, der gegen
		// den Speicher gruen ist, verdeckt dann eine Aenderung, die im Cluster
		// nie passiert waere. Deshalb steht das im gemeinsamen Vertrag.
		s := newStore(t)
		ctx := context.Background()

		in := newTestInstance("inst-isolation")
		in.AppliedObjects = []string{"objekt-1"}
		require.NoError(t, s.PutInstance(ctx, in))

		in.Parameters["foo"] = "nachtraeglich geaendert"
		in.AppliedObjects[0] = "nachtraeglich geaendert"

		got, err := s.GetInstance(ctx, "inst-isolation")
		require.NoError(t, err)
		assert.Equal(t, "bar", got.Parameters["foo"], "Schreiben darf keine Referenz behalten")
		assert.Equal(t, "objekt-1", got.AppliedObjects[0])

		got.Parameters["foo"] = "vom Leser geaendert"
		again, err := s.GetInstance(ctx, "inst-isolation")
		require.NoError(t, err)
		assert.Equal(t, "bar", again.Parameters["foo"], "Lesen darf keine Referenz herausgeben")
	})

	t.Run("IDs, die keine gueltigen Kubernetes-Namen sind", func(t *testing.T) {
		// OSB-IDs sind beliebige Strings. Die Plattform schickt zwar UUIDs,
		// aber die Spezifikation garantiert das nicht - und ein Store, der
		// Objektnamen daraus ableitet, muss auch Grossbuchstaben, Punkte,
		// Unterstriche und Ueberlaenge vertragen, ohne zwei IDs zu verwechseln.
		s := newStore(t)
		ctx := context.Background()

		ids := []string{
			"Instance_With_UPPER.and.dots",
			strings.Repeat("sehr-lange-id-", 12),
			"id/mit/slashes",
			"-fuehrender-bindestrich",
		}
		for _, id := range ids {
			require.NoError(t, s.PutInstance(ctx, newTestInstance(id)), "id %q", id)
		}
		for _, id := range ids {
			got, err := s.GetInstance(ctx, id)
			require.NoError(t, err, "id %q", id)
			assert.Equal(t, id, got.ID, "die urspruengliche ID muss erhalten bleiben")
		}
	})
}

func TestStateStoreContract_InMemory(t *testing.T) {
	runStateStoreContract(t, func(t *testing.T) StateStore { return NewInMemoryStateStore() })
}
