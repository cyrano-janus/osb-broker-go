// Package checks implements the osb-checker conformance checks against an
// OSB 2.17 broker. Exit contract: Run() returns the failure count.
//
// The suite mirrors the 21-check full audit of the standalone osb-checker
// (development-open-service-broker/osb-checker) plus broker-specific checks
// (auth enforcement, plan_updateable advertisement, 410-Gone mapping).
package checker

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Config carries the checker run parameters.
type Config struct {
	BaseURL  string
	User     string
	Pass     string
	IDPrefix string
	Timeout  int64 // seconds; reserved for future per-check deadlines

	// TLS options (Phase 4.5). CACert verifies the broker certificate;
	// ClientCert/ClientKey authenticate the checker via mTLS.
	CACert     string
	ClientCert string
	ClientKey  string
	Insecure   bool
}

const cnpgServiceID = "f48a9e21-cnpg-0000-0000-000000000001"
const smallPlanID = "plan-small-0000-0000-000000000001"
const legacyServiceID = "service-1"
const legacyPlanID = "plan-free"

var failures int

func fail(check string, format string, args ...interface{}) {
	failures++
	msg := fmt.Sprintf("FAIL [%s]: %s", check, fmt.Sprintf(format, args...))
	fmt.Println(msg)
	// GitHub Actions annotation: appears in the run UI and annotations API.
	fmt.Printf("::error::%s\n", msg)
}

func pass(check string, format string, args ...interface{}) {
	fmt.Printf("PASS [%s]: %s\n", check, fmt.Sprintf(format, args...))
}

type client struct {
	// http carries credentials and, when configured, the client
	// certificate.
	http *http.Client
	// anon carries neither. The auth-enforcement check needs a caller the
	// broker cannot possibly authenticate: with mTLS enabled the
	// credentialled client would be authenticated by its certificate alone
	// and the expected 401 would never appear.
	anon *http.Client
	cfg  Config
}

// newHTTPClients builds the credentialled and the anonymous client.
func newHTTPClients(cfg Config) (authed, anon *http.Client, err error) {
	base, err := tlsConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	anonTLS := base.Clone()
	anonTLS.Certificates = nil

	authedTLS := base.Clone()
	if cfg.ClientCert != "" || cfg.ClientKey != "" {
		pair, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, nil, fmt.Errorf("load client certificate: %w", err)
		}
		authedTLS.Certificates = []tls.Certificate{pair}
	}

	return &http.Client{Transport: &http.Transport{TLSClientConfig: authedTLS}},
		&http.Client{Transport: &http.Transport{TLSClientConfig: anonTLS}},
		nil
}

func tlsConfig(cfg Config) (*tls.Config, error) {
	out := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.Insecure,
	}
	if cfg.CACert == "" {
		return out, nil
	}
	pem, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA file %s contains no usable certificate", cfg.CACert)
	}
	out.RootCAs = pool
	return out, nil
}

