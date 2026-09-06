package definition

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maintenance_info ist der Weg, auf dem Cloud Foundry einen neuen Stand eines
// Plans ANBIETET, statt ihn zu verhaengen: die Version steht im Katalog und an
// der Instanz, `cf services` zeigt die Abweichung als `upgrade available`, und
// `cf upgrade-service` loest den Vorgang aus. Der Besitzer entscheidet.
//
// Ohne das Feld aendert nur der Betreiber, und der Besitzer erfaehrt es nicht.

func withMaintenanceInfo(t *testing.T, yamlFragment string) (*ServiceDefinition, error) {
	t.Helper()
	return Parse([]byte(strings.Replace(validYAML,
		"      - id: plan-small-0000-0000-000000000001\n        name: small\n",
		"      - id: plan-small-0000-0000-000000000001\n        name: small\n"+yamlFragment, 1)))
}

// OSB 2.17: "This MUST be a string conforming to a semantic version 2.0."
// Die Pruefung gehoert an das Laden, nicht an den ersten Request: eine
// unbrauchbare Version faellt sonst erst auf, wenn eine Plattform sie liest.
func TestMaintenanceInfo_UngueltigeVersionFaelltBeimLadenAuf(t *testing.T) {
	_, err := withMaintenanceInfo(t, "        maintenanceInfo:\n          version: \"eins\"\n")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenanceInfo",
		"die Meldung muss sagen, welches Feld unbrauchbar ist: %v", err)
}

func TestMaintenanceInfo_VersionOhneVersionIstUnbrauchbar(t *testing.T) {
	_, err := withMaintenanceInfo(t, "        maintenanceInfo:\n          description: \"nur Text\"\n")

	require.Error(t, err, "ein Block ohne version sagt nichts aus")
}

func TestMaintenanceInfo_SemverGehtDurch(t *testing.T) {
	for _, v := range []string{"1.0.0", "0.1.0", "2.3.4-rc.1", "1.2.3+build.5"} {
		_, err := withMaintenanceInfo(t, "        maintenanceInfo:\n          version: \""+v+"\"\n")
		assert.NoError(t, err, "%q ist gueltiges Semver 2.0", v)
	}
}

func TestMaintenanceInfo_StehtImKatalog(t *testing.T) {
	sd, err := withMaintenanceInfo(t,
		"        maintenanceInfo:\n          version: \"1.4.0\"\n          description: \"PostgreSQL 18.6\"\n")
	require.NoError(t, err)

	plan := plansJSON(t, sd)[0]
	mi, ok := plan["maintenance_info"].(map[string]interface{})
	require.True(t, ok, "der Plan traegt kein maintenance_info: %v", plan)
	assert.Equal(t, "1.4.0", mi["version"])
	assert.Equal(t, "PostgreSQL 18.6", mi["description"])
}

// Anders als `free` wird das Feld weggelassen, wenn es niemand erklaert hat.
// Ein leerer Block waere eine Aussage - er hiesse "es gibt einen Stand", und
// eine Plattform verglichene ihn gegen den der Instanz.
func TestMaintenanceInfo_OhneAngabeFehltDasFeldGanz(t *testing.T) {
	plan := plansJSON(t, testDefinition(t))[0]

	_, vorhanden := plan["maintenance_info"]
	assert.False(t, vorhanden, "ohne Angabe darf das Feld nicht im Katalog stehen")
}

func TestMaintenanceInfo_VersionDesPlansIstAuffindbar(t *testing.T) {
	sd, err := withMaintenanceInfo(t, "        maintenanceInfo:\n          version: \"2.0.0\"\n")
	require.NoError(t, err)

	small, large := sd.Spec.Offering.Plans[0].ID, sd.Spec.Offering.Plans[1].ID
	assert.Equal(t, "2.0.0", MaintenanceVersion(sd, small))
	assert.Equal(t, "", MaintenanceVersion(sd, large), "ein Plan ohne Angabe hat keine Version")
	assert.Equal(t, "", MaintenanceVersion(sd, "gibt-es-nicht"))
}

// Der Katalog geht unveraendert ueber die Leitung - ein Feld, das der Go-Typ
// traegt und `omitempty` verschluckt, existiert fuer die Plattform nicht.
func TestMaintenanceInfo_UeberlebtDieSerialisierung(t *testing.T) {
	sd, err := withMaintenanceInfo(t, "        maintenanceInfo:\n          version: \"1.0.0\"\n")
	require.NoError(t, err)

	raw, err := json.Marshal(NewEngine(nil, sd).Catalog()[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"maintenance_info":{"version":"1.0.0"}`)
}
