package checks

import (
	"encoding/json"
	"regexp"
)

// `maintenance_info` ist der einzige Weg, auf dem eine Plattform einem
// Anwender sagen kann, dass fuer seine Instanz ein neuer Stand bereitliegt.
// Cloud Foundry haelt dafuer den Stand der Instanz gegen den des Plans und
// zeigt die Differenz als `upgrade available`.
//
// Damit das traegt, muessen drei Dinge stimmen - und keines davon prueft die
// Spezifikation fuer sich allein:
//
//   - die Version ist Semantic Versioning 2.0, sonst vergleicht die Plattform
//     Zeichenketten, die keine Ordnung haben,
//   - ein Request mit veraltetem Stand wird mit 422 abgelehnt, statt eine
//     Instanz auf gut Glueck anzulegen,
//   - GET auf die Instanz meldet ihren Stand, sonst hat die Plattform nichts
//     zu vergleichen.
//
// Nennt kein Plan einen Stand, ist das konform - dann wird uebersprungen. Ein
// Broker, der die Faehigkeit nicht anbietet, ist nicht fehlerhaft.

var semver2 = regexp.MustCompile(
	`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

func (c *client) checkMaintenanceInfo(instanceID, serviceID, planID string, svcs []catalogService) {
	const check = "maintenance-info"

	svc := serviceByID(svcs, serviceID)
	if svc == nil {
		return
	}
	var mi *maintenanceInfo
	for _, p := range svc.Plans {
		if p.ID == planID {
			mi = p.MaintenanceInfo
		}
	}
	if mi == nil {
		c.skip(check, "plan %q nennt keinen Stand - ohne Zusage nichts zu pruefen", planID)
		return
	}

	// OSB 2.17: "This MUST be a string conforming to a semantic version 2.0."
	if !semver2.MatchString(mi.Version) {
		c.fail(check, "maintenance_info.version %q ist kein Semantic Versioning 2.0", mi.Version)
		return
	}

	// Der Stand der Instanz muss abrufbar sein, sonst kann die Plattform ihn
	// gegen nichts halten - und meldete nie ein Upgrade.
	status, body := c.do("GET", "/v2/service_instances/"+instanceID, nil)
	if status != 200 {
		c.fail(check, "GET auf die Instanz ergibt %d, ihr Stand ist damit unabfragbar: %s",
			status, truncate(body))
		return
	}
	var instanz struct {
		MaintenanceInfo *maintenanceInfo `json:"maintenance_info"`
	}
	if err := json.Unmarshal(body, &instanz); err != nil {
		c.fail(check, "GET auf die Instanz liefert kein gueltiges JSON: %s", truncate(body))
		return
	}
	if instanz.MaintenanceInfo == nil || instanz.MaintenanceInfo.Version == "" {
		c.fail(check, "der Plan nennt Stand %q, die Instanz meldet keinen - die Plattform "+
			"sieht damit nie ein Upgrade", mi.Version)
		return
	}
	gemeldet := instanz.MaintenanceInfo.Version

	// Ein veralteter Stand im Request heisst: die Katalogkopie der Plattform
	// ist alt. Wer das ausfuehrt, aendert eine Instanz auf einen Stand, den
	// niemand mehr bestellt hat.
	veraltet := "0.0.1-stale"
	status, body = c.do("PATCH", "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id":       serviceID,
		"maintenance_info": map[string]interface{}{"version": veraltet},
	})
	switch {
	case status == 200 || status == 202:
		c.fail(check, "ein Update mit dem veralteten Stand %q wird vollzogen (%d) - OSB verlangt "+
			"422 MaintenanceInfoConflict", veraltet, status)
	case status != 422:
		c.fail(check, "ein Update mit dem veralteten Stand %q wird mit %d abgelehnt - OSB verlangt "+
			"422: %s", veraltet, status, truncate(body))
	default:
		c.pass(check, "Stand %q im Katalog, %q an der Instanz, veralteter Stand mit 422 abgelehnt",
			mi.Version, gemeldet)
	}
}
