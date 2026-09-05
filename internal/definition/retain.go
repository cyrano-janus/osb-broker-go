package definition

import (
	"context"
	"fmt"
	"time"
)

// Loeschschutz. `cf delete-service` loescht die Backing-Ressource sofort - fuer
// einen Entwicklungsplan richtig, fuer einen Produktionsplan die
// unwiderrufliche Loeschung einer Datenbank auf einen Tastendruck. OSB kennt
// fuer das Deprovision keinen Weg, eine Bestaetigung zu uebermitteln: der
// Request traegt weder Koerper noch Parameter, mit dem ein Benutzer "ja
// wirklich" sagen koennte.
//
// Deshalb entscheidet der Plan. Bei `retainOnDeprovision` gibt der Broker die
// Instanz auf, laesst die Ressourcen aber stehen und markiert sie.

const (
	// LabelRetainedInstance nennt die OSB-Instanz, zu der eine
	// stehengelassene Ressource gehoerte. Ohne sie laesst sie sich nicht
	// zuordnen und ist Muell.
	LabelRetainedInstance = "osb.io/retained-instance"
	// LabelRetainedAt sagt, seit wann sie liegt.
	LabelRetainedAt = "osb.io/retained-at"
)

// retainedAtLayout ist RFC3339 ohne Doppelpunkte: ein Label-Wert darf nur
// Buchstaben, Ziffern, Punkt, Bindestrich und Unterstrich enthalten.
const retainedAtLayout = "2006-01-02T15-04-05Z"

// planRetains meldet, ob der Plan der Instanz seine Ressourcen behaelt.
//
// Ohne Datensatz ist der Plan unbekannt, und dann wird geloescht:
// zurueckzuhalten waere eine Annahme ueber einen Plan, den der Broker nicht
// kennt, und jede verlorene Buchfuehrung wuerde stillschweigend Ressourcen
// anhaeufen.
func (e *Engine) planRetains(sd *ServiceDefinition, rec *InstanceRecord) bool {
	if rec == nil || rec.PlanID == "" {
		return false
	}
	plan, err := sd.PlanByID(rec.PlanID)
	if err != nil {
		return false
	}
	return plan.RetainOnDeprovision
}

// markRetained beschriftet die stehengelassenen Objekte.
//
// Ein Fehler beim Beschriften darf das Deprovision nicht scheitern lassen: die
// Instanz ist aufgegeben, und ein fehlendes Label macht die Ressource
// schwerer auffindbar, nicht kaputt. Er wird deshalb zurueckgegeben und vom
// Aufrufer der Meldung angehaengt.
func (e *Engine) markRetained(ctx context.Context, namespace, instanceID string, refs []ObjectRef) error {
	stamp := time.Now().UTC().Format(retainedAtLayout)
	labels := map[string]string{
		LabelRetainedInstance: instanceID,
		LabelRetainedAt:       stamp,
	}
	var failed []string
	for _, ref := range refs {
		ns := ref.Namespace
		if ns == "" {
			ns = namespace
		}
		if err := e.op.LabelCR(ctx, ref.APIVersion, ref.Kind, ns, ref.Name, labels); err != nil {
			failed = append(failed, fmt.Sprintf("%s %q: %v", ref.Kind, ref.Name, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("retained resources could not be labelled: %v", failed)
	}
	return nil
}
