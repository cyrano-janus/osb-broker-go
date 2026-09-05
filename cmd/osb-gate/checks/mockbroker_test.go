package checks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ein Gate, dessen Pruefungen wirkungslos sind, ist von einem gruenen nicht zu
// unterscheiden. Genau dieser Zustand hat den frueheren Doppelpfad des Brokers
// verdeckt: die Auswahl traf immer das Demo-Angebot, also lief der Audit gegen
// eine Attrappe und meldete gruen.
//
// Diese Suite belegt dreierlei:
//   - gegen einen konformen Broker schlaegt nichts fehl,
//   - je Mutation - genau eine verletzte Regel - schlaegt genau die zustaendige
//     Pruefung an,
//   - gegen einen geschlossenen Server besteht nichts.
//
// Die dritte Zusage ist die wichtigste. Eine Negativpruefung, die einen
// Transportfehler als "der Broker hat abgelehnt" liest, meldet einen
// unerreichbaren Broker als konform.

// -----------------------------------------------------------------------
// Der Mock-Broker
// -----------------------------------------------------------------------

// mutation beschreibt genau eine Abweichung vom konformen Verhalten. Der
// Nullwert ist der konforme Broker.
type mutation struct {
	noAuthChallenge      bool // 401 ohne WWW-Authenticate
	authNotEnforced      bool // Katalog auch ohne Zugangsdaten
	healthzProtected     bool // /healthz verlangt Zugangsdaten
	serviceNoDesc        bool // Service ohne description
	duplicatePlanID      bool // zwei Plaene mit derselben id
	provisionStatus      int  // statt 202
	reprovisionStatus    int  // statt 200 bei identischer Wiederholung
	badRequestStatus     int  // statt 400 bei fehlender service_id/plan_id
	unknownServiceCode   int  // statt 400 bei unbekannter service_id
	deprovisionGoneCode  int  // statt 410 bei unbekannter Instanz
	bindStatus           int  // statt 201
	bindNoCreds          bool // 201, aber ohne credentials
	rebindStatus         int  // statt 200 bei Wiederholung
	bindUnknownInstance  int  // statt 404
	getInstanceStatus    int  // statt 200
	getBindingStatus     int  // statt 200
	lastOperationNoState bool
	updateStatus         int  // statt 200
	updateNeedsPlanID    bool // PATCH ohne plan_id wird abgelehnt
	updateDropsParams    bool // PATCH nimmt parameters an und verwirft sie
	unbindStatus         int  // statt 200
	bindingTypeNotString bool // credentials.type ist keine Zeichenkette
}

const (
	mockUser        = "broker-user"
	mockPass        = "s3cret"
	mockDemoService = "service-1"
	mockRealService = "real-service-0001"
	mockPlan        = "real-plan-small"
	mockPlanOther   = "real-plan-large"
)

type mockBroker struct {
	*httptest.Server
	mu        sync.Mutex
	instances map[string][2]string              // id -> {service_id, plan_id}
	params    map[string]map[string]interface{} // id -> zuletzt gesetzte parameters
	bindings  map[string]string                 // bindingID -> instanceID
	mut       mutation
}

func newMockBroker(m mutation) *mockBroker {
	b := &mockBroker{
		instances: map[string][2]string{},
		params:    map[string]map[string]interface{}{},
		bindings:  map[string]string{},
		mut:       m,
	}
	b.Server = httptest.NewServer(http.HandlerFunc(b.route))
	return b
}

func (b *mockBroker) authOK(r *http.Request) bool {
	u, p, ok := r.BasicAuth()
	return ok && u == mockUser && p == mockPass
}

