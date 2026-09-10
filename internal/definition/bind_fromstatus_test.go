package definition

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Ein Binding muss eine Adresse ausliefern, die der KONSUMENT erreicht.
//
// Der Operator schreibt in sein Secret, was innerhalb des Clusters gilt - bei
// CloudNativePG den blanken Servicenamen. Eine Cloud-Foundry-Anwendung laeuft
// abgeschottet davon und kommt dort nie an (FINDINGS #32). Die Adresse, die
// zaehlt, steht dann im STATUS eines anderen Objekts: der externen Service-IP.
//
// `bind.fromStatus` holt sie von dort.

const dienstYAML = `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: mit-status
spec:
  offering:
    id: svc-status-0001
    name: mit-status
    plans:
      - id: plan-status-0001
        name: small
        params: {}
  provision:
    apiVersion: test.example.com/v1
    kind: Database
    template: |
      apiVersion: test.example.com/v1
      kind: Database
      metadata:
        name: {{ .safeName }}
  readiness:
    statusJSONPath: 'status.ready'
  bind:
    credentialsFromSecret: "{{ .safeName }}-app"
    fromStatus:
      - name: externerHost
        apiVersion: v1
        kind: Service
        objectName: "{{ .safeName }}-extern"
        jsonPath: 'status.loadBalancer.ingress.0.ip'
    mapping:
      - name: username
        from: username
      - name: host
        value: "{{ .fromStatus.externerHost }}"
      - name: uri
        value: "postgres://{{ .credentials.username }}@{{ .fromStatus.externerHost }}:5432/app"
`

func definitionMitStatus(t *testing.T, ersetzungen ...string) (*ServiceDefinition, error) {
	t.Helper()
	y := dienstYAML
	for i := 0; i+1 < len(ersetzungen); i += 2 {
		y = strings.Replace(y, ersetzungen[i], ersetzungen[i+1], 1)
	}
	return Parse([]byte(y))
}

// --- Ladepruefungen ------------------------------------------------------

// fromStatus ohne mapping ist wirkungslos: die Werte haetten keinen Weg ins
// Ergebnis. Das faellt beim Laden auf, nicht beim ersten Bind.
func TestFromStatus_OhneMappingIstEinKonfigurationsfehler(t *testing.T) {
	_, err := definitionMitStatus(t, "    mapping:", "    mappingAus:")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mapping",
		"die Meldung muss sagen, dass die Werte nirgends ankommen: %v", err)
}

func TestFromStatus_UnvollstaendigerEintragFaelltBeimLadenAuf(t *testing.T) {
	for _, weg := range []string{
		"        jsonPath: 'status.loadBalancer.ingress.0.ip'\n",
		"        kind: Service\n",
		"      - name: externerHost\n",
	} {
		_, err := definitionMitStatus(t, weg, "")
		assert.Error(t, err, "fehlend: %q", strings.TrimSpace(weg))
	}
}

// --- Aufloesung ----------------------------------------------------------

// geheim legt das Secret an, aus dem der Bind sonst alles nimmt.
func geheim(t *testing.T, oc *OperatorClient, namespace, name string, daten map[string]string) {
	t.Helper()
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		StringData: daten,
		Data:       map[string][]byte{},
	}
	for k, v := range daten {
		sec.Data[k] = []byte(v)
	}
	require.NoError(t, oc.Client.Create(context.Background(), sec))
}

func serviceMitIP(t *testing.T, oc *OperatorClient, namespace, name, ip string) {
	t.Helper()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
	}
	if ip != "" {
		svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: ip}}
	}
	require.NoError(t, oc.Client.Create(context.Background(), svc))
}

func TestFromStatus_WertLandetInDenCredentials(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd, err := definitionMitStatus(t)
	require.NoError(t, err)
	e := NewEngine(oc, sd)

	const instanz = "inst-status-1"
	serviceMitIP(t, oc, "default", SanitizeInstanceName(instanz)+"-extern", "203.0.113.7")
	geheim(t, oc, "default", SanitizeInstanceName(instanz)+"-app", map[string]string{"username": "app"})

	creds, _, err := e.BindCredentials(context.Background(), sd, "default", instanz)
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", creds["host"])
	assert.Equal(t, "postgres://app@203.0.113.7:5432/app", creds["uri"],
		"die externe Adresse gehoert auch in die URI - sonst nutzt sie niemandem")
}

// --- Die drei Fehlerfaelle, und warum sie verschieden sind ---------------

func TestFromStatus_FehlendesObjektNenntObjektUndArt(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd, err := definitionMitStatus(t)
	require.NoError(t, err)
	e := NewEngine(oc, sd)

	const instanz = "inst-status-2"
	geheim(t, oc, "default", SanitizeInstanceName(instanz)+"-app", map[string]string{"username": "app"})

	_, _, err = e.BindCredentials(context.Background(), sd, "default", instanz)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Service")
	assert.Contains(t, err.Error(), "-extern",
		"ohne den Objektnamen sucht der Betreiber blind: %v", err)
}

// **Der wichtigste Fall.** Das Objekt ist da, der Pfad findet nichts - eine
// LoadBalancer-Adresse wird nicht sofort vergeben. Das ist KEIN Konfigurations-
// fehler, sondern ein "noch nicht", und die Meldung muss das sagen: der
// Betreiber soll warten, nicht die Definition umbauen.
func TestFromStatus_NochNichtVergebenIstEinEigenerFall(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd, err := definitionMitStatus(t)
	require.NoError(t, err)
	e := NewEngine(oc, sd)

	const instanz = "inst-status-3"
	serviceMitIP(t, oc, "default", SanitizeInstanceName(instanz)+"-extern", "") // Status leer
	geheim(t, oc, "default", SanitizeInstanceName(instanz)+"-app", map[string]string{"username": "app"})

	_, _, err = e.BindCredentials(context.Background(), sd, "default", instanz)

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "noch nicht",
		"ein leerer Status ist ein Warten, kein Fehler in der Definition: %v", err)
}

func TestFromStatus_FalscherPfadZeigtWasWirklichDaSteht(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd, err := definitionMitStatus(t,
		"jsonPath: 'status.loadBalancer.ingress.0.ip'", "jsonPath: 'status.gibtEsNicht'")
	require.NoError(t, err)
	e := NewEngine(oc, sd)

	const instanz = "inst-status-4"
	serviceMitIP(t, oc, "default", SanitizeInstanceName(instanz)+"-extern", "203.0.113.7")
	geheim(t, oc, "default", SanitizeInstanceName(instanz)+"-app", map[string]string{"username": "app"})

	_, _, err = e.BindCredentials(context.Background(), sd, "default", instanz)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status.gibtEsNicht")
	assert.Contains(t, err.Error(), "loadBalancer",
		"wie bei der Readiness: zeigen, was der Status WIRKLICH enthaelt: %v", err)
}
