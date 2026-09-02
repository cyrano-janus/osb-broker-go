package v1alpha1

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// Kubernetes prueft Custom Resources gegen ein strukturelles Schema und
// LOESCHT dabei jedes Feld, das darin nicht vorkommt - ohne Fehler, ohne
// Warnung. Ein Feld, das im Go-Typ steht, aber im ausgelieferten CRD fehlt,
// faellt also nicht beim Schreiben auf, sondern erst, wenn jemand es
// zurueckliest und es weg ist.
//
// Deshalb vergleicht dieser Test die json-Tags der Spec-Strukturen mit den
// Properties im YAML. Er ist der Grund, warum die CRDs von Hand gepflegt
// werden duerfen, ohne dass daraus eine Fehlerquelle wird.

type crdSchema struct {
	Spec struct {
		Group string `json:"group"`
		Scope string `json:"scope"`
		Names struct {
			Kind   string `json:"kind"`
			Plural string `json:"plural"`
		} `json:"names"`
		Versions []struct {
			Name    string `json:"name"`
			Served  bool   `json:"served"`
			Storage bool   `json:"storage"`
			Schema  struct {
				OpenAPIV3Schema struct {
					Properties map[string]struct {
						Properties map[string]interface{} `json:"properties"`
						Required   []string               `json:"required"`
					} `json:"properties"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

func loadCRD(t *testing.T, file string) crdSchema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "crds", file))
	require.NoError(t, err, "die ausgelieferten CRDs muessen im Repo liegen")
	var out crdSchema
	require.NoError(t, yaml.Unmarshal(raw, &out))
	return out
}

// jsonFields liefert die json-Feldnamen einer Struktur.
func jsonFields(t *testing.T, v interface{}) []string {
	t.Helper()
	typ := reflect.TypeOf(v)
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func assertSchemaDecktStruktur(t *testing.T, crd crdSchema, spec interface{}) {
	t.Helper()
	require.Len(t, crd.Spec.Versions, 1)
	require.True(t, crd.Spec.Versions[0].Served)
	require.True(t, crd.Spec.Versions[0].Storage)
	assert.Equal(t, GroupName, crd.Spec.Group)
	assert.Equal(t, Version, crd.Spec.Versions[0].Name)
	assert.Equal(t, "Namespaced", crd.Spec.Scope,
		"der Zustand gehoert in den Namespace des Brokers, nicht in den Cluster-Scope")

	props := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties
	require.NotEmpty(t, props)
	for _, field := range jsonFields(t, spec) {
		assert.Contains(t, props, field,
			"Feld %q fehlt im CRD-Schema - Kubernetes wuerde es beim Schreiben still verwerfen", field)
	}
}

func TestCRD_InstanzSchemaDecktDenGoTyp(t *testing.T) {
	crd := loadCRD(t, "broker.osb.io_osbserviceinstances.yaml")
	assert.Equal(t, "OSBServiceInstance", crd.Spec.Names.Kind)
	assertSchemaDecktStruktur(t, crd, OSBServiceInstanceSpec{})
	assert.ElementsMatch(t, []string{"id", "serviceId", "planId"},
		crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Required)
}

func TestCRD_BindingSchemaDecktDenGoTyp(t *testing.T) {
	crd := loadCRD(t, "broker.osb.io_osbservicebindings.yaml")
	assert.Equal(t, "OSBServiceBinding", crd.Spec.Names.Kind)
	assertSchemaDecktStruktur(t, crd, OSBServiceBindingSpec{})
	assert.ElementsMatch(t, []string{"id", "instanceId", "serviceId", "planId"},
		crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Required)
}

func TestCRD_KontextSchemaDecktDenGoTyp(t *testing.T) {
	// Der Kontext ist verschachtelt und wird von beiden Typen benutzt; ein
	// vergessenes Feld hier verliert die Space-GUID, an der die
	// Namespace-Zuordnung haengt.
	for _, file := range []string{
		"broker.osb.io_osbserviceinstances.yaml",
		"broker.osb.io_osbservicebindings.yaml",
	} {
		crd := loadCRD(t, file)
		spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties
		ctx, ok := spec["context"].(map[string]interface{})
		require.True(t, ok, file)
		ctxProps, ok := ctx["properties"].(map[string]interface{})
		require.True(t, ok, file)
		for _, field := range jsonFields(t, OSBContext{}) {
			assert.Contains(t, ctxProps, field, "%s: context.%s fehlt", file, field)
		}
	}
}
