package checker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// checkServiceBindingSpec meldet ueber die paketweite failures-Variable; die
// Tests setzen sie zurueck und lesen sie aus.
func runSpecCheck(body string) int {
	failures = 0
	checkServiceBindingSpec([]byte(body))
	return failures
}

func TestSpecCheck_OhneTypIstKeinFehler(t *testing.T) {
	// Nicht jeder Dienst ist spec-konform, und das ist erlaubt. Wuerde das
	// als Fehler zaehlen, waere jeder bestehende Broker ueber Nacht rot.
	assert.Zero(t, runSpecCheck(`{"credentials":{"username":"app","password":"x"}}`))
}

func TestSpecCheck_VollstaendigerTypIstGruen(t *testing.T) {
	assert.Zero(t, runSpecCheck(`{"credentials":{
		"type":"rabbitmq","host":"h","port":"5672","username":"u","password":"p"}}`))
}

func TestSpecCheck_FehlendeWellKnownKeysSindEinFehler(t *testing.T) {
	// Ein Konsument, der sich auf type=postgresql verlaesst, erwartet genau
	// diese Felder - fehlen sie, ist die Typangabe irrefuehrend.
	assert.Equal(t, 1, runSpecCheck(`{"credentials":{"type":"postgresql","host":"h"}}`))
}

func TestSpecCheck_TypMussEinNichtLeererStringSein(t *testing.T) {
	assert.Equal(t, 1, runSpecCheck(`{"credentials":{"type":123}}`))
	assert.Equal(t, 1, runSpecCheck(`{"credentials":{"type":""}}`))
}

func TestSpecCheck_TypMussKleingeschriebenSein(t *testing.T) {
	// Der Wert landet als Secret-Type servicebinding.io/<typ> und muss dort
	// ein gueltiger Bezeichner sein.
	assert.Equal(t, 1, runSpecCheck(`{"credentials":{"type":"PostgreSQL"}}`))
}

func TestSpecCheck_UnbekannterTypWirdNichtBewertet(t *testing.T) {
	// Die Spezifikation kennt eine offene Typmenge; ein eigener Typ ist kein
	// Verstoss, nur nicht auf bekannte Felder pruefbar.
	assert.Zero(t, runSpecCheck(`{"credentials":{"type":"eigener-dienst"}}`))
}

func TestSpecCheck_KaputtesJSONIstEinFehler(t *testing.T) {
	assert.Equal(t, 1, runSpecCheck(`{kein json`))
}
