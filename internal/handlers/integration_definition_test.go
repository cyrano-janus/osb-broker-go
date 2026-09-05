package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/definition"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8scorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testDefYAML = `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: test-db
spec:
  offering:
    id: def-svc-0001
    name: test-db
    description: "Test DB via definition"
    metadata:
      displayName: "Test-Datenbank"
      documentationUrl: "https://example.invalid/docs"
    planUpdateable: true
    plans:
      - id: def-plan-free
        name: free
        allowedParameters: [size]
        params:
          size: small
      - id: def-plan-paid
        name: paid
        free: false
        metadata:
          bullets: ["mehr Speicher"]
        params:
          size: large
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
    credentialsFromSecret: "{{ .safeName }}-creds"
`

func newDefinitionRouter(t *testing.T) (*gin.Engine, *definition.OperatorClient) {
	return routerForYAML(t, testDefYAML)
}

// newNoPlanChangeRouter baut dieselbe Route aus einer Definition, die keinen
// Planwechsel zusagt - die Gegenprobe zu testDefYAML.
func newNoPlanChangeRouter(t *testing.T) (*gin.Engine, *definition.OperatorClient) {
	return routerForYAML(t, strings.Replace(testDefYAML, "    planUpdateable: true\n", "", 1))
}

func routerForYAML(t *testing.T, defYAML string) (*gin.Engine, *definition.OperatorClient) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sd, err := definition.Parse([]byte(defYAML))
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	ctrlClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	oc := definition.NewOperatorClient(ctrlClient)
	engine := definition.NewEngine(oc, sd)

	stateStore := broker.NewInMemoryStateStore()
	engine.SetInstanceRegistry(&stateStoreRegistry{store: stateStore})

	b := broker.New(stateStore)
	h := New(b)
	h.SetEngine(&EngineHolder{Engine: engine, Op: oc})

	// Der zuletzt gebaute Broker, damit Tests den gespeicherten Datensatz
	// pruefen koennen.
	testBroker = b
	testEngine = engine
	testStore = stateStore

	return h.SetupRouter(), oc
}

// testBroker haelt den Broker der zuletzt gebauten Testroute.
var testBroker *broker.Broker

// testEngine haelt die Engine derselben Route, damit ein Test den Katalog
// ueber die Leitung gegen den der Engine halten kann.
var testEngine *definition.Engine

// testStore haelt den Zustandsspeicher derselben Route - der Abgleich zaehlt
// darueber die Instanzen auf.
var testStore broker.StateStore

func putJSON(router *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	return sendJSON(router, "PUT", path, body)
}

// provisionJSON provisioniert wie die Plattform es tut: mit
// accepts_incomplete=true in der Query. Ohne den Parameter antwortet der
// Definitions-Pfad mit 422 AsyncRequired - er kann nicht synchron fertig
// werden, und das zu behaupten war der Fehler.
func provisionJSON(router *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	return sendJSON(router, "PUT", path+"?accepts_incomplete=true", body)
}

func patchJSON(router *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	return sendJSON(router, "PATCH", path, body)
}

func sendJSON(router *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	// Wie eine echte Plattform: ohne den Header ist die Antwort 412.
	req.Header.Set("X-Broker-API-Version", "2.17")
	router.ServeHTTP(w, req)
	return w
}

func TestIntegration_DefinitionLifecycleOverHTTP(t *testing.T) {
	router, oc := newDefinitionRouter(t)
	ctx := context.Background()

	// 1. Catalog contains the definition service
	w := perform(router, "/v2/catalog", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var cat struct {
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cat))
	found := false
	for _, s := range cat.Services {
		if s.Name == "test-db" {
			found = true
		}
	}
	assert.True(t, found, "test-db must appear in catalog")

	// 2. Provision
	w = provisionJSON(router, "/v2/service_instances/inst-int-1", map[string]interface{}{
		"service_id": "def-svc-0001",
		"plan_id":    "def-plan-free",
		"context":    map[string]interface{}{"platform": "cloudfoundry"},
	})
	require.Equal(t, http.StatusAccepted, w.Code, "body: %s", w.Body.String())

	// CR exists with rendered spec
	cr, err := oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "osb-inst-int-1")
	require.NoError(t, err)
	size, _, _ := unstructured.NestedString(cr.Object, "spec", "size")
	assert.Equal(t, "small", size)

	// 3. LastOperation: in progress (no status yet)
	w = httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v2/service_instances/inst-int-1/last_operation?service_id=def-svc-0001&operation=provision", nil)
	req.Header.Set("X-Broker-API-Version", "2.17")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var lo struct {
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lo))
	assert.Equal(t, "in progress", lo.State)

	// 4. Operator marks CR Running -> succeeded
	cr, err = oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "osb-inst-int-1")
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedField(cr.Object, "Running", "status", "phase"))
	require.NoError(t, oc.Client.Update(ctx, cr))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/v2/service_instances/inst-int-1/last_operation?service_id=def-svc-0001&operation=provision", nil)
	req.Header.Set("X-Broker-API-Version", "2.17")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lo))
	assert.Equal(t, "succeeded", lo.State)

	// 5. Bind reads operator secret
	secret := newSecret("default", "osb-inst-int-1-creds", map[string][]byte{
		"username": []byte("db-user"),
		"password": []byte("db-pass"),
	})
	require.NoError(t, oc.Client.Create(ctx, secret))

	w = putJSON(router, "/v2/service_instances/inst-int-1/service_bindings/bind-int-1", map[string]interface{}{
		"service_id": "def-svc-0001",
		"plan_id":    "def-plan-free",
	})
	require.Equal(t, http.StatusCreated, w.Code)
	var bindResp struct {
		Credentials map[string]string `json:"credentials"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bindResp))
	assert.Equal(t, "db-user", bindResp.Credentials["username"])
	assert.Equal(t, "db-pass", bindResp.Credentials["password"])

	// 6. Unbind + Deprovision
	w = httptest.NewRecorder()
	delReq, _ := http.NewRequest("DELETE", "/v2/service_instances/inst-int-1/service_bindings/bind-int-1?service_id=def-svc-0001&plan_id=def-plan-free", nil)
	delReq.Header.Set("X-Broker-API-Version", "2.17")
	router.ServeHTTP(w, delReq)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	delReq, _ = http.NewRequest("DELETE", "/v2/service_instances/inst-int-1?service_id=def-svc-0001&plan_id=def-plan-free", nil)
	delReq.Header.Set("X-Broker-API-Version", "2.17")
	router.ServeHTTP(w, delReq)
	assert.Equal(t, http.StatusOK, w.Code)

	_, err = oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "osb-inst-int-1")
	assert.Error(t, err, "CR should be gone after deprovision")
}

