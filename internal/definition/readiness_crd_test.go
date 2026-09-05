package definition

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Fuenf der sieben mitgelieferten Definitionen trugen denselben kopierten
// Readiness-Pfad auf `type=="Ready"`, ohne dass je ein CR des jeweiligen
// Operators dagegen gehalten wurde. Drei davon konnten nie zutreffen - das
// laesst sich am CRD des Operators beweisen, ohne einen Controller zu starten:
//
//   - Redis (opstree): status hat KEINE Eigenschaften. Der API-Server schneidet
//     alles weg, was nicht im Schema steht, also gibt es dort nie conditions.
//   - MinIO Tenant: status hat keine conditions, sondern currentState und
//     healthStatus.
//   - Redpanda Cluster (redpanda.vectorized.io): conditions[].type ist ein Enum
//     mit genau einem Wert, ClusterConfigured.
//
// Diese Pruefung macht das dauerhaft: zu jeder Definition liegt unter
// testdata/crds/ der status-Ausschnitt aus dem CRD ihres Operators, und der
// Readiness-Pfad muss dagegen erfuellbar sein. Sie ersetzt keinen Durchlauf
// gegen einen laufenden Operator - ein Schema sagt, was moeglich ist, nicht was
// der Operator tut. Sie schliesst aber die Faelle aus, die nachweislich
// unmoeglich sind, und die waren hier die Mehrheit.

const crdDir = "testdata/crds"

type crdExcerpt struct {
	Group    string `yaml:"group"`
	Kind     string `yaml:"kind"`
	Versions []struct {
		Name   string                 `yaml:"name"`
		Status map[string]interface{} `yaml:"status"`
	} `yaml:"versions"`
}

func loadCRD(t *testing.T, name string) crdExcerpt {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(crdDir, name+".yaml"))
	require.NoError(t, err, "zu jeder Definition muss ein CRD-Ausschnitt liegen")
	var c crdExcerpt
	require.NoError(t, yaml.Unmarshal(b, &c))
	return c
}

func loadShippedDefinition(t *testing.T, name string) *ServiceDefinition {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "definitions", name+".yaml"))
	require.NoError(t, err)
	sd, err := Parse(b)
	require.NoError(t, err)
	return sd
}

func shippedNames(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "definitions", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".yaml"))
	}
	return out
}

// statusFor liefert das status-Schema der Version, die die Definition anfasst.
func statusFor(t *testing.T, c crdExcerpt, apiVersion string) (map[string]interface{}, bool) {
	t.Helper()
	_, version, _ := strings.Cut(apiVersion, "/")
	for _, v := range c.Versions {
		if v.Name == version {
			return v.Status, true
		}
	}
	return nil, false
}

// conditionType liest den Condition-Typ aus einem gjson-Pfad der Form
// status.conditions.#(type=="X").status. Leer, wenn der Pfad anders gebaut ist.
var conditionTypeRe = regexp.MustCompile(`conditions\.#\(type==["']([^"']+)["']\)`)

func TestReadiness_JedeDefinitionHatEinCRDSchema(t *testing.T) {
	for _, name := range shippedNames(t) {
		_, err := os.Stat(filepath.Join(crdDir, name+".yaml"))
		assert.NoError(t, err,
			"%s hat keinen CRD-Ausschnitt unter %s - dann ist der Readiness-Pfad ungeprueft, "+
				"und ein falscher faellt erst beim Kunden auf", name, crdDir)
	}
}

func TestReadiness_DerPfadPasstZumSchemaDesOperators(t *testing.T) {
	for _, name := range shippedNames(t) {
		t.Run(name, func(t *testing.T) {
			sd := loadShippedDefinition(t, name)
			crd := loadCRD(t, name)

			gotGroup, _, _ := strings.Cut(sd.Spec.Provision.APIVersion, "/")
			require.Equal(t, crd.Group, gotGroup,
				"die Definition zielt auf eine andere CRD-Gruppe als der hinterlegte Ausschnitt")
			require.Equal(t, crd.Kind, sd.Spec.Provision.Kind)

			status, ok := statusFor(t, crd, sd.Spec.Provision.APIVersion)
			require.True(t, ok, "das CRD kennt die Version aus provision.apiVersion nicht")

			props, _ := status["properties"].(map[string]interface{})
			require.NotEmpty(t, props,
				"das status-Schema hat keine Eigenschaften: der API-Server schneidet dort alles weg, "+
					"eine Readiness aus dem Status ist unmoeglich")

			path := strings.TrimPrefix(sd.Spec.Readiness.StatusJSONPath, ".")
			root, _, _ := strings.Cut(path, ".")
			require.Equal(t, "status", root, "der Pfad muss unter status beginnen")

			field, _, _ := strings.Cut(strings.TrimPrefix(path, "status."), ".")
			_, exists := props[field]
			require.True(t, exists,
				"status.%s gibt es im Schema des Operators nicht - vorhanden sind: %s",
				field, sortedKeys(props))

			// Wo der Operator die Condition-Typen enumeriert, muss der gesuchte
			// dabei sein. Genau daran ist Redpanda aufgefallen.
			if m := conditionTypeRe.FindStringSubmatch(path); m != nil {
				if allowed := conditionTypeEnum(props); len(allowed) > 0 {
					assert.Contains(t, allowed, m[1],
						"der Operator fuehrt nur die Condition-Typen %v - %q kann nie zutreffen",
						allowed, m[1])
				}
			}
		})
	}
}

func conditionTypeEnum(props map[string]interface{}) []string {
	cond, _ := props["conditions"].(map[string]interface{})
	items, _ := cond["items"].(map[string]interface{})
	ip, _ := items["properties"].(map[string]interface{})
	typ, _ := ip["type"].(map[string]interface{})
	raw, _ := typ["enum"].([]interface{})
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