func (b *mockBroker) route(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")

	if path == "healthz" {
		if b.mut.healthzProtected && !b.authOK(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if !b.mut.authNotEnforced && !b.authOK(r) {
		if !b.mut.noAuthChallenge {
			w.Header().Set("WWW-Authenticate", `Basic realm="osb"`)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 2 && parts[1] == "catalog":
		b.catalog(w)
	case len(parts) == 3 && parts[1] == "service_instances":
		b.instance(w, r, parts[2])
	case len(parts) == 4 && parts[1] == "service_instances" && parts[3] == "last_operation":
		b.lastOperation(w, r, parts[2])
	case len(parts) == 5 && parts[1] == "service_instances" && parts[3] == "service_bindings":
		b.binding(w, r, parts[2], parts[4])
	case len(parts) == 6 && parts[1] == "service_instances" && parts[3] == "service_bindings" && parts[5] == "last_operation":
		writeJSON(w, 200, map[string]interface{}{"state": "succeeded"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (b *mockBroker) catalog(w http.ResponseWriter) {
	desc := "a real service backed by an operator"
	if b.mut.serviceNoDesc {
		desc = ""
	}
	secondPlanID := mockPlanOther
	if b.mut.duplicatePlanID {
		secondPlanID = mockPlan
	}
	writeJSON(w, 200, map[string]interface{}{"services": []map[string]interface{}{
		// Das Demo-Angebot steht bewusst vorn: der Audit muss es
		// ueberspringen und den echten Service waehlen.
		{
			"id": mockDemoService, "name": "demo", "description": "demo offering",
			"bindable": true,
			"plans":    []map[string]interface{}{{"id": "demo-plan", "name": "free", "description": "d"}},
		},
		{
			"id": mockRealService, "name": "real", "description": desc,
			"bindable": true, "plan_updateable": true,
			"plans": []map[string]interface{}{
				{"id": mockPlan, "name": "small", "description": "s"},
				{"id": secondPlanID, "name": "large", "description": "l"},
			},
		},
	}})
}

func (b *mockBroker) instance(w http.ResponseWriter, r *http.Request, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch r.Method {
	case "PUT":
		var req struct {
			ServiceID  string                 `json:"service_id"`
			PlanID     string                 `json:"plan_id"`
			Parameters map[string]interface{} `json:"parameters"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req.ServiceID == "" || req.PlanID == "" {
			writeErr(w, orDefault(b.mut.badRequestStatus, 400), "BadRequest", "service_id and plan_id are required")
			return
		}
		if req.ServiceID != mockRealService && req.ServiceID != mockDemoService {
			writeErr(w, orDefault(b.mut.unknownServiceCode, 400), "BadRequest", "unknown service_id")
			return
		}
		if known, ok := b.instances[id]; ok {
			if known[0] != req.ServiceID || known[1] != req.PlanID {
				writeErr(w, 409, "Conflict", "instance exists with other attributes")
				return
			}
			writeJSON(w, orDefault(b.mut.reprovisionStatus, 200), map[string]interface{}{
				"dashboard_url": "https://example.invalid/" + id})
			return
		}
		if r.URL.Query().Get("accepts_incomplete") != "true" {
			writeErr(w, 422, "AsyncRequired", "this plan requires client support for async")
			return
		}
		b.instances[id] = [2]string{req.ServiceID, req.PlanID}
		b.params[id] = req.Parameters
		writeJSON(w, orDefault(b.mut.provisionStatus, 202), map[string]interface{}{
			"dashboard_url": "https://example.invalid/" + id, "operation": "provision"})

	case "PATCH":
		inst, ok := b.instances[id]
		if !ok {
			writeErr(w, 404, "NotFound", "instance not found")
			return
		}
		var req struct {
			PlanID     string                 `json:"plan_id"`
			Parameters map[string]interface{} `json:"parameters"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.PlanID == "" && b.mut.updateNeedsPlanID {
			writeErr(w, 400, "BadRequest", "plan_id is required")
			return
		}
		if req.PlanID != "" {
			b.instances[id] = [2]string{inst[0], req.PlanID}
		}
		if len(req.Parameters) > 0 && !b.mut.updateDropsParams {
			if b.params[id] == nil {
				b.params[id] = map[string]interface{}{}
			}
			for k, v := range req.Parameters {
				b.params[id][k] = v
			}
		}
		writeJSON(w, orDefault(b.mut.updateStatus, 200), map[string]interface{}{"operation": "update"})

	case "GET":
		inst, ok := b.instances[id]
		if !ok {
			writeErr(w, 404, "NotFound", "instance not found")
			return
		}
		p := b.params[id]
		if p == nil {
			p = map[string]interface{}{}
		}
		body := map[string]interface{}{"service_id": inst[0], "plan_id": inst[1], "parameters": p}
		writeJSON(w, orDefault(b.mut.getInstanceStatus, 200), body)

	case "DELETE":
		if _, ok := b.instances[id]; !ok {
			writeErr(w, orDefault(b.mut.deprovisionGoneCode, 410), "Gone", "instance not found")
			return
		}
		delete(b.instances, id)
		writeJSON(w, 200, map[string]interface{}{})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (b *mockBroker) lastOperation(w http.ResponseWriter, _ *http.Request, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.instances[id]; !ok {
		writeErr(w, 410, "Gone", "instance not found")
		return
	}
	if b.mut.lastOperationNoState {
		writeJSON(w, 200, map[string]interface{}{"description": "fertig"})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"state": "succeeded", "description": "ready"})
}

func (b *mockBroker) binding(w http.ResponseWriter, r *http.Request, instanceID, bindingID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch r.Method {
	case "PUT":
		var req struct {
			ServiceID string `json:"service_id"`
			PlanID    string `json:"plan_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ServiceID == "" || req.PlanID == "" {
			writeErr(w, orDefault(b.mut.badRequestStatus, 400), "BadRequest", "service_id and plan_id are required")
			return
		}
		if _, ok := b.instances[instanceID]; !ok {
			writeErr(w, orDefault(b.mut.bindUnknownInstance, 404), "NotFound", "instance not found")
			return
		}
		body := map[string]interface{}{"credentials": b.creds()}
		if b.mut.bindNoCreds {
			body = map[string]interface{}{}
		}
		if _, exists := b.bindings[bindingID]; exists {
			writeJSON(w, orDefault(b.mut.rebindStatus, 200), body)
			return
		}
		b.bindings[bindingID] = instanceID
		writeJSON(w, orDefault(b.mut.bindStatus, 201), body)

	case "GET":
		if _, ok := b.bindings[bindingID]; !ok {
			writeErr(w, 404, "NotFound", "binding not found")
			return
		}
		writeJSON(w, orDefault(b.mut.getBindingStatus, 200),
			map[string]interface{}{"credentials": b.creds()})

	case "DELETE":
		if _, ok := b.bindings[bindingID]; !ok {
			writeErr(w, 410, "Gone", "binding not found")
			return
		}
		delete(b.bindings, bindingID)
		writeJSON(w, orDefault(b.mut.unbindStatus, 200), map[string]interface{}{})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (b *mockBroker) creds() map[string]interface{} {
	var typ interface{} = "postgresql"
	if b.mut.bindingTypeNotString {
		typ = 42
	}
	return map[string]interface{}{
		"type": typ, "host": "db.invalid", "port": "5432",
		"username": "app", "password": "x", "database": "app", "uri": "postgresql://app:x@db.invalid:5432/app",
	}
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, kind, desc string) {
	writeJSON(w, status, map[string]string{"error": kind, "description": desc})
}

// -----------------------------------------------------------------------
// Die Suite
// -----------------------------------------------------------------------

func runAgainst(url string) *Report {
	return RunReport(Config{
		BaseURL:  url,
		User:     mockUser,
		Pass:     mockPass,
		IDPrefix: "mut",
		// Kurze Fristen: die Suite prueft Regeln, nicht Geduld. Ohne sie
		// wartet die Mutation "last_operation ohne state" vier Minuten.
		Timeout:      5,
		AsyncTimeout: 2,
	})
}

func withBroker(t *testing.T, m mutation) *Report {
	t.Helper()
	b := newMockBroker(m)
	t.Cleanup(b.Close)
	return runAgainst(b.URL)
}

func failedNames(r *Report) string { return strings.Join(r.Failed, ", ") }

func TestMock_KonformerBrokerHatKeineFehlschlaege(t *testing.T) {
	r := withBroker(t, mutation{})
	assert.Zero(t, r.Failures(),
		"gegen einen konformen Broker darf nichts anschlagen, es schlug an: %s", failedNames(r))
	assert.NotEmpty(t, r.Passed, "es muss ueberhaupt etwas geprueft worden sein")
}

// Ohne diese Zusage prueft der Audit die Attrappe statt der Engine - genau der
// Fehler, der den frueheren Doppelpfad so lange verdeckt hat.
func TestMock_DerAuditWaehltNichtDenErstenKatalogeintrag(t *testing.T) {
	b := newMockBroker(mutation{})
	t.Cleanup(b.Close)

	r := runAgainst(b.URL)
	require.Zero(t, r.Failures(), failedNames(r))

	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Empty(t, b.instances, "der Audit muss hinter sich aufraeumen")
}

func TestMock_JedeMutationWirdBemerkt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mut   mutation
		check string
	}{
		{"401 ohne WWW-Authenticate", mutation{noAuthChallenge: true}, "auth-enforcement"},
		{"Katalog auch ohne Zugangsdaten", mutation{authNotEnforced: true}, "auth-enforcement"},
		{"/healthz verlangt Zugangsdaten", mutation{healthzProtected: true}, "auth-enforcement"},
		{"Service ohne description", mutation{serviceNoDesc: true}, "catalog-conformance"},
		{"zwei Plaene mit derselben id", mutation{duplicatePlanID: true}, "catalog-conformance"},
		{"unbekannte service_id ergibt 500", mutation{unknownServiceCode: 500}, "error-mapping"},
		{"Deprovision einer Unbekannten ergibt 200", mutation{deprovisionGoneCode: 200}, "error-mapping"},
		{"Provision einer neuen Instanz antwortet 200", mutation{provisionStatus: 200}, "lifecycle-provision"},
		{"Wiederholtes Provision antwortet 201", mutation{reprovisionStatus: 201}, "lifecycle-provision-idempotent"},
		{"fehlende service_id ergibt 500", mutation{badRequestStatus: 500}, "lifecycle-provision-missing-service"},
		{"last_operation ohne state", mutation{lastOperationNoState: true}, "lifecycle-provision-async"},
		{"Bind antwortet 200 statt 201", mutation{bindStatus: 200}, "lifecycle-bind"},
		{"Bind ohne credentials", mutation{bindNoCreds: true}, "lifecycle-bind"},
		{"Wiederholtes Bind antwortet 201", mutation{rebindStatus: 201}, "lifecycle-bind-idempotent"},
		{"Bind auf unbekannte Instanz ergibt 200", mutation{bindUnknownInstance: 200}, "lifecycle-bind-nonexistent-instance"},
		{"GET instance antwortet 404", mutation{getInstanceStatus: 404}, "fetch-get-instance"},
		{"GET binding antwortet 404", mutation{getBindingStatus: 404}, "fetch-get-binding"},
		{"Update antwortet 500", mutation{updateStatus: 500}, "update-instance"},
		{"PATCH ohne plan_id wird abgelehnt", mutation{updateNeedsPlanID: true}, "update-parameters"},
		{"PATCH nimmt parameters an und verwirft sie", mutation{updateDropsParams: true}, "update-parameters"},
		{"Unbind antwortet 500", mutation{unbindStatus: 500}, "lifecycle-unbind"},
		{"credentials.type ist eine Zahl", mutation{bindingTypeNotString: true}, "service-binding-spec"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := withBroker(t, tc.mut)
			require.NotZero(t, r.Failures(),
				"die Verletzung blieb unbemerkt - %d Pruefungen bestanden", len(r.Passed))
			assert.True(t, r.HasFailed(tc.check),
				"es schlug etwas fehl, aber nicht %q: %s", tc.check, failedNames(r))
		})
	}
}

// Der wichtigste Fall. Eine Negativpruefung, die einen Transportfehler als
// "der Broker hat abgelehnt" liest, meldet einen unerreichbaren Broker als
// konform.
func TestMock_GeschlossenerServerLaesstNichtsDurchgehen(t *testing.T) {
	b := newMockBroker(mutation{})
	url := b.URL
	b.Close()

	r := runAgainst(url)

	assert.Empty(t, r.Passed,
		"%d Pruefungen gelten als bestanden, obwohl der Broker nicht erreichbar war: %s",
		len(r.Passed), strings.Join(r.Passed, ", "))
	assert.NotZero(t, r.Failures(), "kein einziger Fehlschlag gegen einen unerreichbaren Broker")
}

var _ = fmt.Sprintf
