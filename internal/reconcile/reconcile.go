// Package reconcile gleicht bestehende Instanzen periodisch gegen die
// Definitionen ab, die gerade geladen sind.
//
// **Warum es das braucht.** Definitionen werden beim Start gelesen. Ohne
// Abgleich fasst eine geaenderte Definition bestehende Instanzen nicht an: wer
// den Plan "small" von 1Gi auf 2Gi hebt, hebt ihn fuer neue Instanzen und
// laesst die alten stehen. Nach einigen Aenderungen weiss niemand mehr, welche
// Instanz welchen Stand hat, und "welchen Plan hat der Kunde" ist nur noch
// durch Nachsehen im Cluster zu beantworten.
//
// **Was dieses Paket ist und was nicht.** Es ist der Taktgeber und die
// Buchfuehrung: jeden Datensatz genau einmal je Durchlauf vorlegen, zaehlen,
// was dabei herauskam, und weitermachen, wenn einer scheitert. Was ein
// Abgleich bedeutet - und vor allem, was er nicht tut - steht in
// internal/definition/reconcile.go.
//
// **Kein Leader-Election.** Das Chart faehrt eine Replik. Bei mehreren
// arbeiteten sie dasselbe doppelt; korrumpieren koennen sie sich nicht, weil
// der Abgleich vor jedem Schreiben vergleicht und identische Inhalte schreibt.
// Verschwendung ja, Schaden nein - und eine Lease einzufuehren, bevor jemand
// zwei Repliken faehrt, waere Maschinerie ohne Anlass.
package reconcile

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/definition"
)

// Lister liefert die abzugleichenden Datensaetze.
type Lister interface {
	ListInstances(ctx context.Context) ([]*broker.Instance, error)
}

// Engine gleicht einen einzelnen Datensatz ab.
type Engine interface {
	ReconcileInstance(ctx context.Context, rec *definition.InstanceRecord) (definition.ReconcileResult, error)
}

// Options konfiguriert den Taktgeber.
type Options struct {
	// Interval ist der Abstand zwischen zwei Durchlaeufen. 0 schaltet den
	// Abgleich ab - ein Dauerlauf waere sonst die Folge eines vergessenen
	// Wertes.
	Interval time.Duration
	// MaxProblems begrenzt, wie viele Gruende ein Ergebnis mitfuehrt. Bei
	// tausend verwaisten Instanzen ist die tausendste Meldung wortgleich mit
	// der ersten und fuellt nur das Log. 0 = Vorgabe.
	MaxProblems int
	// Observer bekommt jedes Ergebnis. Nil ist erlaubt.
	Observer func(Result)
}

const defaultMaxProblems = 20

// Result ist die Buchfuehrung eines Durchlaufs.
type Result struct {
	Seen           int
	UpToDate       int
	Applied        int
	ObjectsMissing int
	Unresolvable   int
	Failed         int
	// Problems traegt die Gruende im Klartext, gekappt auf MaxProblems.
	Problems []string
	// Err ist gesetzt, wenn der Durchlauf als Ganzes nicht stattfinden
	// konnte - der Zustandsspeicher war nicht lesbar oder der Kontext endete.
	// Das ist etwas anderes als Failed: dort sind einzelne Instanzen
	// gescheitert, hier hat der Durchlauf nichts gesagt.
	Err      error
	Duration time.Duration
}

// Reconciler ist der Taktgeber.
type Reconciler struct {
	lister Lister
	engine Engine
	opts   Options
}

// New baut den Taktgeber.
func New(l Lister, e Engine, o Options) *Reconciler {
	if o.MaxProblems <= 0 {
		o.MaxProblems = defaultMaxProblems
	}
	return &Reconciler{lister: l, engine: e, opts: o}
}

// Enabled meldet, ob ueberhaupt abgeglichen wird.
func (r *Reconciler) Enabled() bool {
	return r != nil && r.opts.Interval > 0 && r.lister != nil && r.engine != nil
}

