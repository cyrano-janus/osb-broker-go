package definition

import (
	"context"
	"errors"
	"fmt"
)

// Der Abgleich einer einzelnen Instanz gegen die Definition, die jetzt gilt.
//
// Definitionen werden beim Start gelesen. Ohne Abgleich fasst eine geaenderte
// Definition bestehende Instanzen nicht an: wer den Plan "small" von 1Gi auf
// 2Gi hebt, hebt ihn fuer neue Instanzen und laesst die alten stehen. Nach
// einigen Aenderungen weiss niemand mehr, welche Instanz welchen Stand hat.
//
// **Der Abgleich ist absichtlich zurueckhaltend.** Er schreibt genau eine Art
// von Aenderung - das gerenderte Manifest auf den Stand der Definition - und
// nichts sonst. Er loescht nie, und er legt nie an. Ein Abgleich, der
// aufraeumt, was er nicht versteht, kostet bei einem Tippfehler in einem
// Dateinamen die Datenbank eines Kunden; einer, der Fehlendes ersetzt, stellt
// eine leere Datenbank an die Stelle einer geloeschten und laesst sie gesund
// aussehen.

// ReconcileResult sagt, was ein Abgleich mit einem Datensatz gemacht hat.
type ReconcileResult string

const (
	// ReconcileUpToDate: das Manifest steht schon so da, nichts geschrieben.
	ReconcileUpToDate ReconcileResult = "up-to-date"
	// ReconcileApplied: es gab eine Abweichung, sie wurde angewendet.
	ReconcileApplied ReconcileResult = "applied"
	// ReconcileUnresolvable: der Datensatz laesst sich nicht auflösen -
	// Definition, Plan oder Namespace fehlen. Nichts angefasst.
	ReconcileUnresolvable ReconcileResult = "unresolvable"
	// ReconcileObjectsMissing: der Datensatz ist da, seine Ressourcen nicht.
	// Nichts angelegt.
	ReconcileObjectsMissing ReconcileResult = "objects-missing"
	// ReconcileFailed: der Abgleich wurde versucht und ist gescheitert - der
	// Operator hat die Aenderung abgelehnt, oder der API-Server war nicht
	// erreichbar.
	ReconcileFailed ReconcileResult = "failed"
)

// ErrObjectsMissing meldet einen Datensatz, dessen Objekte verschwunden sind.
var ErrObjectsMissing = errors.New("the instance's objects are gone")

// ReconcileInstance gleicht einen Datensatz gegen die geltende Definition ab.
//
// Gerendert wird aus dem gespeicherten Plan und den gespeicherten Parametern -
// nicht nur aus dem Plan. Ein Abgleich, der die Benutzerparameter vergaesse,
// setzte jede Instanz auf die Plangroesse zurueck, und das waere bei einem
// Speicher eine stille Verkleinerung.
func (e *Engine) ReconcileInstance(ctx context.Context, rec *InstanceRecord) (ReconcileResult, error) {
	if rec == nil || rec.ID == "" {
		return ReconcileUnresolvable, fmt.Errorf("reconcile: empty instance record")
	}
	if rec.Namespace == "" {
		// Im Rueckfall-Namespace zu rendern hiesse, im falschen Space zu
		// schreiben - siehe die Begruendung an InstanceRecord.Namespace.
		return ReconcileUnresolvable, fmt.Errorf("reconcile %q: no namespace on the record", rec.ID)
	}

	sd, err := e.DefinitionByServiceID(rec.ServiceID)
	if err != nil {
		return ReconcileUnresolvable, fmt.Errorf("reconcile %q: %w", rec.ID, err)
	}
	if _, err := sd.PlanByID(rec.PlanID); err != nil {
		return ReconcileUnresolvable, fmt.Errorf("reconcile %q: %w", rec.ID, err)
	}

	// Erst nachsehen, ob es ueberhaupt noch etwas abzugleichen gibt. Das
	// Anwenden legt sonst an, was fehlt - und eine neu angelegte Datenbank ist
	// leer, sieht aber gesund aus.
	present, err := e.objectsPresent(ctx, sd, rec)
	if err != nil {
		return ReconcileFailed, fmt.Errorf("reconcile %q: %w", rec.ID, err)
	}
	if !present {
		return ReconcileObjectsMissing, fmt.Errorf("reconcile %q: %w", rec.ID, ErrObjectsMissing)
	}

	// Leere Parameter: updateDefinition legt sie ueber die gespeicherten, es
	// bleiben also genau die gespeicherten. Gerendert wird aber mit der
	// Definition, die jetzt geladen ist - darin liegt der Abgleich.
	before, err := e.manifestUpToDate(ctx, sd, rec)
	if err != nil {
		return ReconcileFailed, fmt.Errorf("reconcile %q: %w", rec.ID, err)
	}
	if before {
		return ReconcileUpToDate, nil
	}
	if _, err := e.updateDefinition(ctx, sd, rec.ID, rec.Namespace, rec.PlanID, nil); err != nil {
		return ReconcileFailed, fmt.Errorf("reconcile %q: %w", rec.ID, err)
	}
	return ReconcileApplied, nil
}

// objectsPresent meldet, ob mindestens ein Objekt der Instanz noch da ist.
//
// Mindestens eines und nicht alle: ein mehrteiliges Manifest, dem ein Teil
// fehlt, ist ein Fall fuer den Abgleich - er stellt den fehlenden Teil neben
// den vorhandenen. Ist dagegen nichts mehr da, wurde die Instanz von aussen
// entfernt, und der Abgleich haette nichts zu ergaenzen, sondern alles neu
// anzulegen.
func (e *Engine) objectsPresent(ctx context.Context, sd *ServiceDefinition, rec *InstanceRecord) (bool, error) {
	refs := retainableRefs(sd, rec, SanitizeInstanceName(rec.ID), rec.Namespace)
	for _, ref := range refs {
		_, err := e.op.GetCR(ctx, ref.APIVersion, ref.Kind, ref.Namespace, ref.Name)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, ErrResourceGone) {
			return false, err
		}
	}
	return false, nil
}

// manifestUpToDate rendert aus dem Datensatz und vergleicht mit dem Bestand,
// ohne zu schreiben.
func (e *Engine) manifestUpToDate(ctx context.Context, sd *ServiceDefinition, rec *InstanceRecord) (bool, error) {
	plan, err := sd.PlanByID(rec.PlanID)
	if err != nil {
		return false, err
	}
	rendered, err := RenderProvision(sd, rec.ID, plan.Params, rec.Parameters)
	if err != nil {
		return false, err
	}
	return e.op.ManifestsUpToDate(ctx,
		sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, rec.Namespace, rendered)
}
