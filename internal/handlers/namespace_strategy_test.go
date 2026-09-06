package handlers

import (
	"net/http"
	"testing"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Ziel-Namespace kommt aus der Konfiguration, nicht aus einer Annahme
// ueber die Plattform (ADR 0010).
//
// Vorher leitete der Broker ihn aus der Space-GUID ab - das ging auf, solange
// die Plattform je Space einen Namespace anlegte. Cloud Foundry tut das nicht.

func kontext(orgGUID, orgName, spaceGUID, spaceName string) broker.Context {
	return broker.Context{
		Platform:         "cloudfoundry",
		OrganizationGUID: orgGUID,
		OrganizationName: orgName,
		SpaceGUID:        spaceGUID,
		SpaceName:        spaceName,
	}
}

// Die Vorgabe traegt auf jeder Plattform: ein fester Name, kein neues Recht,
// keine Annahme darueber, was die Plattform anlegt.
func TestNamespace_VorgabeIstEinFesterName(t *testing.T) {
	s, err := NewNamespaceStrategy("", false)
	require.NoError(t, err)

	got, err := s.Resolve(kontext("org-1", "Team A", "space-1", "Produktion"))
	require.NoError(t, err)
	assert.Equal(t, DefaultInstanceNamespace, got)
	assert.Equal(t, "osb-instances", got, "die Vorgabe steht im ADR und gehoert nicht stillschweigend geaendert")
}

func TestNamespace_TemplateWirdAusgewertet(t *testing.T) {
	s, err := NewNamespaceStrategy("osb-{{ .orgGUID }}", false)
	require.NoError(t, err)

	got, err := s.Resolve(kontext("abc-123", "Team A", "space-1", "Produktion"))
	require.NoError(t, err)
	assert.Equal(t, "osb-abc-123", got)
}

func TestNamespace_AlleFelderDesKontextsStehenBereit(t *testing.T) {
	for platzhalter, erwartet := range map[string]string{
		"{{ .orgGUID }}":   "org-1",
		"{{ .spaceGUID }}": "space-1",
		"{{ .platform }}":  "cloudfoundry",
	} {
		s, err := NewNamespaceStrategy(platzhalter, false)
		require.NoError(t, err, platzhalter)
		got, err := s.Resolve(kontext("org-1", "Team A", "space-1", "Produktion"))
		require.NoError(t, err, platzhalter)
		assert.Equal(t, erwartet, got, platzhalter)
	}
}

// Ein Tippfehler im Template faellt beim LADEN auf, nicht beim ersten
// Provision. Sonst startet ein Broker, der bei der ersten Bestellung scheitert.
func TestNamespace_UnbekanntesFeldFaelltBeimLadenAuf(t *testing.T) {
	_, err := NewNamespaceStrategy("osb-{{ .spaceNam }}", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "spaceNam", "die Meldung muss den Tippfehler nennen: %v", err)
}

func TestNamespace_KaputtesTemplateFaelltBeimLadenAuf(t *testing.T) {
	_, err := NewNamespaceStrategy("osb-{{ .orgGUID", false)
	require.Error(t, err)
}

// **Nicht zurechtbiegen.** Ein still verstuemmelter Name fuehrt zwei Mandanten
// in denselben Namespace, und das faellt niemandem auf.
func TestNamespace_UngueltigesErgebnisWirdAbgelehnt(t *testing.T) {
	s, err := NewNamespaceStrategy("{{ .spaceName }}", false)
	require.NoError(t, err, "der Betreiber darf das konfigurieren - es faellt erst am echten Wert auf")

	_, err = s.Resolve(kontext("org-1", "Team A", "space-1", "Team Grün (Q3)"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Team Grün (Q3)",
		"die Meldung muss den unbrauchbaren Wert zeigen, sonst sucht der Betreiber blind: %v", err)
}

func TestNamespace_LeeresErgebnisWirdAbgelehnt(t *testing.T) {
	s, err := NewNamespaceStrategy("{{ .spaceName }}", false)
	require.NoError(t, err)

	_, err = s.Resolve(kontext("org-1", "Team A", "space-1", ""))
	require.Error(t, err, "ein leerer Namespace waere der des Brokers - dort gehoeren keine Instanzen hin")
}

// Die Zusage aus dem ADR: ohne ausdrueckliche Erlaubnis legt der Broker nichts
// an. Was er anlegte, raeumte nie jemand ab.
func TestNamespace_AnlegenIstNichtDieVorgabe(t *testing.T) {
	s, err := NewNamespaceStrategy("", false)
	require.NoError(t, err)
	assert.False(t, s.Create)

	s, err = NewNamespaceStrategy("", true)
	require.NoError(t, err)
	assert.True(t, s.Create, "wer es ausdruecklich erlaubt, bekommt es")
}

// Die Gegenprobe zur Testroute: ohne Konfiguration landet eine Instanz
// wirklich im ausgelieferten Vorgabe-Namespace, nicht in "default".
func TestNamespace_VorgabeGiltAuchImProvision(t *testing.T) {
	withNamespaceTemplate(t, "")
	router, oc := newDefinitionRouter(t)
	const instanceID = "ns-vorgabe-1"

	w := provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "def-svc-0001", "plan_id": "def-plan-free",
		"organization_guid": "org-1", "space_guid": "space-egal",
	})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	_, err := crIn(t, oc, DefaultInstanceNamespace, instanceID)
	require.NoError(t, err, "die Instanz gehoert in den Vorgabe-Namespace")

	_, err = crIn(t, oc, "space-egal", instanceID)
	assert.Error(t, err, "und ausdruecklich NICHT mehr in den Space")
}
