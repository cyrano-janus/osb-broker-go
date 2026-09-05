package checks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Beispiel-IDs dieses Brokers. Sie stehen hier und nicht im Produktivcode:
// ein allgemeines Konformitaetswerkzeug darf keine Service-IDs kennen.
const cnpgServiceID = "f48a9e21-cnpg-0000-0000-000000000001"
const smallPlanID = "plan-small-0000-0000-000000000001"

// Die Auswahl des zu pruefenden Service entscheidet, welchen Codepfad der
// gesamte Lifecycle-Audit anfasst. Sie nahm frueher den ersten Katalogeintrag,
// und weil internal/store seine Demo-Angebote voranstellt, war das immer der
// Fallback-Pfad - die Engine wurde nie geprueft. Diese Tests halten fest, dass
// die Wahl auf eine ServiceDefinition faellt und dass eine Vorgabe, die nicht
// aufgeht, laut scheitert statt still etwas anderes zu pruefen.

// svc baut einen Katalogeintrag mit den angegebenen Plan-IDs.
func svc(id string, planIDs ...string) catalogService {
	s := catalogService{ID: id, Name: id}
	for _, p := range planIDs {
		s.Plans = append(s.Plans, struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}{ID: p, Name: p})
	}
	return s
}

// pick ruft pickService und liefert Auswahl samt Fehlerzahl.
func pick(cfg Config, svcs []catalogService) (string, string, int) {
	c := &client{cfg: cfg, rep: &Report{}}
	sid, pid := c.pickService(svcs)
	return sid, pid, c.rep.Failures()
}

func TestPickService_UeberspringtDieDemoAngebote(t *testing.T) {
	// Genau die Reihenfolge, die der Broker ausliefert: erst service-1 und
	// service-2 aus internal/store, dann die Definitionen.
	catalog := []catalogService{
		svc("service-1", "plan-free", "plan-premium"),
		svc("service-2", "plan-small", "plan-large"),
		svc(cnpgServiceID, smallPlanID),
	}

	sid, pid, f := pick(Config{}, catalog)

	assert.Equal(t, cnpgServiceID, sid, "die erste ServiceDefinition muss gewinnen, nicht der erste Katalogeintrag")
	assert.Equal(t, smallPlanID, pid)
	assert.Zero(t, f)
}

func TestPickService_NimmtDasDemoAngebotNurWennEsNichtsAnderesGibt(t *testing.T) {
	// Der Broker ohne DEFINITIONS_DIR. Ein Skip waere hier falsch: der
	// Fallback-Pfad ist dann das Einzige, was es zu pruefen gibt.
	sid, pid, f := pick(Config{}, []catalogService{svc("service-1", "plan-free")})

	assert.Equal(t, "service-1", sid)
	assert.Equal(t, "plan-free", pid)
	assert.Zero(t, f)
}

func TestPickService_VorgabeSchlaegtAutomatik(t *testing.T) {
	catalog := []catalogService{
		svc("service-1", "plan-free"),
		svc(cnpgServiceID, smallPlanID),
		svc("rabbit", "dev", "prod"),
	}

	sid, pid, f := pick(Config{ServiceID: "rabbit", PlanID: "prod"}, catalog)

	assert.Equal(t, "rabbit", sid)
	assert.Equal(t, "prod", pid)
	assert.Zero(t, f)
}

func TestPickService_VorgabeOhnePlanNimmtDenErsten(t *testing.T) {
	sid, pid, f := pick(Config{ServiceID: "rabbit"}, []catalogService{svc("rabbit", "dev", "prod")})

	assert.Equal(t, "rabbit", sid)
	assert.Equal(t, "dev", pid)
	assert.Zero(t, f)
}

func TestPickService_UnbekannterServiceScheitertLaut(t *testing.T) {
	// Ein stiller Rueckfall waere hier das Schlimmste: die CI meldete gruen
	// fuer einen Service, den niemand pruefen wollte.
	sid, _, f := pick(Config{ServiceID: "gibt-es-nicht"}, []catalogService{svc(cnpgServiceID, smallPlanID)})

	assert.Empty(t, sid)
	assert.Equal(t, 1, f)
}

func TestPickService_PlanDerNichtDazugehoertScheitertLaut(t *testing.T) {
	sid, _, f := pick(Config{ServiceID: "rabbit", PlanID: smallPlanID}, []catalogService{svc("rabbit", "dev")})

	assert.Empty(t, sid)
	assert.Equal(t, 1, f)
}

func TestPickService_ServiceOhnePlaeneWirdUebersprungen(t *testing.T) {
	// Frueher lief das in ein Plans[0] ohne Laengenpruefung.
	catalog := []catalogService{
		svc("ohne-plaene"),
		svc(cnpgServiceID, smallPlanID),
	}

	sid, pid, f := pick(Config{}, catalog)

	assert.Equal(t, cnpgServiceID, sid)
	assert.Equal(t, smallPlanID, pid)
	assert.Zero(t, f)
}

func TestPickService_LeererKatalogWaehltNichts(t *testing.T) {
	sid, pid, f := pick(Config{}, nil)

	assert.Empty(t, sid)
	assert.Empty(t, pid)
	assert.Zero(t, f, "ein leerer Katalog ist kein Auswahlfehler - checkCatalogStructure meldet ihn")
}
