package checks

import (
	"fmt"
	"strings"
)

// Der Katalog ist das Einzige, was ein Marktplatz vom Broker sieht, bevor er
// ihn benutzt - und die einzige Stelle, an der ein Broker etwas behaupten
// kann, ohne dass es jemand nachprueft. Eine Zusage, die das Verhalten nicht
// haelt, scheitert erst beim Anwender, auf einer Plattform, die niemand
// nachstellt.
//
// Hier wird jede Zusage gegen die Route gehalten, die sie einloesen muss.

// checkCatalogPromises prueft die Zusagen, die sich nur durch Handeln pruefen
// lassen. Die Abrufbarkeit steckt im Fetch-Audit, weil sie dort ohnehin
// aufgerufen wird; hier steht der Planwechsel.
func (c *client) checkCatalogPromises(instanceID, serviceID, planID string, svcs []catalogService) {
	const check = "catalog-promises"

	svc := serviceByID(svcs, serviceID)
	if svc == nil {
		c.fail(check, "der geprueft Service %q steht nicht im Katalog", serviceID)
		return
	}

	other := otherPlan(svcs, serviceID, planID)
	if other == "" {
		c.skip(check, "das Angebot hat nur einen Plan - ein Planwechsel ist nicht pruefbar")
		return
	}

	status, body := c.do("PATCH", "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    other,
	})

	// Beide Richtungen sind Fehler. Eine fehlende Zusage kostet eine
	// Faehigkeit; eine falsche kostet den Anwender eine Instanz, die still
	// auf einem Plan steht, den er nicht bestellt hat.
	switch {
	case svc.PlanUpdateable && status != 200 && status != 202:
		c.fail(check, "plan_updateable ist zugesagt, der Wechsel auf %q ergibt aber %d: %s",
			other, status, truncate(body))
	case svc.PlanUpdateable:
		c.pass(check, "plan_updateable zugesagt und der Wechsel auf %q wird vollzogen (%d)", other, status)
		// Zurueck auf den urspruenglichen Plan: die folgenden Pruefungen und
		// das Aufraeumen erwarten die Instanz so, wie sie angelegt wurde.
		c.do("PATCH", "/v2/service_instances/"+instanceID, map[string]interface{}{
			"service_id": serviceID, "plan_id": planID,
		})

	// Nicht zugesagt. Ein vollzogener Wechsel ist der teure Fall: er aendert
	// nicht nur Groessen, sondern kann eine Instanz auf einen Plan mit
	// anderer Loeschsemantik schieben, ohne dass es jemand angefordert hat.
	case status == 200 || status == 202:
		c.fail(check, "plan_updateable ist nicht zugesagt, der Wechsel auf %q wird aber vollzogen (%d)",
			other, status)

	// Abgelehnt - aber der Code muss stimmen. OSB 2.17: 422 "MUST be returned
	// if the requested change is not supported". Ein 400 sagt dem Anwender
	// "deine Anfrage ist kaputt" statt "das kann dieser Service nicht".
	case status != 422:
		c.fail(check, "plan_updateable ist nicht zugesagt und der Wechsel wird mit %d abgelehnt - OSB verlangt 422: %s",
			status, truncate(body))
	default:
		c.pass(check, "plan_updateable nicht zugesagt und der Wechsel wird mit 422 abgelehnt")
	}
}

// checkCatalogDisplay prueft, was ein Marktplatz anzeigt.
//
// Nichts davon verlangt OSB. Ein Katalog ohne Anzeigeblock ist konform - und
// im Marktplatz eine Kachel mit dem technischen Namen und sonst nichts.
// Deshalb ist ein fehlender Block ein Hinweis (uebersprungen), ein
// missgestalteter dagegen ein Fehler: er bricht die Anzeige, statt sie
// auszulassen.
func (c *client) checkCatalogDisplay(svcs []catalogService) {
	const check = "catalog-display"
	if len(svcs) == 0 {
		return
	}

	var probleme, fehlend []string
	for _, s := range svcs {
		if s.Metadata != nil {
			if _, ok := s.Metadata.(map[string]interface{}); !ok {
				probleme = append(probleme, fmt.Sprintf("service %q: metadata ist %T, kein Block", s.Name, s.Metadata))
			}
		} else {
			fehlend = append(fehlend, fmt.Sprintf("service %q ohne metadata", s.Name))
		}
		for _, p := range s.Plans {
			if p.Metadata != nil {
				if _, ok := p.Metadata.(map[string]interface{}); !ok {
					probleme = append(probleme, fmt.Sprintf("plan %q: metadata ist %T, kein Block", p.Name, p.Metadata))
				}
			}
			if p.MaximumPollingDuration != nil && *p.MaximumPollingDuration <= 0 {
				probleme = append(probleme, fmt.Sprintf("plan %q: maximum_polling_duration ist %d - eine Plattform wuerde sofort aufgeben",
					p.Name, *p.MaximumPollingDuration))
			}
			if p.Free == nil {
				fehlend = append(fehlend, fmt.Sprintf("plan %q sagt nicht, ob er kostenlos ist (OSB nimmt dann: ja)", p.Name))
			}
		}
	}

	if len(probleme) > 0 {
		c.fail(check, "%s", strings.Join(probleme, "; "))
		return
	}
	if len(fehlend) > 0 {
		c.skip(check, "konform, aber im Marktplatz unvollstaendig: %s", strings.Join(fehlend, "; "))
		return
	}
	c.pass(check, "jeder Service traegt einen Anzeigeblock, jeder Plan sagt free und seine Pollfrist")
}
