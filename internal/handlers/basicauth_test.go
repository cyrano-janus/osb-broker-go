package handlers

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// newAuthTestRouter returns a router backed by a real broker (catalog
// endpoint needs it) with the given Basic Auth credentials configured.
func newAuthTestRouter(user, pass string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	b := broker.New(nil)
	h := New(b)
	h.SetBasicAuthCredentials(user, pass)
	return h.SetupRouter()
}

// perform schickt eine Anfrage wie eine echte Plattform: mit
// X-Broker-API-Version. Ohne den Header antwortet der Broker seit der
// Versionsaushandlung mit 412, und jeder Test pruefte nur noch die
// Middleware. Wer den Header gezielt weglassen will, uebergibt ihn leer -
// siehe apiversion_test.go.
func perform(r *gin.Engine, path string, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	req.Header.Set("X-Broker-API-Version", "2.17")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestBasicAuth_ValidCredentialsPass(t *testing.T) {
	router := newAuthTestRouter("broker-user", "broker-secret")

	w := perform(router, "/v2/catalog", map[string]string{
		"Authorization": basicAuthHeader("broker-user", "broker-secret"),
	})
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBasicAuth_MissingHeaderRejected(t *testing.T) {
	router := newAuthTestRouter("broker-user", "broker-secret")

	w := perform(router, "/v2/catalog", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, `Basic realm="osb-broker"`, w.Header().Get("WWW-Authenticate"))
	assert.Contains(t, w.Body.String(), "Unauthorized")
}

func TestBasicAuth_WrongCredentialsRejected(t *testing.T) {
	router := newAuthTestRouter("broker-user", "broker-secret")

	w := perform(router, "/v2/catalog", map[string]string{
		"Authorization": basicAuthHeader("broker-user", "wrong"),
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBasicAuth_MalformedHeaderRejected(t *testing.T) {
	router := newAuthTestRouter("broker-user", "broker-secret")

	for _, hdr := range []string{"Bearer abc123", "Basic !!!not-base64!!!", "basic " + base64.StdEncoding.EncodeToString([]byte("no-colon"))} {
		w := perform(router, "/v2/catalog", map[string]string{"Authorization": hdr})
		assert.Equal(t, http.StatusUnauthorized, w.Code, "header: %s", hdr)
	}
}

func TestBasicAuth_HealthzExempt(t *testing.T) {
	router := newAuthTestRouter("broker-user", "broker-secret")

	// Kubernetes probes cannot carry credentials.
	w := perform(router, "/healthz", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBasicAuth_DisabledWhenNoCredentialsSet(t *testing.T) {
	// Backwards compatibility: without configured credentials the broker
	// behaves as before (open). Deployments should always set credentials.
	router := newAuthTestRouter("", "")

	w := perform(router, "/v2/catalog", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}
