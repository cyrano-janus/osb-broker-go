package definition

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