func TestIntegration_DefinitionUpdateOverHTTP(t *testing.T) {
	router, oc := newDefinitionRouter(t)
	ctx := context.Background()

	// Provision small
	w := provisionJSON(router, "/v2/service_instances/inst-upd-http", map[string]interface{}{
		"service_id": "def-svc-0001",
		"plan_id":    "def-plan-free",
	})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	cr, err := oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "osb-inst-upd-http")
	require.NoError(t, err)

	// Operator fügt ein Feld hinzu, das der Broker nicht verwaltet
	require.NoError(t, unstructured.SetNestedField(cr.Object, "operator-owned", "status", "phase"))
	require.NoError(t, oc.Client.Update(ctx, cr))

	// PATCH auf denselben Plan → No-op: CR darf nicht verändert werden
	patchBody, _ := json.Marshal(map[string]interface{}{
		"service_id":      "def-svc-0001",
		"plan_id":         "def-plan-free",
		"previous_values": map[string]interface{}{"plan_id": "def-plan-free"},
	})
	w = httptest.NewRecorder()
	req, _ := http.NewRequest("PATCH", "/v2/service_instances/inst-upd-http", bytes.NewReader(patchBody))
	req.Header.Set("X-Broker-API-Version", "2.17")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	crAfter, err := oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "osb-inst-upd-http")
	require.NoError(t, err)
	assert.Equal(t, cr.GetResourceVersion(), crAfter.GetResourceVersion(),
		"same plan must not touch CR")
	phase, _, _ := unstructured.NestedString(crAfter.Object, "status", "phase")
	assert.Equal(t, "operator-owned", phase)

	// Unbekannter Parameter → 400
	badBody, _ := json.Marshal(map[string]interface{}{
		"service_id": "def-svc-0001",
		"plan_id":    "def-plan-free",
		"parameters": map[string]interface{}{"evil_key": "x"},
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PATCH", "/v2/service_instances/inst-upd-http", bytes.NewReader(badBody))
	req.Header.Set("X-Broker-API-Version", "2.17")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestIntegration_RebindReadsFreshSecret(t *testing.T) {
	router, oc := newDefinitionRouter(t)
	ctx := context.Background()

	// Secret v1 anlegen
	require.NoError(t, oc.Client.Create(ctx, newSecret("default", "osb-inst-rot-creds", map[string][]byte{
		"password": []byte("old-password"),
	})))

	// Bind #1
	w := putJSON(router, "/v2/service_instances/inst-rot/service_bindings/bind-r1", map[string]interface{}{
		"service_id": "def-svc-0001",
		"plan_id":    "def-plan-free",
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var bindResp struct {
		Credentials map[string]string `json:"credentials"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bindResp))
	assert.Equal(t, "old-password", bindResp.Credentials["password"])

	// Operator rotiert das Secret (Update)
	s, err := oc.GetSecretObj(ctx, "default", "osb-inst-rot-creds")
	require.NoError(t, err)
	s.Data["password"] = []byte("new-password")
	require.NoError(t, oc.Client.Update(ctx, s))

	// Bind #2 nach Rotation → frische Credentials
	w = putJSON(router, "/v2/service_instances/inst-rot/service_bindings/bind-r2", map[string]interface{}{
		"service_id": "def-svc-0001",
		"plan_id":    "def-plan-free",
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bindResp))
	assert.Equal(t, "new-password", bindResp.Credentials["password"],
		"rebind must read the rotated secret, not cache")
}

func newSecret(namespace, name string, data map[string][]byte) *k8scorev1.Secret {
	s := k8scorev1.Secret{}
	s.Name = name
	s.Namespace = namespace
	s.Data = data
	return &s
}
