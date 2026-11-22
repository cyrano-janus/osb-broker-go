package definition

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Eine Postgres-Definition, die kein Image und keine Major nennt, bekommt
// die Vorgabe des Operators - und die wandert mit jedem Operator-Update,
// ohne dass der Katalog es sagt. Genau so lief cnpg-postgresql bis zum
// 2026-11-22: der Katalog versprach "PostgreSQL", die Instanz bekam 18.4,
// und beim naechsten Operator-Stand haette sie etwas anderes bekommen.
//
// Dieser Waechter verlangt von jeder Definition, die einen CloudNativePG-
// Cluster anlegt, eine ausdrueckliche Major - entweder als Parameter ueber
// den ClusterImageCatalog (cnpg-postgresql) oder als fest genanntes Image
// mit Major (cnpg-pgvector, solange die Erweiterung die Major vorschreibt).
func TestPostgres_JedeDefinitionNenntDieMajor(t *testing.T) {
	defs, err := LoadFromDir("../../definitions")
	require.NoError(t, err)

	pinned := regexp.MustCompile(`imageName: ghcr\.io/cloudnative-pg/postgresql:\d+(\.\d+)?-`)
	catalog := "major: {{ .plan.majorVersion }}"

	gesehen := 0
	for _, d := range defs {
		tpl := d.Spec.Provision.Template
		if !strings.Contains(tpl, "postgresql.cnpg.io") || !strings.Contains(tpl, "kind: Cluster") {
			continue
		}
		gesehen++
		t.Run(d.Metadata.Name, func(t *testing.T) {
			ueberKatalog := strings.Contains(tpl, catalog)
			festesImage := pinned.MatchString(tpl)
			require.True(t, ueberKatalog || festesImage,
				"das Template nennt weder `%s` noch ein imageName mit Major - die Version kaeme vom Operator", catalog)
			require.False(t, ueberKatalog && festesImage,
				"Katalog UND festes Image: der Operator naehme das Image, der Parameter waere Schein")
			if !ueberKatalog {
				return
			}

			// Der Parameter traegt die Major, also muss jeder Plan ihn
			// vorgeben, freigeben und begrenzen - und zwar auf dieselbe
			// Liste. Ein Plan mit anderem Fenster waere ein zweites Angebot.
			var fenster []string
			for _, p := range d.Spec.Offering.Plans {
				vorgabe, ok := p.Params["majorVersion"].(string)
				assert.True(t, ok, "plan %q: params.majorVersion fehlt oder ist keine Zeichenkette", p.Name)
				assert.Contains(t, p.AllowedParameters, "majorVersion", "plan %q: majorVersion ist nicht freigegeben", p.Name)
				limit := p.ParameterLimits["majorVersion"]
				require.NotEmpty(t, limit.OneOf, "plan %q: ohne oneOf waere jede Major erlaubt, auch eine ohne Katalogeintrag", p.Name)
				assert.Contains(t, limit.OneOf, vorgabe, "plan %q: die Vorgabe %q liegt ausserhalb des Fensters", p.Name, vorgabe)
				if fenster == nil {
					fenster = limit.OneOf
				} else {
					assert.Equal(t, fenster, limit.OneOf, "plan %q: anderes Versionsfenster als die Plaene davor", p.Name)
				}
			}
		})
	}
	require.Equal(t, 2, gesehen, "zwei Postgres-Definitionen erwartet - wer eine dritte baut, traegt sie hier ein")
}

// Cloud Foundry reicht `-c '{"majorVersion":19}'` als JSON-Zahl durch, die
// YAML-Vorgabe ist eine Zeichenkette. Beide muessen dieselbe Major ergeben,
// und das Template muss daraus ein `major: 19` rendern, das der Operator als
// Ganzzahl liest. Die Gegenprobe: eine Major ausserhalb des Fensters ist 400,
// nicht ein Provision, das am fehlenden Katalogeintrag haengen bleibt.
func TestPostgres_MajorAlsZahlOderZeichenkette(t *testing.T) {
	defs, err := LoadFromDir("../../definitions")
	require.NoError(t, err)
	var sd *ServiceDefinition
	for _, d := range defs {
		if d.Metadata.Name == "cnpg-postgresql" {
			sd = d
		}
	}
	require.NotNil(t, sd)
	plan := &sd.Spec.Offering.Plans[0]

	for _, wert := range []interface{}{"19", float64(19), 19} {
		params := map[string]interface{}{"majorVersion": wert}
		require.NoError(t, ValidatePlanParams(plan, params), "wert %v (%T)", wert, wert)
		out, err := RenderProvision(sd, "inst-1", plan.Params, params)
		require.NoError(t, err)
		assert.Contains(t, out, "major: 19\n", "wert %v (%T)", wert, wert)
	}

	// Ohne Benutzerangabe gilt die Vorgabe des Plans.
	out, err := RenderProvision(sd, "inst-1", plan.Params, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "major: 18\n")

	for _, falsch := range []interface{}{"16", float64(16), "20", float64(19.5), "neunzehn"} {
		err := ValidatePlanParams(plan, map[string]interface{}{"majorVersion": falsch})
		assert.ErrorIs(t, err, ErrParameterNotAllowed, "wert %v (%T) muss abgelehnt werden", falsch, falsch)
	}
}
