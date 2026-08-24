package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRouter() (*gin.Engine, *broker.Broker) {
	serviceStore := store.NewInMemoryStore()
	b := broker.New(serviceStore, nil)
	h := New(b)
	router := h.SetupRouter()
	return router, b
}

func TestGetCatalog(t *testing.T) {
	router, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v2/catalog", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var catalog broker.Catalog
	err := json.Unmarshal(w.Body.Bytes(), &catalog)
	require.NoError(t, err)
	assert.Len(t, catalog.Services, 2)
}

func TestProvisionServiceInstance(t *testing.T) {
	router, b := setupTestRouter()

	reqBody := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify instance was created via the exported accessor
	_, err := b.GetInstance("instance-1")
	assert.NoError(t, err)
}

func TestProvisionServiceInstanceMissingServiceID(t *testing.T) {
	router, _ := setupTestRouter()

	reqBody := broker.ProvisionRequest{
		PlanID: "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProvisionServiceInstanceInvalidService(t *testing.T) {
	router, _ := setupTestRouter()

	reqBody := broker.ProvisionRequest{
		ServiceID: "invalid-service",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// OSB spec: invalid service_id/plan_id -> 400 Bad Request
	// (see openservicebrokerapi/servicebroker#678)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeprovisionServiceInstance(t *testing.T) {
	router, b := setupTestRouter()

	// First provision
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Then deprovision
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/v2/service_instances/instance-1?service_id=service-1&plan_id=plan-free", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify instance was deleted via the exported accessor
	_, err := b.GetInstance("instance-1")
	assert.Error(t, err)
}

func TestBindServiceInstance(t *testing.T) {
	router, b := setupTestRouter()

	// First provision
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Then bind
	bindReq := broker.BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ = json.Marshal(bindReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/v2/service_instances/instance-1/service_bindings/binding-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response broker.BindResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.NotNil(t, response.Credentials)

	// Verify binding was created via the exported accessor
	_, err = b.GetBinding("instance-1", "binding-1")
	assert.NoError(t, err)
}

func TestUnbindServiceInstance(t *testing.T) {
	router, b := setupTestRouter()

	// Provision and bind
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	bindReq := broker.BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ = json.Marshal(bindReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/v2/service_instances/instance-1/service_bindings/binding-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Then unbind
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/v2/service_instances/instance-1/service_bindings/binding-1?service_id=service-1&plan_id=plan-free", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify binding was deleted via the exported accessor
	_, err := b.GetBinding("instance-1", "binding-1")
	assert.Error(t, err)
}

func TestGetServiceInstance(t *testing.T) {
	router, _ := setupTestRouter()

	// First provision
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Then get
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/v2/service_instances/instance-1", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response broker.GetInstanceResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "service-1", response.ServiceID)
}

func TestGetServiceInstanceNotFound(t *testing.T) {
	router, _ := setupTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v2/service_instances/nonexistent", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetBinding(t *testing.T) {
	router, _ := setupTestRouter()

	// Provision and bind
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	bindReq := broker.BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ = json.Marshal(bindReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/v2/service_instances/instance-1/service_bindings/binding-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Then get binding
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/v2/service_instances/instance-1/service_bindings/binding-1", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response broker.GetBindingResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.NotNil(t, response.Credentials)
}

func TestUpdateServiceInstance(t *testing.T) {
	router, b := setupTestRouter()

	// First provision
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Then update
	updateReq := broker.UpdateInstanceRequest{
		ServiceID: "service-1",
		PlanID:    "plan-premium",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
		PreviousValues: broker.PreviousValues{
			PlanID: "plan-free",
		},
	}
	body, _ = json.Marshal(updateReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PATCH", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify plan was updated via the exported accessor
	instance, err := b.GetInstance("instance-1")
	require.NoError(t, err)
	assert.Equal(t, "plan-premium", instance.PlanID)
}

func TestGetLastOperation(t *testing.T) {
	router, _ := setupTestRouter()

	// First provision
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Then get last operation
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/v2/service_instances/instance-1/last_operation", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response broker.LastOperationResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", response.State)
}

func TestGetLastBindingOperation(t *testing.T) {
	router, _ := setupTestRouter()

	// Provision and bind
	provReq := broker.ProvisionRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ := json.Marshal(provReq)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/v2/service_instances/instance-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	bindReq := broker.BindRequest{
		ServiceID: "service-1",
		PlanID:    "plan-free",
		AppGUID:   "app-123",
		Context: broker.Context{
			Platform: "cloudfoundry",
		},
	}
	body, _ = json.Marshal(bindReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/v2/service_instances/instance-1/service_bindings/binding-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Then get last binding operation
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/v2/service_instances/instance-1/service_bindings/binding-1/last_operation", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response broker.LastOperationResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", response.State)
}