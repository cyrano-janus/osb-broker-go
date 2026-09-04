package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/definition"
	"github.com/example/osb-broker/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8scorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Definition mit Service-Binding-Spec-Merkmalen (Phase 6): der Secret-Name
// kommt aus status.binding.name, die Keys werden umgeformt, und das Ergebnis
// wird zusaetzlich als spec-konformes Secret abgelegt.
const specDefYAML = `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: spec-db
spec:
  offering:
    id: spec-svc-0001
    name: spec-db
    description: "DB mit Service-Binding-Spec"
    plans:
      - id: spec-plan-free
        name: free
        params:
          size: small
  provision:
    apiVersion: test.example.com/v1
    kind: Database
    template: |
      apiVersion: test.example.com/v1
      kind: Database
      metadata:
        name: {{ .safeName }}
      spec:
        size: {{ .plan.size }}
  readiness:
    statusJSONPath: 'status.phase'
    expectedValue: "Running"
  bind:
    provisionedService: true
    projectSecret: true
    type: postgresql
    provider: test-operator
    mapping:
      - name: username
        from: user
      - name: password
        from: pass
      - name: uri
        value: "postgresql://{{ .credentials.user }}:{{ .credentials.pass }}@{{ .credentials.host }}:5432/app"
`

func newSpecBindingRouter(t *testing.T) (*gin.Engine, *definition.OperatorClient) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sd, err := definition.Parse([]byte(specDefYAML))
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	ctrlClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	oc := definition.NewOperatorClient(ctrlClient)
	engine := definition.NewEngine(oc, sd)

	stateStore := broker.NewInMemoryStateStore()
	engine.SetInstanceRegistry(&stateStoreRegistry{store: stateStore})

	b := broker.New(store.NewInMemoryStore(), stateStore)
	h := New(b)
	h.SetEngine(&EngineHolder{Engine: engine, Op: oc})
	return h.SetupRouter(), oc
}

// operatorSecret legt das Secret an, das der Operator erzeugt haben wuerde,
// und traegt seinen Namen in status.binding.name des CR ein.
func operatorSecret(t *testing.T, oc *definition.OperatorClient, instanceID string) {
	t.Helper()
	ctx := context.Background()
	safe := definition.SanitizeInstanceName(instanceID)

	require.NoError(t, oc.Client.Create(ctx, &k8scorev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vom-operator", Namespace: "default"},
		Data: map[string][]byte{
			"user": []byte("app"), "pass": []byte("s3cr3t"), "host": []byte("db-rw"),
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----"),
		},
	}))

	cr, err := oc.GetCR(ctx, "test.example.com/v1", "Database", "default", safe)
	require.NoError(t, err)
	cr.Object["status"] = map[string]interface{}{
		"phase":   "Running",
		"binding": map[string]interface{}{"name": "vom-operator"},
	}
	require.NoError(t, oc.Client.Update(ctx, cr))
}

func deleteJSON(router *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", path, nil)
	router.ServeHTTP(w, req)
	return w
}

func TestSpecBinding_BindLiefertGeformteCredentialsUndSchreibtDasSecret(t *testing.T) {
	router, oc := newSpecBindingRouter(t)
	const instanceID = "spec-inst-1"
	const bindingID = "spec-bind-1"

	w := provisionJSON(router, "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": "spec-svc-0001", "plan_id": "spec-plan-free",
	})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	operatorSecret(t, oc, instanceID)

	w = putJSON(router, "/v2/service_instances/"+instanceID+"/service_bindings/"+bindingID,
		map[string]interface{}{"service_id": "spec-svc-0001", "plan_id": "spec-plan-free"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var resp struct {
		Credentials map[string]interface{} `json:"credentials"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "postgresql", resp.Credentials["type"], "die Spezifikation verlangt type")
	assert.Equal(t, "test-operator", resp.Credentials["provider"])
	assert.Equal(t, "app", resp.Credentials["username"])
	assert.Equal(t, "postgresql://app:s3cr3t@db-rw:5432/app", resp.Credentials["uri"])
	assert.NotContains(t, resp.Credentials, "ca.crt",
		"eine Zertifikatsdatei gehoert nicht ungefragt in ein Binding (FINDINGS #23)")

	// 6.4: dasselbe Binding steht Konsumenten ausserhalb von Cloud Foundry
	// als Secret zur Verfuegung.
	sec, err := oc.GetSecretObj(context.Background(), "default",
		definition.SanitizeInstanceName(bindingID+"-binding"))
	require.NoError(t, err)
	assert.Equal(t, "servicebinding.io/postgresql", string(sec.Type))
	assert.Equal(t, "app", string(sec.Data["username"]))
	assert.Equal(t, "postgresql", string(sec.Data["type"]))
}

func TestSpecBinding_UnbindRaeumtDasProjizierteSecretAb(t *testing.T) {
	router, oc := newSpecBindingRouter(t)
	const instanceID = "spec-inst-2"
	const bindingID = "spec-bind-2"
	ctx := context.Background()

	require.Equal(t, http.StatusAccepted, provisionJSON(router, "/v2/service_instances/"+instanceID,
		map[string]interface{}{"service_id": "spec-svc-0001", "plan_id": "spec-plan-free"}).Code)
	operatorSecret(t, oc, instanceID)
	require.Equal(t, http.StatusCreated, putJSON(router,
		"/v2/service_instances/"+instanceID+"/service_bindings/"+bindingID,
		map[string]interface{}{"service_id": "spec-svc-0001", "plan_id": "spec-plan-free"}).Code)

	name := definition.SanitizeInstanceName(bindingID + "-binding")
	_, err := oc.GetSecretObj(ctx, "default", name)
	require.NoError(t, err)

	w := deleteJSON(router, "/v2/service_instances/"+instanceID+"/service_bindings/"+bindingID+
		"?service_id=spec-svc-0001&plan_id=spec-plan-free")
	require.Equal(t, http.StatusOK, w.Code)

	_, err = oc.GetSecretObj(ctx, "default", name)
	assert.Error(t, err, "nach dem Unbind darf kein Secret mit echten Credentials stehen bleiben")
}

func TestSpecBinding_UnbindEinerUnbekanntenIst410(t *testing.T) {
	// OSB 2.17: das Unbind einer Binding, die es nicht gibt, ist 410 Gone.
	// Idempotenz beim Loeschen heisst genau das - nicht ein zweites 200, das
	// eine Loeschung behauptet, die nie stattgefunden hat. Ohne
	// Binding-Datensatz konnte der Broker den Unterschied nicht kennen.
	router, _ := newSpecBindingRouter(t)
	w := deleteJSON(router, "/v2/service_instances/spec-inst-3/service_bindings/spec-bind-3"+
		"?service_id=spec-svc-0001&plan_id=spec-plan-free")
	assert.Equal(t, http.StatusGone, w.Code, w.Body.String())
}
