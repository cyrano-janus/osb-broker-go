package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OSB 2.17 verlangt `X-Broker-API-Version` in jeder Anfrage an die API und
// `412 Precondition Failed`, wenn der Broker die genannte Version nicht
// bedienen kann.
//
// Der Broker nahm den Header bislang entgegen und setzte bei Abwesenheit still
// die eigene Version ein. Das ist die bequeme Variante und die falsche: ein
// Aufrufer, der den Header vergisst, bekommt Antworten nach einer Version, auf
// die er sich nie geeinigt hat - und merkt es erst, wenn sich eine Bedeutung
// aendert.

func versionedRequest(r *gin.Engine, method, path, version string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, nil)
	if version != "" {
		req.Header.Set("X-Broker-API-Version", version)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestAPIVersion_FehlenderHeaderIst412(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := versionedRequest(router, "GET", "/v2/catalog", "")

	require.Equal(t, http.StatusPreconditionFailed, w.Code, w.Body.String())

	// Der Fehlerkoerper ist Teil der Zusage: OSB verlangt `error` und
	// `description`, damit eine Plattform die Ursache anzeigen kann.
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "PreconditionFailed", body["error"])
	assert.Contains(t, body["description"], "X-Broker-API-Version")
}

func TestAPIVersion_UnterstuetzteVersionenGehenDurch(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	for _, v := range []string{"2.17", "2.13", "2.0"} {
		w := versionedRequest(router, "GET", "/v2/catalog", v)
		assert.Equal(t, http.StatusOK, w.Code, "Version %q muss bedienbar sein", v)
	}
}

// Eine andere Hauptversion ist eine andere Schnittstelle. Sie zu bedienen,
// als waere sie dieselbe, ist genau der Fall, den 412 verhindern soll.
func TestAPIVersion_FremdeHauptversionIst412(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	for _, v := range []string{"3.0", "1.9", "0.1"} {
		w := versionedRequest(router, "GET", "/v2/catalog", v)
		assert.Equal(t, http.StatusPreconditionFailed, w.Code,
			"Hauptversion aus %q ist nicht 2", v)
	}
}

func TestAPIVersion_UnlesbarerWertIst412(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	for _, v := range []string{"zwei", "2", "2.x", "v2.17", ""} {
		w := versionedRequest(router, "GET", "/v2/catalog", v)
		assert.Equal(t, http.StatusPreconditionFailed, w.Code,
			"%q ist keine Versionsangabe der Form major.minor", v)
	}
}

// Eine neuere Nebenversion darf nicht abgelehnt werden: die Plattform nennt,
// was sie zu sprechen bereit ist, und ein Broker, der weniger kann, antwortet
// mit dem, was er kann. Ein 412 waere hier eine Ablehnung, die die
// Spezifikation nicht verlangt.
func TestAPIVersion_NeuereNebenversionWirdBedient(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	w := versionedRequest(router, "GET", "/v2/catalog", "2.99")
	assert.Equal(t, http.StatusOK, w.Code)
}

// Die freien Pfade bleiben frei. Ein Liveness-Probe schickt keinen
// OSB-Header, und /metrics wird von Prometheus abgeholt.
func TestAPIVersion_FreiePfadeBrauchenKeinenHeader(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	for _, p := range []string{"/healthz", "/openapi.yaml", "/schemas/service-definition.schema.json"} {
		w := versionedRequest(router, "GET", p, "")
		assert.NotEqual(t, http.StatusPreconditionFailed, w.Code,
			"%s muss ohne Versionsheader erreichbar bleiben", p)
	}

	// /metrics gibt es nur, wenn Metriken eingeschaltet sind - deshalb ein
	// eigener Router. Ohne ihn liefe die Anfrage in die Middleware-Kette
	// eines nicht registrierten Pfades und der 412 saehe wie ein Befund aus.
	mRouter, _ := newMetricsTestRouter(t)
	w := versionedRequest(mRouter, "GET", "/metrics", "")
	assert.Equal(t, http.StatusOK, w.Code, "/metrics muss ohne Versionsheader erreichbar bleiben")
}

// Die Pruefung gilt fuer jeden API-Pfad, nicht nur den Katalog.
func TestAPIVersion_GiltFuerAlleAPIPfade(t *testing.T) {
	router, _ := newDefinitionRouter(t)

	for _, tc := range []struct{ method, path string }{
		{"GET", "/v2/catalog"},
		{"PUT", "/v2/service_instances/x"},
		{"PATCH", "/v2/service_instances/x"},
		{"DELETE", "/v2/service_instances/x"},
		{"GET", "/v2/service_instances/x"},
		{"GET", "/v2/service_instances/x/last_operation"},
		{"PUT", "/v2/service_instances/x/service_bindings/y"},
		{"DELETE", "/v2/service_instances/x/service_bindings/y"},
		{"GET", "/v2/service_instances/x/service_bindings/y"},
	} {
		w := versionedRequest(router, tc.method, tc.path, "")
		assert.Equal(t, http.StatusPreconditionFailed, w.Code,
			"%s %s ohne Versionsheader", tc.method, tc.path)
	}
}
