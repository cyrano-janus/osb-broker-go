// Package chart traegt die Waechter ueber das Helm-Chart.
//
// Das Chart liefert die ServiceDefinitions als eingebettete Zeichenketten in
// einer Wertedatei aus. Zwei Kopien desselben Inhalts driften auseinander,
// sobald jemand nur eine anfasst - und niemand merkt es, weil beide fuer sich
// gelesen stimmig bleiben. Genau das war der Fall: die eingebettete
// RabbitMQ-Definition fehlten drei Felder, drei Schluessel kamen doppelt vor,
// und eine Definition nannte eine andere CRD-Gruppe als die Datei unter
// definitions/.
//
// Die Waechter hier machen das unmoeglich, ohne die Auslieferung umzubauen:
// die eingebetteten Kopien muessen zeichengleich mit definitions/ sein, jeder
// Schluessel darf genau einmal vorkommen, und die RBAC-Liste muss genau die
// CRD-Gruppen abdecken, die die Definitionen anfassen - nicht weniger (403
// beim Provision) und nicht mehr (unnoetige clusterweite Rechte).
package chart

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	repoRoot       = "../.."
	definitionsDir = "definitions"
	chartDir       = "deploy/helm/osb-broker-go"
)

func read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	require.NoError(t, err)
	return string(b)
}

// shippedDefinitions liest die Definitionen, die das Repo ausliefert.
func shippedDefinitions(t *testing.T) map[string]string {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(repoRoot, definitionsDir, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "es muss Definitionen geben")

	out := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(e)
		require.NoError(t, err)
		out[filepath.Base(e)] = string(b)
	}
	return out
}

// embeddedDefinitions liest die in eine Wertedatei eingebetteten Definitionen
// aus dem Rohtext - nicht ueber den YAML-Parser, denn der schluckt doppelte
// Schluessel stillschweigend und laesst genau den Fehler verschwinden, um den
// es hier geht.
func embeddedDefinitions(raw string) map[string][]string {
	out := map[string][]string{}
	lines := strings.Split(raw, "\n")
	key := regexp.MustCompile(`^    ([a-z0-9.-]+\.yaml): \|-?$`)

	for i := 0; i < len(lines); i++ {
		m := key.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		var body []string
		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				body = append(body, "")
				continue
			}
			if !strings.HasPrefix(l, "      ") {
				break
			}
			body = append(body, strings.TrimPrefix(l, "      "))
		}
		// Leerzeilen am Ende gehoeren zum Blockende, nicht zum Inhalt.
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		out[m[1]] = append(out[m[1]], strings.Join(body, "\n")+"\n")
	}
	return out
}

func TestChart_KeinSchluesselKommtDoppeltVor(t *testing.T) {
	for _, f := range []string{"values-kind.yaml", "values-ci.yaml"} {
		path := filepath.Join(chartDir, f)
		if _, err := os.Stat(filepath.Join(repoRoot, path)); err != nil {
			continue
		}
		for name, bodies := range embeddedDefinitions(read(t, path)) {
			assert.Len(t, bodies, 1,
				"%s: %q kommt %d mal vor - YAML nimmt den letzten, und welcher das ist, sieht niemand",
				f, name, len(bodies))
		}
	}
}

