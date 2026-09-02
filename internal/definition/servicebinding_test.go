package definition

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Phase 6: die ServiceDefinition beschreibt, wie aus dem Secret des Operators
// ein Binding nach CNCF Service Binding Specification wird.

// bindYAML baut eine gueltige Definition mit dem uebergebenen bind-Block.
func bindYAML(bindBlock string) string {
	return `apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: test
spec:
  offering:
    id: svc-1
    name: test
    plans:
      - id: plan-1
        name: small
  provision:
    apiVersion: example.com/v1
    kind: Widget
    template: |
      apiVersion: example.com/v1
      kind: Widget
  readiness:
    statusJSONPath: status.ready
  bind:
` + bindBlock
}

func TestBind_BestehendeDefinitionBleibtGueltig(t *testing.T) {
	// Rueckwaertskompatibilitaet (6.3): eine Definition ohne die neuen Felder
	// muss unveraendert parsen und validieren.
	sd, err := Parse([]byte(bindYAML("    credentialsFromSecret: \"{{ .safeName }}-app\"\n")))
	require.NoError(t, err)
	assert.Equal(t, "{{ .safeName }}-app", sd.Spec.Bind.CredentialsFromSecret)
	assert.False(t, sd.Spec.Bind.ProvisionedService)
	assert.Empty(t, sd.Spec.Bind.Mapping)
}

func TestBind_ProvisionedServiceErsetztDenNamenstemplate(t *testing.T) {
	// 6.1: der Secret-Name kommt aus status.binding.name des CR, wie es die
	// Spec fuer einen Provisioned Service vorschreibt. Dann ist
	// credentialsFromSecret nicht mehr noetig.
	sd, err := Parse([]byte(bindYAML("    provisionedService: true\n    type: postgresql\n")))
	require.NoError(t, err)
	assert.True(t, sd.Spec.Bind.ProvisionedService)
	assert.Equal(t, "postgresql", sd.Spec.Bind.Type)
	assert.NoError(t, sd.Validate())
}

func TestBind_OhneQuelleIstUngueltig(t *testing.T) {
	// Weder ein Namenstemplate noch der Spec-Weg: dann weiss der Broker
	// nicht, wo die Credentials liegen.
	_, err := Parse([]byte(bindYAML("    type: postgresql\n")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentialsFromSecret")
}

func TestBind_MappingWirdGeparst(t *testing.T) {
	// 6.2: der Fallback-Adapter fuer Operatoren ohne native Spec-Unterstuetzung.
	sd, err := Parse([]byte(bindYAML(`    credentialsFromSecret: "{{ .safeName }}-app"
    type: postgresql
    provider: cloudnative-pg
    mapping:
      - name: username
        from: user
      - name: uri
        value: "postgresql://{{ .credentials.user }}@{{ .credentials.host }}:5432/app"
`)))
	require.NoError(t, err)
	require.Len(t, sd.Spec.Bind.Mapping, 2)
	assert.Equal(t, "username", sd.Spec.Bind.Mapping[0].Name)
	assert.Equal(t, "user", sd.Spec.Bind.Mapping[0].From)
	assert.Contains(t, sd.Spec.Bind.Mapping[1].Value, "postgresql://")
	assert.Equal(t, "cloudnative-pg", sd.Spec.Bind.Provider)
}

func TestBind_MappingValidierung(t *testing.T) {
	cases := []struct {
		name  string
		block string
		want  string
	}{
		{
			name:  "Eintrag ohne Zielnamen",
			block: "    credentialsFromSecret: s\n    mapping:\n      - from: user\n",
			want:  "name is required",
		},
		{
			name:  "weder from noch value",
			block: "    credentialsFromSecret: s\n    mapping:\n      - name: username\n",
			want:  "from",
		},
		{
			name:  "from und value gleichzeitig",
			block: "    credentialsFromSecret: s\n    mapping:\n      - name: username\n        from: user\n        value: x\n",
			want:  "exactly one",
		},
		{
			name:  "doppelter Zielname",
			block: "    credentialsFromSecret: s\n    mapping:\n      - name: username\n        from: user\n      - name: username\n        from: other\n",
			want:  "duplicate",
		},
		{
			name:  "kaputtes Template im value",
			block: "    credentialsFromSecret: s\n    mapping:\n      - name: uri\n        value: \"{{ .credentials.user \"\n",
			want:  "template",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(bindYAML(tc.block)))
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tc.want))
		})
	}
}

