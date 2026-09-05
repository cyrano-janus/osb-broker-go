package definition

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pläne beschrieben Größen, aber nichts erzwang sie: stand ein Schlüssel in
// `allowedParameters`, durfte ein Konsument jeden Wert dafür setzen. Ein Plan
// „small" mit 1Gi liess sich mit `-c '{"storageSize":"10Ti"}'` provisionieren,
// und der Betreiber sah es erst an der Rechnung.
//
// `parameterLimits` schliesst das. Die Grenzen gelten fuer Provision und
// Update gleichermassen und werden zusaetzlich als OSB-Plan-Schema im Katalog
// veroeffentlicht - die Plattform kann dann ablehnen, bevor der Broker
// ueberhaupt gefragt wird.

// planWithLimits baut einen Plan mit Allowlist und Grenzen.
func planWithLimits(allowed []string, limits map[string]ParameterLimit) *Plan {
	return &Plan{
		ID:                "p1",
		Name:              "small",
		AllowedParameters: allowed,
		ParameterLimits:   limits,
	}
}

func TestLimits_WertInnerhalbDerGrenzeGehtDurch(t *testing.T) {
	p := planWithLimits([]string{"storageSize"},
		map[string]ParameterLimit{"storageSize": {Max: "10Gi"}})

	assert.NoError(t, ValidatePlanParams(p, map[string]interface{}{"storageSize": "5Gi"}))
	assert.NoError(t, ValidatePlanParams(p, map[string]interface{}{"storageSize": "10Gi"}),
		"die Grenze selbst ist erlaubt, nicht ausgeschlossen")
}

func TestLimits_WertUeberDerGrenzeWirdAbgelehnt(t *testing.T) {
	p := planWithLimits([]string{"storageSize"},
		map[string]ParameterLimit{"storageSize": {Max: "10Gi"}})

	err := ValidatePlanParams(p, map[string]interface{}{"storageSize": "20Gi"})

	require.ErrorIs(t, err, ErrParameterNotAllowed)
	assert.Contains(t, err.Error(), "storageSize")
	assert.Contains(t, err.Error(), "10Gi", "die Meldung muss die Grenze nennen, nicht nur dass es zu viel ist")
}

// Ohne Gegenprobe waere nicht belegt, dass die Grenze ueberhaupt etwas tut:
// derselbe Wert muss ohne Grenze durchgehen.
func TestLimits_OhneGrenzeGehtDerselbeWertDurch(t *testing.T) {
	ohne := planWithLimits([]string{"storageSize"}, nil)

	assert.NoError(t, ValidatePlanParams(ohne, map[string]interface{}{"storageSize": "20Gi"}))
}

func TestLimits_UntergrenzeGiltAuch(t *testing.T) {
	p := planWithLimits([]string{"instances"},
		map[string]ParameterLimit{"instances": {Min: "1", Max: "3"}})

	assert.NoError(t, ValidatePlanParams(p, map[string]interface{}{"instances": 2}))
	assert.Error(t, ValidatePlanParams(p, map[string]interface{}{"instances": 0}))
	assert.Error(t, ValidatePlanParams(p, map[string]interface{}{"instances": 4}))
}

// Zahlen kommen aus JSON als float64. Ohne Normalisierung waere 2 nicht mit
// "2" vergleichbar, und die Grenze griffe je nach Herkunft des Wertes anders.
func TestLimits_ZahlenAusJSONWerdenVerglichen(t *testing.T) {
	p := planWithLimits([]string{"instances"},
		map[string]ParameterLimit{"instances": {Max: "3"}})

	for _, v := range []interface{}{3, int64(3), 3.0, "3"} {
		assert.NoError(t, ValidatePlanParams(p, map[string]interface{}{"instances": v}),
			"%T %v muss die Grenze treffen, nicht ueberschreiten", v, v)
	}
	for _, v := range []interface{}{4, int64(4), 4.0, "4"} {
		assert.Error(t, ValidatePlanParams(p, map[string]interface{}{"instances": v}),
			"%T %v liegt ueber der Grenze", v, v)
	}
}

func TestLimits_AufzaehlungBegrenztDieWerte(t *testing.T) {
	p := planWithLimits([]string{"tier"},
		map[string]ParameterLimit{"tier": {OneOf: []string{"bronze", "silber"}}})

	assert.NoError(t, ValidatePlanParams(p, map[string]interface{}{"tier": "silber"}))

	err := ValidatePlanParams(p, map[string]interface{}{"tier": "gold"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bronze")
	assert.Contains(t, err.Error(), "silber")
}

// Ein Wert, der sich nicht vergleichen laesst, darf nicht stillschweigend
// durchgehen - sonst umgeht "10Gi ist keine Zahl" jede Grenze.
func TestLimits_UnvergleichbarerWertWirdAbgelehnt(t *testing.T) {
	p := planWithLimits([]string{"storageSize"},
		map[string]ParameterLimit{"storageSize": {Max: "10Gi"}})

	err := ValidatePlanParams(p, map[string]interface{}{"storageSize": "viel"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storageSize")
}

// Eine Grenze auf einem Schluessel, den niemand setzen darf, ist ein
// Konfigurationsfehler: sie kann nie greifen und taeuscht Schutz vor.
func TestLimits_GrenzeOhnePassendenAllowedParameterIstEinFehler(t *testing.T) {
	yaml := strings.Replace(validYAML,
		"        allowedParameters: [replicas, storageSize, instances]",
		"        allowedParameters: [replicas]\n        parameterLimits:\n          storageSize: {max: 10Gi}", 1)

	_, err := Parse([]byte(yaml))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storageSize")
	assert.Contains(t, err.Error(), "allowedParameters")
}

// Eine Grenze, die sich selbst nicht lesen laesst, faellt beim Laden auf und
// nicht beim ersten Provision.
func TestLimits_UnlesbareGrenzeFaelltBeimLadenAuf(t *testing.T) {
	yaml := strings.Replace(validYAML,
		"        allowedParameters: [replicas, storageSize, instances]",
		"        allowedParameters: [storageSize]\n        parameterLimits:\n          storageSize: {max: sehrviel}", 1)

	_, err := Parse([]byte(yaml))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sehrviel")
}