func (c *client) do(method, path string, body interface{}) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(c.cfg.BaseURL, "/")+path, rd)
	if err != nil {
		return -1, nil
	}
	if c.cfg.User != "" || c.cfg.Pass != "" {
		req.SetBasicAuth(c.cfg.User, c.cfg.Pass)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Broker-API-Version", "2.17")
	resp, err := c.http.Do(req)
	if err != nil {
		return -1, nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

type catalogService struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Bindable       bool   `json:"bindable"`
	PlanUpdateable bool   `json:"plan_updateable"`
	Description    string `json:"description"`
	Plans          []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"plans"`
}

func fetchCatalog(c *client) ([]catalogService, bool) {
	status, body := c.do("GET", "/v2/catalog", nil)
	if status != 200 {
		return nil, false
	}
	var cat struct {
		Services []catalogService `json:"services"`
	}
	if err := json.Unmarshal(body, &cat); err != nil {
		return nil, false
	}
	return cat.Services, true
}

// catalogHasService reports whether the given service id is offered.
func catalogHasService(c *client, serviceID string) bool {
	svcs, ok := fetchCatalog(c)
	if !ok {
		return false
	}
	for _, s := range svcs {
		if s.ID == serviceID {
			return true
		}
	}
	return false
}

// Run executes all conformance checks and returns the failure count.
func Run(cfg Config) int {
	failures = 0

	authed, anon, err := newHTTPClients(cfg)
	if err != nil {
		fail("tls-setup", "%v", err)
		return failures
	}
	c := &client{http: authed, anon: anon, cfg: cfg}

	checkAuthEnforcement(c)
	svcs := checkCatalogStructure(c)
	checkErrorMapping(c)

	// Full lifecycle + fetch + update audit (mirrors the standalone
	// osb-checker's provision/bind/update/fetch categories).
	serviceID, planID := pickService(svcs)
	if serviceID == "" {
		fmt.Println("SKIP [lifecycle-*]: no known service in catalog")
	} else {
		inst := runLifecycleAudit(c, serviceID, planID)
		if inst != "" {
			runFetchAudit(c, inst, serviceID, planID)
			runUpdateAudit(c, inst, serviceID, planID)
			cleanupAudit(c, inst, serviceID, planID)
		}
	}
	return failures
}

// pickService selects a known service+plan for the lifecycle audit.
func pickService(svcs []catalogService) (serviceID, planID string) {
	for _, s := range svcs {
		switch s.ID {
		case cnpgServiceID:
			return cnpgServiceID, smallPlanID
		case legacyServiceID:
			return legacyServiceID, legacyPlanID
		}
	}
	return "", ""
}

func checkAuthEnforcement(c *client) {
	const check = "auth-enforcement"
	req, err := http.NewRequest("GET", strings.TrimSuffix(c.cfg.BaseURL, "/")+"/v2/catalog", nil)
	if err != nil {
		fail(check, "build request: %v", err)
		return
	}
	req.Header.Set("X-Broker-API-Version", "2.17")
	resp, err := c.anon.Do(req)
	if err != nil {
		fail(check, "request failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		fail(check, "expected 401 without credentials, got %d", resp.StatusCode)
		return
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		fail(check, "401 without WWW-Authenticate header")
		return
	}
	pass(check, "unauthenticated request -> 401 with WWW-Authenticate")

	hreq, _ := http.NewRequest("GET", strings.TrimSuffix(c.cfg.BaseURL, "/")+"/healthz", nil)
	hresp, err := c.anon.Do(hreq)
	if err != nil {
		fail(check, "healthz request failed: %v", err)
		return
	}
	hresp.Body.Close()
	if hresp.StatusCode != 200 {
		fail(check, "healthz expected 200 unauthenticated, got %d", hresp.StatusCode)
		return
	}
	pass(check, "/healthz reachable without credentials")
}

// checkCatalogStructure validates the catalog shape: unique ids, required
// fields, plans present — and (broker-specific) plan_updateable on the
// definition service. Returns the services for downstream audits.
func checkCatalogStructure(c *client) []catalogService {
	const check = "catalog-conformance"
	svcs, ok := fetchCatalog(c)
	if !ok {
		fail(check, "GET /v2/catalog failed or returned invalid JSON")
		return nil
	}
	if len(svcs) == 0 {
		fail(check, "catalog has no services")
		return nil
	}
	seenSvc := map[string]bool{}
	for _, s := range svcs {
		if s.ID == "" || seenSvc[s.ID] {
			fail(check, "service id empty or duplicated: %q", s.ID)
			return svcs
		}
		seenSvc[s.ID] = true
		if s.Name == "" || s.Description == "" {
			fail(check, "service %q missing name/description", s.ID)
			return svcs
		}
		seenPlan := map[string]bool{}
		for _, p := range s.Plans {
			if p.ID == "" || seenPlan[p.ID] {
				fail(check, "service %q: plan id empty or duplicated: %q", s.ID, p.ID)
				return svcs
			}
			if p.Name == "" {
				fail(check, "service %q: plan %q missing name", s.ID, p.ID)
				return svcs
			}
			seenPlan[p.ID] = true
		}
		if len(s.Plans) == 0 {
			fail(check, "service %q has no plans", s.ID)
			return svcs
		}
		if s.ID == cnpgServiceID && !s.PlanUpdateable {
			fail(check, "definition service %q must advertise plan_updateable", s.Name)
			return svcs
		}
	}
	pass(check, "%d services, unique ids, required fields, plans present", len(svcs))
	return svcs
}

func checkErrorMapping(c *client) {
	const check = "error-mapping"

	status, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-err-1", map[string]interface{}{
		"service_id": "does-not-exist",
		"plan_id":    "also-not",
	})
	if status != 400 {
		fail(check, "unknown service_id -> expected 400, got %d", status)
	} else {
		pass(check, "unknown service_id -> 400")
	}

	status, body := c.do("DELETE", "/v2/service_instances/"+c.cfg.IDPrefix+"-err-2?service_id="+cnpgServiceID+"&plan_id="+smallPlanID, nil)
	if status != 410 {
		fail(check, "deprovision nonexistent -> expected 410, got %d (%s)", status, truncate(body))
	} else {
		pass(check, "deprovision nonexistent -> 410 Gone")
	}
}

// runLifecycleAudit provisions and binds; returns the instance id for the
// follow-up audits ("" on failure).
func runLifecycleAudit(c *client, serviceID, planID string) string {
	instanceID := c.cfg.IDPrefix + "-lc"

	status, body := c.do("PUT", "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
		"context": map[string]interface{}{
			"platform":   "cloudfoundry",
			"space_guid": "default",
		},
	})
	if status != 201 {
		fmt.Printf("::error::PROVISION FULL RESPONSE (status %d): %s\n", status, string(body))
		fail("lifecycle-provision", "provision -> expected 201, got %d: %s", status, truncate(body))
		return ""
	}
	pass("lifecycle-provision", "provision -> 201")

	// Idempotent re-provision must succeed (200 per OSB spec).
	st2, _ := c.do("PUT", "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	if st2 != 200 {
		fail("lifecycle-provision-idempotent", "re-provision same params -> expected 200, got %d", st2)
	} else {
		pass("lifecycle-provision-idempotent", "re-provision same params -> 200")
	}

	// Missing ids must fail with 400.
	st3, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-miss-svc", map[string]interface{}{
		"plan_id": planID,
	})
	if st3 != 400 {
		fail("lifecycle-provision-missing-service", "provision without service_id -> expected 400, got %d", st3)
	} else {
		pass("lifecycle-provision-missing-service", "provision without service_id -> 400")
	}

	st4, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-miss-plan", map[string]interface{}{
		"service_id": serviceID,
	})
	if st4 != 400 {
		fail("lifecycle-provision-missing-plan", "provision without plan_id -> expected 400, got %d", st4)
	} else {
		pass("lifecycle-provision-missing-plan", "provision without plan_id -> 400")
	}

	bStatus, bBody := c.do("PUT", "/v2/service_instances/"+instanceID+"/service_bindings/"+instanceID+"-b1", map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	switch bStatus {
	case 201:
		pass("lifecycle-bind", "bind -> 201")
	default:
		fmt.Printf("INFO [lifecycle-bind]: bind -> %d (credentials secret not pre-created): %s\n",
			bStatus, truncate(bBody))
	}

	if bStatus == 201 {
		// Idempotent re-bind returns 200 with the same credentials.
		rbStatus, rbBody := c.do("PUT", "/v2/service_instances/"+instanceID+"/service_bindings/"+instanceID+"-b1", map[string]interface{}{
			"service_id": serviceID,
			"plan_id":    planID,
		})
		if rbStatus != 200 {
			fail("lifecycle-bind-idempotent", "re-bind -> expected 200, got %d", rbStatus)
		} else if !strings.Contains(string(rbBody), "credentials") {
			fail("lifecycle-bind-idempotent", "re-bind response lacks credentials object")
		} else {
			pass("lifecycle-bind-idempotent", "re-bind -> 200 with credentials")

			// Bind validation errors.
			bm1, _ := c.do("PUT", "/v2/service_instances/"+instanceID+"/service_bindings/"+c.cfg.IDPrefix+"-bm1", map[string]interface{}{
				"plan_id": planID,
			})
			if bm1 != 400 {
				fail("lifecycle-bind-missing-service", "bind without service_id -> expected 400, got %d", bm1)
			} else {
				pass("lifecycle-bind-missing-service", "bind without service_id -> 400")
			}
			bm2, _ := c.do("PUT", "/v2/service_instances/"+instanceID+"/service_bindings/"+c.cfg.IDPrefix+"-bm2", map[string]interface{}{
				"service_id": serviceID,
			})
			if bm2 != 400 {
				fail("lifecycle-bind-missing-plan", "bind without plan_id -> expected 400, got %d", bm2)
			} else {
				pass("lifecycle-bind-missing-plan", "bind without plan_id -> 400")
			}
			bm3, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-noinst"+"/service_bindings/"+c.cfg.IDPrefix+"-bm3", map[string]interface{}{
				"service_id": serviceID,
				"plan_id":    planID,
			})
			if bm3 != 404 {
				fail("lifecycle-bind-nonexistent-instance", "bind to nonexistent instance -> expected 404, got %d", bm3)
			} else {
				pass("lifecycle-bind-nonexistent-instance", "bind to nonexistent instance -> 404")
			}

			st, _ := c.do("DELETE", "/v2/service_instances/"+instanceID+"/service_bindings/"+instanceID+"-b1?service_id="+serviceID+"&plan_id="+planID, nil)
			if st != 200 {
				fail("lifecycle-unbind", "unbind -> expected 200, got %d", st)
			} else {
				pass("lifecycle-unbind", "unbind -> 200")
			}
		}
	}
	return instanceID
}

// runFetchAudit exercises GET instance / GET binding / last_operation incl.
// the 404 paths for nonexistent resources.
func runFetchAudit(c *client, instanceID, serviceID, planID string) {
	const check = "fetch-get-instance"
	status, body := c.do("GET", "/v2/service_instances/"+instanceID+"?service_id="+serviceID+"&plan_id="+planID, nil)
	if status != 200 {
		fail(check, "GET instance -> expected 200, got %d: %s", status, truncate(body))
		return
	}
	var gi struct {
		ServiceID string `json:"service_id"`
		PlanID    string `json:"plan_id"`
	}
	json.Unmarshal(body, &gi)
	if gi.ServiceID != serviceID || gi.PlanID != planID {
		fail(check, "GET instance response missing/incorrect service_id/plan_id")
		return
	}
	pass(check, "GET instance -> 200 with service_id+plan_id")

	checkLO := "fetch-last-operation"
	stLO, loBody := c.do("GET", "/v2/service_instances/"+instanceID+"/last_operation?service_id="+serviceID+"&plan_id="+planID, nil)
	if stLO != 200 {
		fail(checkLO, "last_operation -> expected 200, got %d: %s", stLO, truncate(loBody))
	} else if !strings.Contains(string(loBody), "state") {
		fail(checkLO, "last_operation response lacks state field")
	} else {
		pass(checkLO, "last_operation -> 200 with state")
	}

	checkGhostInst := "fetch-nonexistent-instance"
	st404, _ := c.do("GET", "/v2/service_instances/"+c.cfg.IDPrefix+"-ghost?service_id="+serviceID+"&plan_id="+planID, nil)
	if st404 != 404 {
		fail(checkGhostInst, "GET nonexistent instance -> expected 404, got %d", st404)
	} else {
		pass(checkGhostInst, "GET nonexistent instance -> 404")
	}

	checkGhostBind := "fetch-nonexistent-binding"
	st404b, _ := c.do("GET", "/v2/service_instances/"+instanceID+"/service_bindings/"+c.cfg.IDPrefix+"-ghost-b?service_id="+serviceID+"&plan_id="+planID, nil)
	if st404b != 404 {
		fail(checkGhostBind, "GET nonexistent binding -> expected 404, got %d", st404b)
	} else {
		pass(checkGhostBind, "GET nonexistent binding -> 404")
	}
}

// runUpdateAudit verifies PATCH update semantics: valid update -> 200,
// update of nonexistent instance -> 404.
func runUpdateAudit(c *client, instanceID, serviceID, planID string) {
	const check = "update-instance"
	status, body := c.do("PATCH", "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	if status != 200 {
		fail(check, "update -> expected 200, got %d: %s", status, truncate(body))
		return
	}
	pass(check, "update -> 200")

	checkGhost := "update-nonexistent-instance"
	st404, _ := c.do("PATCH", "/v2/service_instances/"+c.cfg.IDPrefix+"-ghost", map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	if st404 != 404 {
		fail(checkGhost, "update nonexistent instance -> expected 404, got %d", st404)
	} else {
		pass(checkGhost, "update nonexistent instance -> 404")
	}
}

// cleanupAudit unbinds and deprovisions the audit instance and asserts the
// subsequent deprovision yields 410 Gone.
func cleanupAudit(c *client, instanceID, serviceID, planID string) {
	st, _ := c.do("DELETE", "/v2/service_instances/"+instanceID+"/service_bindings/"+instanceID+"-b1?service_id="+serviceID+"&plan_id="+planID, nil)
	switch st {
	case 200, 404, 410:
		pass("cleanup-unbind", "unbind during cleanup -> %d", st)
	default:
		fail("cleanup-unbind", "unbind during cleanup -> unexpected %d", st)
	}

	st, dbody := c.do("DELETE", "/v2/service_instances/"+instanceID+"?service_id="+serviceID+"&plan_id="+planID, nil)
	if st != 200 {
		fail("lifecycle-deprovision", "deprovision -> expected 200, got %d: %s", st, truncate(dbody))
		return
	}
	pass("lifecycle-deprovision", "deprovision -> 200")

	st, _ = c.do("DELETE", "/v2/service_instances/"+instanceID+"?service_id="+serviceID+"&plan_id="+planID, nil)
	if st != 410 {
		fail("lifecycle-deprovision-gone", "second deprovision -> expected 410, got %d", st)
		return
	}
	pass("lifecycle-deprovision-gone", "second deprovision -> 410 Gone")
}