func TestBind_ProjectSecretBrauchtEinenTyp(t *testing.T) {
	// 6.4: das projizierte Secret ist nur dann spec-konform, wenn es ein
	// type-Feld traegt - die Spec verlangt es. Ohne Typ waere die Ausgabe
	// wertlos fuer jeden Konsumenten, der sich darauf verlaesst.
	_, err := Parse([]byte(bindYAML("    credentialsFromSecret: s\n    projectSecret: true\n")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type")

	sd, err := Parse([]byte(bindYAML("    credentialsFromSecret: s\n    projectSecret: true\n    type: redis\n")))
	require.NoError(t, err)
	assert.True(t, sd.Spec.Bind.ProjectSecret)
}

func TestBind_TypDarfKeinUngueltigerSecretKeyWerden(t *testing.T) {
	// type landet als Key im projizierten Secret und in den Credentials;
	// Leerzeichen oder Grossbuchstaben waeren dort ein Problem.
	_, err := Parse([]byte(bindYAML("    credentialsFromSecret: s\n    type: \"Post greSQL\"\n")))
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "type")
}

// --- 6.1: Secret-Namen aus status.binding.name ---

// bindTestEngine baut eine Engine mit Fake-Client, legt das CR mit dem
// gegebenen status-Block an und schreibt die Secrets.
func bindTestEngine(t *testing.T, sd *ServiceDefinition, status map[string]interface{}, secrets map[string]map[string][]byte) *Engine {
	t.Helper()
	oc, scheme := newTestOperatorClient(t)

	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"})
	cr.SetNamespace("default")
	cr.SetName(SanitizeInstanceName("inst-1"))
	if status != nil {
		require.NoError(t, unstructured.SetNestedMap(cr.Object, status, "status"))
	}
	require.NoError(t, oc.Client.Create(context.Background(), cr))

	for name, data := range secrets {
		require.NoError(t, oc.Client.Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}, Data: data,
		}))
	}
	_ = scheme

	return NewEngine(oc, sd)
}

func parseBind(t *testing.T, block string) *ServiceDefinition {
	t.Helper()
	sd, err := Parse([]byte(bindYAML(block)))
	require.NoError(t, err)
	return sd
}

func TestBindCredentials_LiestDenSecretNamenAusStatusBinding(t *testing.T) {
	// Der Kern von 6.1: der Operator sagt selbst, wo die Credentials liegen.
	sd := parseBind(t, "    provisionedService: true\n    type: postgresql\n")
	e := bindTestEngine(t, sd,
		map[string]interface{}{"binding": map[string]interface{}{"name": "vom-operator-benannt"}},
		map[string]map[string][]byte{"vom-operator-benannt": {"username": []byte("app")}})

	creds, secretName, err := e.BindCredentials(context.Background(), sd, "default", "inst-1")
	require.NoError(t, err)
	assert.Equal(t, "vom-operator-benannt", secretName)
	assert.Equal(t, "app", creds["username"])
}

func TestBindCredentials_FaelltAufDasNamenstemplateZurueck(t *testing.T) {
	// 6.3: Operatoren, die status.binding.name noch nicht fuellen, bleiben
	// nutzbar - genau dafuer ist das Template die Rueckfallebene.
	sd := parseBind(t, "    provisionedService: true\n    credentialsFromSecret: \"{{ .safeName }}-app\"\n")
	e := bindTestEngine(t, sd,
		map[string]interface{}{"ready": "True"}, // kein binding-Block
		map[string]map[string][]byte{SanitizeInstanceName("inst-1") + "-app": {"username": []byte("app")}})

	creds, secretName, err := e.BindCredentials(context.Background(), sd, "default", "inst-1")
	require.NoError(t, err)
	assert.Equal(t, SanitizeInstanceName("inst-1")+"-app", secretName)
	assert.Equal(t, "app", creds["username"])
}

func TestBindCredentials_OhneStatusUndOhneTemplateIstEinKlarerFehler(t *testing.T) {
	sd := parseBind(t, "    provisionedService: true\n")
	e := bindTestEngine(t, sd, map[string]interface{}{"ready": "True"}, nil)

	_, _, err := e.BindCredentials(context.Background(), sd, "default", "inst-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status.binding.name")
}

