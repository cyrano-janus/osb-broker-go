package handlers

import (
	"bytes"
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
    plans:
      - id: def-plan-free
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
        name: {{ .instanceID }}
      spec:
        size: {{ .plan.size }}
  readiness:
    statusJSONPath: 'status.phase'
    expectedValue: "Running"
  bind:
    credentialsFromSecret: "{{ .instanceID }}-creds"
`

func newDefinitionRouter(t *testing.T) (*gin.Engine, *definition.OperatorClient) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sd, err := definition.Parse([]byte(testDefYAML))
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	ctrlClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	oc := definition.NewOperatorClient(ctrlClient)
	engine := definition.NewEngine(oc, sd)

	b := broker.New(store.NewInMemoryStore(), nil)
	h := New(b)
	h.SetEngine(&EngineHolder{Engine: engine, Op: oc})

	return h.SetupRouter(), oc
}

func putJSON(router *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", path, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
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
	w = putJSON(router, "/v2/service_instances/inst-int-1", map[string]interface{}{
		"service_id": "def-svc-0001",
		"plan_id":    "def-plan-free",
		"context":    map[string]interface{}{"platform": "cloudfoundry"},
	})
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	// CR exists with rendered spec
	cr, err := oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "inst-int-1")
	require.NoError(t, err)
	size, _, _ := unstructured.NestedString(cr.Object, "spec", "size")
	assert.Equal(t, "small", size)

	// 3. LastOperation: in progress (no status yet)
	w = httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v2/service_instances/inst-int-1/last_operation?service_id=def-svc-0001&operation=provision", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var lo struct {
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lo))
	assert.Equal(t, "in progress", lo.State)

	// 4. Operator marks CR Running -> succeeded
	cr, err = oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "inst-int-1")
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedField(cr.Object, "Running", "status", "phase"))
	require.NoError(t, oc.Client.Update(ctx, cr))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/v2/service_instances/inst-int-1/last_operation?service_id=def-svc-0001&operation=provision", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lo))
	assert.Equal(t, "succeeded", lo.State)

	// 5. Bind reads operator secret
	secret := newSecret("default", "inst-int-1-creds", map[string][]byte{
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
	router.ServeHTTP(w, delReq)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	delReq, _ = http.NewRequest("DELETE", "/v2/service_instances/inst-int-1?service_id=def-svc-0001&plan_id=def-plan-free", nil)
	router.ServeHTTP(w, delReq)
	assert.Equal(t, http.StatusOK, w.Code)

	_, err = oc.GetCR(ctx, "test.example.com/v1", "Database", "default", "inst-int-1")
	assert.Error(t, err, "CR should be gone after deprovision")
}

func newSecret(namespace, name string, data map[string][]byte) *k8scorev1.Secret {
	s := k8scorev1.Secret{}
	s.Name = name
	s.Namespace = namespace
	s.Data = data
	return &s
}
