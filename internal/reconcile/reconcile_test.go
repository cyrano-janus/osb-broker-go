package reconcile

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/definition"
)

// Die Schleife selbst hat eine einzige Aufgabe: jeden Datensatz genau einmal
// je Durchlauf dem Abgleich vorlegen und Buch führen. Was ein Abgleich
// bedeutet, steht in internal/definition.
//
// Die Eigenschaft, die zählt: **ein kaputter Datensatz darf die anderen nicht
// aufhalten.** Bräche der Durchlauf beim ersten Fehler ab, hielte eine einzige
// verwaiste Instanz jedes Upgrade aller anderen auf — und niemand sähe warum.

type fakeLister struct {
	items []*broker.Instance
	err   error
	calls int
}

func (f *fakeLister) ListInstances(context.Context) ([]*broker.Instance, error) {
	f.calls++
	return f.items, f.err
}

type fakeEngine struct {
	mu      sync.Mutex
	seen    []string
	results map[string]definition.ReconcileResult
	errs    map[string]error
}

func (f *fakeEngine) ReconcileInstance(_ context.Context, rec *definition.InstanceRecord) (definition.ReconcileResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, rec.ID)
	res, ok := f.results[rec.ID]
	if !ok {
		res = definition.ReconcileUpToDate
	}
	return res, f.errs[rec.ID]
}

func inst(id, svc, plan, ns string) *broker.Instance {
	return &broker.Instance{ID: id, ServiceID: svc, PlanID: plan, Namespace: ns}
}

func newFor(t *testing.T, l *fakeLister, e *fakeEngine) *Reconciler {
	t.Helper()
	return New(l, e, Options{Interval: time.Hour})
}

func TestAbgleich_JederDatensatzWirdGenauEinmalVorgelegt(t *testing.T) {
	l := &fakeLister{items: []*broker.Instance{
		inst("a", "pg", "small", "s1"), inst("b", "pg", "large", "s1"), inst("c", "mq", "dev", "s2"),
	}}
	e := &fakeEngine{results: map[string]definition.ReconcileResult{}}

	res := newFor(t, l, e).Once(context.Background())

	assert.ElementsMatch(t, []string{"a", "b", "c"}, e.seen)
	assert.Equal(t, 3, res.Seen)
	assert.Equal(t, 3, res.UpToDate)
}

// Die wichtigste Zusage der Schleife.
func TestAbgleich_EinKaputterDatensatzHaeltDieAnderenNichtAuf(t *testing.T) {
	l := &fakeLister{items: []*broker.Instance{
		inst("a", "pg", "small", "s1"), inst("kaputt", "weg", "weg", "s1"), inst("c", "mq", "dev", "s2"),
	}}
	e := &fakeEngine{
		results: map[string]definition.ReconcileResult{"kaputt": definition.ReconcileUnresolvable},
		errs:    map[string]error{"kaputt": errors.New("service unbekannt")},
	}

	res := newFor(t, l, e).Once(context.Background())

	assert.ElementsMatch(t, []string{"a", "kaputt", "c"}, e.seen,
		"nach dem Fehlschlag muss weitergemacht werden")
	assert.Equal(t, 1, res.Unresolvable)
	assert.Equal(t, 2, res.UpToDate)
	require.Len(t, res.Problems, 1, "der Grund muss berichtet werden, nicht nur gezaehlt")
	assert.Contains(t, res.Problems[0], "kaputt")
}

func TestAbgleich_JedesErgebnisWirdEinzelnGezaehlt(t *testing.T) {
	l := &fakeLister{items: []*broker.Instance{
		inst("ok", "pg", "small", "s"), inst("drift", "pg", "small", "s"),
		inst("weg", "pg", "small", "s"), inst("nix", "pg", "small", "s"),
		inst("boom", "pg", "small", "s"),
	}}
	e := &fakeEngine{results: map[string]definition.ReconcileResult{
		"drift": definition.ReconcileApplied,
		"weg":   definition.ReconcileObjectsMissing,
		"nix":   definition.ReconcileUnresolvable,
		"boom":  definition.ReconcileFailed,
	}, errs: map[string]error{
		"weg": errors.New("objekte weg"), "nix": errors.New("plan weg"), "boom": errors.New("operator lehnt ab"),
	}}

	res := newFor(t, l, e).Once(context.Background())

	assert.Equal(t, 5, res.Seen)
	assert.Equal(t, 1, res.UpToDate)
	assert.Equal(t, 1, res.Applied)
	assert.Equal(t, 1, res.ObjectsMissing)
	assert.Equal(t, 1, res.Unresolvable)
	assert.Equal(t, 1, res.Failed)
}

// Ein Speicher, der nicht antwortet, darf nicht als "nichts abzugleichen"
// durchgehen - sonst meldet der Abgleich Erfolg, ohne etwas geprueft zu haben.
func TestAbgleich_UnlesbarerSpeicherIstEinFehlschlagUndKeinLeererLauf(t *testing.T) {
	l := &fakeLister{err: errors.New("API-Server nicht erreichbar")}
	e := &fakeEngine{}

	res := newFor(t, l, e).Once(context.Background())

	assert.Zero(t, res.Seen)
	require.Error(t, res.Err)
	assert.Empty(t, e.seen, "ohne Liste darf nichts angefasst werden")
}

// Ein abgebrochener Kontext muss den Durchlauf beenden, nicht ihn zu Ende
// arbeiten. Beim Herunterfahren steht sonst ein Pod, der noch schreibt.
func TestAbgleich_AbgebrochenerKontextBeendetDenDurchlauf(t *testing.T) {
	items := make([]*broker.Instance, 50)
	for i := range items {
		items[i] = inst(string(rune('a'+i%26))+string(rune('0'+i/26)), "pg", "small", "s")
	}
	l := &fakeLister{items: items}
	e := &fakeEngine{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := newFor(t, l, e).Once(ctx)

	assert.Less(t, len(e.seen), len(items), "der Abbruch muss wirken")
	assert.Error(t, res.Err)
}

// Die Schleife läuft, bis der Kontext endet - und der erste Durchlauf kommt
// sofort, nicht erst nach einem Intervall. Sonst wirkt eine geänderte
// Definition nach einem Neustart erst nach der ersten Wartezeit.
func TestAbgleich_ErsterDurchlaufKommtSofort(t *testing.T) {
	l := &fakeLister{items: []*broker.Instance{inst("a", "pg", "small", "s")}}
	e := &fakeEngine{}
	r := New(l, e, Options{Interval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	assert.Eventually(t, func() bool { return l.calls >= 1 }, 2*time.Second, 5*time.Millisecond,
		"der erste Durchlauf darf nicht auf das Intervall warten")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run endet nicht mit dem Kontext")
	}
}

// Ein Intervall von 0 hiesse Dauerlauf. Es muss abgeschaltet sein statt
// unbeabsichtigt zu rasen.
func TestAbgleich_OhneIntervallLaeuftNichts(t *testing.T) {
	l := &fakeLister{}
	r := New(l, &fakeEngine{}, Options{Interval: 0})

	assert.False(t, r.Enabled())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r.Run(ctx)

	assert.Zero(t, l.calls, "ohne Intervall darf kein Durchlauf stattfinden")
}
