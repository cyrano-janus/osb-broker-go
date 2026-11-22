package chart

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Die Definition cnpg-postgresql nennt nur die Major, das Chart liefert den
// Katalog mit den Images dazu. Zwei Listen an zwei Stellen - das oneOf in der
// Definition und die Eintraege im Chart - driften auseinander, sobald jemand
// nur eine hebt: dann ist eine Major waehlbar, zu der es kein Image gibt, und
// das faellt erst am Readiness-Timeout auf.
func TestChart_KatalogDecktDasVersionsfenster(t *testing.T) {
	if !helmAvailable() {
		t.Skip("helm ist nicht installiert")
	}
	out, err := helmTemplate(t, "values-kind.yaml")
	require.NoError(t, err, out)

	var katalog map[string]interface{}
	for _, doc := range strings.Split(out, "\n---") {
		if !strings.Contains(doc, "kind: ClusterImageCatalog") || strings.Contains(doc, "kind: ConfigMap") {
			continue
		}
		require.NoError(t, yaml.Unmarshal([]byte(doc), &katalog))
	}
	require.NotNil(t, katalog, "values-kind.yaml rendert keinen ClusterImageCatalog")
	meta := katalog["metadata"].(map[string]interface{})
	assert.Equal(t, "postgresql", meta["name"], "die Definition verweist auf den Namen `postgresql`")

	var majors []string
	for _, e := range katalog["spec"].(map[string]interface{})["images"].([]interface{}) {
		eintrag := e.(map[string]interface{})
		major := eintrag["major"]
		image := eintrag["image"].(string)
		majors = append(majors, strings.TrimSpace(strings.Trim(yamlScalar(major), "\"")))
		// Mit Nebenversion, sonst entscheidet der Image-Cache des Knotens.
		assert.Regexp(t, `:\d+\.\d+-`, image, "Eintrag %v: Tag ohne Nebenversion", major)
	}

	// Das Fenster aus der Definition, nicht aus dem Kopf.
	var def struct {
		Spec struct {
			Offering struct {
				Plans []struct {
					Name   string `yaml:"name"`
					Limits map[string]struct {
						OneOf []string `yaml:"oneOf"`
					} `yaml:"parameterLimits"`
				} `yaml:"plans"`
			} `yaml:"offering"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(read(t, "definitions/cnpg-postgresql.yaml")), &def))
	for _, p := range def.Spec.Offering.Plans {
		assert.ElementsMatch(t, p.Limits["majorVersion"].OneOf, majors,
			"plan %q: oneOf und Katalog nennen nicht dieselben Majors", p.Name)
	}
}

// Ohne Katalog ist cnpg-postgresql nicht lieferbar. Das Chart soll das beim
// Rendern sagen, nicht die erste Instanz am Readiness-Timeout - es sei denn,
// jemand stellt den Katalog selbst und sagt das mit `existing`.
func TestChart_KatalogFehltIstEinRenderfehler(t *testing.T) {
	if !helmAvailable() {
		t.Skip("helm ist nicht installiert")
	}
	out, err := helmTemplateArgs(t, []string{"values-kind.yaml"}, "--set", "postgresImageCatalog.create=false")
	require.Error(t, err, "rendert, obwohl die Definition einen Katalog braucht und keiner kommt")
	assert.Contains(t, out, "postgresImageCatalog")

	out, err = helmTemplateArgs(t, []string{"values-kind.yaml"},
		"--set", "postgresImageCatalog.create=false", "--set", "postgresImageCatalog.existing=true")
	require.NoError(t, err, out)
	assert.NotContains(t, out, "\nkind: ClusterImageCatalog", "existing heisst: das Chart legt keinen an")

	// Ohne Definitionen braucht niemand einen Katalog - die Vorgabe rendert.
	out, err = helmTemplateArgs(t, nil, "--set", "tls.certManager.issuerName=x")
	if err != nil {
		t.Logf("Vorgabe rendert nicht (das prueft ein anderer Test): %s", out)
	} else {
		assert.NotContains(t, out, "\nkind: ClusterImageCatalog")
	}
}

func yamlScalar(v interface{}) string {
	b, _ := yaml.Marshal(v)
	return string(b)
}