func TestChart_EingebetteteDefinitionenSindZeichengleich(t *testing.T) {
	shipped := shippedDefinitions(t)
	embedded := embeddedDefinitions(read(t, filepath.Join(chartDir, "values-kind.yaml")))

	require.NotEmpty(t, embedded, "values-kind.yaml muss Definitionen einbetten")

	for name, bodies := range embedded {
		want, ok := shipped[name]
		if !assert.True(t, ok, "values-kind.yaml bettet %q ein, das es unter definitions/ nicht gibt", name) {
			continue
		}
		assert.Equal(t, want, bodies[len(bodies)-1],
			"%q weicht von definitions/%s ab - eine Auslieferung damit verhaelt sich anders als das Repo", name, name)
	}

	var missing []string
	for name := range shipped {
		if _, ok := embedded[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	assert.Empty(t, missing, "definitions/ enthaelt Dateien, die values-kind.yaml nicht ausliefert")
}

// provisionGroups liefert die CRD-Gruppen, die die Definitionen anfassen.
func provisionGroups(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, body := range shippedDefinitions(t) {
		var d struct {
			Spec struct {
				Provision struct {
					APIVersion string `yaml:"apiVersion"`
				} `yaml:"provision"`
			} `yaml:"spec"`
		}
		require.NoError(t, yaml.Unmarshal([]byte(body), &d), name)
		av := d.Spec.Provision.APIVersion
		require.NotEmpty(t, av, "%s: spec.provision.apiVersion fehlt", name)
		group, _, _ := strings.Cut(av, "/")
		out[group] = name
	}
	return out
}

func chartRBACGroups(t *testing.T) map[string]bool {
	t.Helper()
	var v struct {
		RBAC struct {
			OperatorCRDs []struct {
				Group string `yaml:"group"`
			} `yaml:"operatorCRDs"`
		} `yaml:"rbac"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(read(t, filepath.Join(chartDir, "values.yaml"))), &v))
	out := map[string]bool{}
	for _, e := range v.RBAC.OperatorCRDs {
		out[e.Group] = true
	}
	return out
}

// Eine Definition, deren CRD-Gruppe nicht in rbac.operatorCRDs steht, laesst
// sich ausliefern und scheitert beim Provision mit 403 - also erst beim
// Kunden.
func TestChart_RBACDecktJedeAusgelieferteDefinitionAb(t *testing.T) {
	granted := chartRBACGroups(t)

	for group, def := range provisionGroups(t) {
		assert.True(t, granted[group],
			"rbac.operatorCRDs deckt %q nicht ab (gebraucht von %s) - jedes Provision waere 403", group, def)
	}
}

// Und andersherum: ein clusterweites Recht auf eine Gruppe, die keine
// mitgelieferte Definition anfasst, ist ein Recht zu viel.
func TestChart_RBACGewaehrtNichtsUeberfluessiges(t *testing.T) {
	needed := provisionGroups(t)

	for group := range chartRBACGroups(t) {
		_, used := needed[group]
		assert.True(t, used,
			"rbac.operatorCRDs gewaehrt %q, aber keine mitgelieferte Definition fasst diese Gruppe an", group)
	}
}

// helmAvailable meldet, ob helm im Pfad liegt. Ohne helm werden die
// Render-Waechter uebersprungen statt fehlzuschlagen: sie pruefen das Chart,
// nicht die Werkzeugausstattung des Rechners.
func helmAvailable() bool {
	_, err := exec.LookPath("helm")
	return err == nil
}

func helmTemplate(t *testing.T, valuesFiles ...string) (string, error) {
	t.Helper()
	args := []string{"template", "guard", chartDir}
	for _, f := range valuesFiles {
		args = append(args, "-f", filepath.Join(chartDir, f))
	}
	cmd := exec.Command("helm", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Jede mitgelieferte Wertedatei muss rendern. Eine, die es nicht tut, faellt
// erst bei dem auf, der sie benutzt.
func TestChart_MitgelieferteWertedateienRendern(t *testing.T) {
	if !helmAvailable() {
		t.Skip("helm ist nicht installiert")
	}
	for _, f := range []string{"values-kind.yaml", "values-ci.yaml"} {
		if _, err := os.Stat(filepath.Join(repoRoot, chartDir, f)); err != nil {
			continue
		}
		out, err := helmTemplate(t, f)
		assert.NoError(t, err, "%s rendert nicht:\n%s", f, out)
	}
}

// Das Chart rendert mit seinen Vorgaben absichtlich NICHT: der Aussteller des
// Broker-Zertifikats ist standortabhaengig und laesst sich nicht raten. Ein
// `required` ist die Art, wie Helm "das musst du entscheiden" ausdrueckt.
//
// Geprueft wird deshalb nicht, dass es rendert, sondern dass es aus dem
// richtigen Grund scheitert und den fehlenden Wert beim Namen nennt. Ein
// stilles Rendern mit leerem Aussteller waere schlimmer: das Deployment ginge
// durch und der Broker bekaeme nie ein Zertifikat.
func TestChart_VorgabenScheiternMitAnsage(t *testing.T) {
	if !helmAvailable() {
		t.Skip("helm ist nicht installiert")
	}
	out, err := helmTemplate(t)
	require.Error(t, err, "die Vorgaben duerfen nicht stillschweigend rendern:\n%s", out)
	assert.Contains(t, out, "tls.certManager.issuerRef.name",
		"die Fehlermeldung muss den fehlenden Wert nennen, sonst sucht jemand an der falschen Stelle")
}

// config.logRequests war im Chart dokumentiert, von keinem Template gerendert
// und vom Broker nie gelesen - ein Schalter, den jemand umlegt und der nichts
// tut. Jeder Wert unter config muss als Umgebungsvariable ankommen.
func TestChart_JederConfigWertErreichtDenBroker(t *testing.T) {
	if !helmAvailable() {
		t.Skip("helm ist nicht installiert")
	}
	out, err := helmTemplate(t, "values-kind.yaml")
	require.NoError(t, err, out)

	for key, env := range map[string]string{
		"storeBackend":      "STORE_BACKEND",
		"logRequests":       "LOG_REQUESTS",
		"reconcileInterval": "RECONCILE_INTERVAL",
	} {
		assert.Contains(t, out, "name: "+env,
			"config.%s wird von keinem Template gerendert - der Schalter waere wirkungslos", key)
	}
}

// Die Image-Version stand an drei Stellen und war an zweien verschieden:
// deploy/k8s/broker.yaml nannte v14, Chart.yaml appVersion v9, values-kind.yaml
// pinnte v9. Welche gilt, sieht man dann erst am laufenden Pod.
//
// Eine Quelle: Chart.yaml appVersion. Das Deployment-Template setzt
// `.Values.image.tag | default .Chart.AppVersion`, eine Wertedatei darf also
// schweigen - und tut sie es nicht, muss sie dasselbe sagen.
func TestChart_DieImageVersionStehtAnEinerStelle(t *testing.T) {
	appVersion := regexp.MustCompile(`(?m)^appVersion:\s*"?([^"\n]+)"?`).
		FindStringSubmatch(read(t, filepath.Join(chartDir, "Chart.yaml")))
	require.Len(t, appVersion, 2, "Chart.yaml muss appVersion nennen")
	want := strings.TrimSpace(appVersion[1])

	// Rohmanifest: dieselbe Version, sonst deployt wer es benutzt etwas anderes
	// als wer das Chart benutzt.
	raw := read(t, "deploy/k8s/broker.yaml")
	if m := regexp.MustCompile(`image: osb-broker-go:(\S+)`).FindStringSubmatch(raw); m != nil {
		assert.Equal(t, want, m[1],
			"deploy/k8s/broker.yaml nennt eine andere Version als Chart.yaml appVersion")
	}

	// Wertedateien: entweder schweigen oder dasselbe sagen.
	//
	// values-ci.yaml ist ausgenommen und pinnt "pr": die CI deployt das Image,
	// das sie gerade gebaut hat, und das traegt keine Release-Version. Ein
	// Gleichlauf mit appVersion waere dort nicht nur unnoetig, sondern falsch.
	for _, f := range []string{"values-kind.yaml"} {
		path := filepath.Join(repoRoot, chartDir, f)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		body := read(t, filepath.Join(chartDir, f))
		m := regexp.MustCompile(`(?m)^  tag:\s*"?([^"\n#]+)"?`).FindStringSubmatch(body)
		if m == nil {
			continue // erbt aus Chart.yaml - genau richtig
		}
		if v := strings.TrimSpace(m[1]); v != "" {
			assert.Equal(t, want, v, "%s pinnt eine andere Version als Chart.yaml appVersion", f)
		}
	}
}