// Run laeuft, bis der Kontext endet.
//
// Der erste Durchlauf kommt sofort und nicht erst nach einem Intervall: sonst
// wirkte eine geaenderte Definition nach einem Neustart erst nach der ersten
// Wartezeit, und genau der Neustart ist der Moment, in dem sie geladen wurde.
func (r *Reconciler) Run(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	ticker := time.NewTicker(r.opts.Interval)
	defer ticker.Stop()

	for {
		r.runOnceAndReport(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Reconciler) runOnceAndReport(ctx context.Context) {
	res := r.Once(ctx)
	if r.opts.Observer != nil {
		r.opts.Observer(res)
	}
	switch {
	case res.Err != nil && ctx.Err() != nil:
		// Beim Herunterfahren ist ein abgebrochener Durchlauf erwartet und
		// keine Meldung wert.
	case res.Err != nil:
		log.Printf("reconcile: der Durchlauf konnte nicht stattfinden: %v", res.Err)
	case res.Applied > 0 || res.Failed > 0 || res.ObjectsMissing > 0 || res.Unresolvable > 0:
		log.Printf("reconcile: %d Instanzen, %d angeglichen, %d unauflösbar, %d ohne Objekte, %d gescheitert (%s)",
			res.Seen, res.Applied, res.Unresolvable, res.ObjectsMissing, res.Failed, res.Duration.Round(time.Millisecond))
		for _, p := range res.Problems {
			log.Printf("reconcile: %s", p)
		}
	}
}

// Once legt jeden Datensatz genau einmal dem Abgleich vor.
//
// Ein Fehlschlag bricht den Durchlauf nicht ab. Braeche er ab, hielte eine
// einzige verwaiste Instanz jedes Upgrade aller anderen auf, und niemand saehe
// warum.
func (r *Reconciler) Once(ctx context.Context) Result {
	start := time.Now()
	res := Result{}

	list, err := r.lister.ListInstances(ctx)
	if err != nil {
		res.Err = fmt.Errorf("reconcile: der Zustandsspeicher ist nicht lesbar: %w", err)
		res.Duration = time.Since(start)
		return res
	}

	for _, inst := range list {
		if ctx.Err() != nil {
			res.Err = fmt.Errorf("reconcile: abgebrochen nach %d von %d Instanzen: %w",
				res.Seen, len(list), ctx.Err())
			break
		}
		res.Seen++
		outcome, err := r.engine.ReconcileInstance(ctx, recordOf(inst))
		switch outcome {
		case definition.ReconcileUpToDate:
			res.UpToDate++
			continue
		case definition.ReconcileApplied:
			res.Applied++
			continue
		case definition.ReconcileObjectsMissing:
			res.ObjectsMissing++
		case definition.ReconcileUnresolvable:
			res.Unresolvable++
		default:
			res.Failed++
		}
		// Die Instanz-ID gehoert in jede Meldung. Ohne sie steht in einem Log
		// mit zweihundert Instanzen "plan unbekannt", und niemand weiss, bei
		// welcher. Ob die Engine sie mitliefert, darf die Schleife nicht
		// voraussetzen.
		if err != nil && len(res.Problems) < r.opts.MaxProblems {
			res.Problems = append(res.Problems, fmt.Sprintf("%s: %s: %v", inst.ID, outcome, err))
		}
	}

	res.Duration = time.Since(start)
	return res
}

// recordOf uebersetzt den Datensatz des Zustandsspeichers in den, den die
// Engine kennt. Beide beschreiben dieselbe Instanz; getrennt bleiben sie,
// damit die Engine nicht am Zustandsspeicher haengt.
func recordOf(i *broker.Instance) *definition.InstanceRecord {
	return &definition.InstanceRecord{
		ID:             i.ID,
		ServiceID:      i.ServiceID,
		PlanID:         i.PlanID,
		Namespace:      i.Namespace,
		Parameters:     i.Parameters,
		AppliedObjects: i.AppliedObjects,
		AppliedRefs:    refsOf(i.AppliedRefs),
	}
}

func refsOf(in []broker.AppliedObjectRef) []definition.ObjectRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]definition.ObjectRef, 0, len(in))
	for _, r := range in {
		out = append(out, definition.ObjectRef{
			APIVersion: r.APIVersion, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
		})
	}
	return out
}