func TestBindCredentials_OhneProvisionedServiceWirdDasCRNichtGelesen(t *testing.T) {
	// Rueckwaertskompatibilitaet: der bisherige Pfad darf keinen zusaetzlichen
	// API-Aufruf machen - und muss auch funktionieren, wenn das CR fehlt.
	sd := parseBind(t, "    credentialsFromSecret: \"{{ .safeName }}-app\"\n")
	oc, _ := newTestOperatorClient(t)
	require.NoError(t, oc.Client.Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: SanitizeInstanceName("inst-1") + "-app", Namespace: "default"},
		Data:       map[string][]byte{"username": []byte("app")},
	}))
	e := NewEngine(oc, sd)

	creds, _, err := e.BindCredentials(context.Background(), sd, "default", "inst-1")
	require.NoError(t, err)
	assert.Equal(t, "app", creds["username"])
}

// --- 6.2: Mapping auf die Zielform ---

func TestShapeCredentials_OhneMappingBleibtAllesWieBisher(t *testing.T) {
	// Rueckwaertskompatibilitaet (6.3), Punkt fuer Punkt: alle Keys, kein
	// type, keine Umbenennung.
	b := &Bind{}
	got, err := shapeCredentials(b, map[string][]byte{"user": []byte("app"), "password": []byte("s3cr3t")})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"user": "app", "password": "s3cr3t"}, got)
}

func TestShapeCredentials_CredentialKeysFiltertWeiterhin(t *testing.T) {
	b := &Bind{CredentialKeys: []string{"user"}}
	got, err := shapeCredentials(b, map[string][]byte{"user": []byte("app"), "ca.crt": []byte("...")})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"user": "app"}, got)
}

func TestShapeCredentials_MappingBenenntUmUndSetztZusammen(t *testing.T) {
	b := &Bind{
		Type:     "postgresql",
		Provider: "cloudnative-pg",
		Mapping: []CredentialMapping{
			{Name: "username", From: "user"},
			{Name: "password", From: "pass"},
			{Name: "uri", Value: "postgresql://{{ .credentials.user }}:{{ .credentials.pass }}@{{ .credentials.host }}:5432/app"},
		},
	}
	got, err := shapeCredentials(b, map[string][]byte{
		"user": []byte("app"), "pass": []byte("s3cr3t"), "host": []byte("db-rw"),
		"ca.crt": []byte("nicht gewollt"),
	})
	require.NoError(t, err)

	assert.Equal(t, "app", got["username"])
	assert.Equal(t, "s3cr3t", got["password"])
	assert.Equal(t, "postgresql://app:s3cr3t@db-rw:5432/app", got["uri"])
	assert.Equal(t, "postgresql", got["type"])
	assert.Equal(t, "cloudnative-pg", got["provider"])

	// Genau die gemappten Keys plus type/provider - sonst waere die
	// definierte Zielform keine.
	assert.NotContains(t, got, "user", "der Originalschluessel darf nicht durchrutschen")
	assert.NotContains(t, got, "ca.crt", "eine Zertifikatsdatei gehoert nicht in ein Binding")
	assert.Len(t, got, 5)
}

func TestShapeCredentials_FehlenderQuellschluesselIstEinFehler(t *testing.T) {
	// Still auslassen waere schlimmer: ein halb gefuelltes Binding faellt
	// erst in der App auf, und dann sieht es wie ein App-Fehler aus.
	b := &Bind{Mapping: []CredentialMapping{{Name: "username", From: "gibt-es-nicht"}}}
	_, err := shapeCredentials(b, map[string][]byte{"user": []byte("app")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gibt-es-nicht")
}

func TestShapeCredentials_FehlenderKeyImTemplateIstEinFehler(t *testing.T) {
	b := &Bind{Mapping: []CredentialMapping{
		{Name: "uri", Value: "postgres://{{ .credentials.fehlt }}/db"},
	}}
	_, err := shapeCredentials(b, map[string][]byte{"user": []byte("app")})
	require.Error(t, err)
}

func TestShapeCredentials_DefinitionHatDasLetzteWortBeimTyp(t *testing.T) {
	// Ein Operator-Secret mit einem eigenen "type"-Key darf den deklarierten
	// Typ nicht ueberschreiben - sonst haengt die Spec-Konformitaet daran,
	// was der Operator zufaellig mitliefert.
	b := &Bind{Type: "postgresql"}
	got, err := shapeCredentials(b, map[string][]byte{"type": []byte("etwas-anderes")})
	require.NoError(t, err)
	assert.Equal(t, "postgresql", got["type"])
}

func TestShapeCredentials_OhneTypBleibtDasFeldWeg(t *testing.T) {
	// Nicht jede Definition ist spec-konform, und ein leeres type-Feld waere
	// schlechter als keines.
	b := &Bind{}
	got, err := shapeCredentials(b, map[string][]byte{"user": []byte("app")})
	require.NoError(t, err)
	assert.NotContains(t, got, "type")
}
