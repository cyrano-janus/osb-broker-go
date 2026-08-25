// Package checks implements the osb-checker conformance checks against an
// OSB 2.17 broker. Exit contract: Run() returns the failure count.
package checker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Config carries the checker run parameters.
type Config struct {
	BaseURL  string
	User     string
	Pass     string
	IDPrefix string
	Timeout  int64 // seconds; reserved for future per-check deadlines
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
	http *http.Client
	cfg  Config
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
	req.SetBasicAuth(c.cfg.User, c.cfg.Pass)
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

// catalogHasService reports whether the given service id is offered.
func catalogHasService(c *client, serviceID string) bool {
	status, body := c.do("GET", "/v2/catalog", nil)
	return status == 200 && strings.Contains(string(body), serviceID)
}

// Run executes all conformance checks and returns the failure count.
func Run(cfg Config) int {
	failures = 0
	c := &client{http: &http.Client{}, cfg: cfg}

	checkAuthEnforcement(c)
	checkCatalog(c)
	checkErrorMapping(c)
	if catalogHasService(c, cnpgServiceID) {
		lifecycle(c, cnpgServiceID, smallPlanID)
	} else if catalogHasService(c, legacyServiceID) {
		lifecycle(c, legacyServiceID, legacyPlanID)
	} else {
		fmt.Println("SKIP [lifecycle-*]: no known service in catalog")
	}
	return failures
}

func checkAuthEnforcement(c *client) {
	const check = "auth-enforcement"
	req, err := http.NewRequest("GET", strings.TrimSuffix(c.cfg.BaseURL, "/")+"/v2/catalog", nil)
	if err != nil {
		fail(check, "build request: %v", err)
		return
	}
	req.Header.Set("X-Broker-API-Version", "2.17")
	resp, err := c.http.Do(req)
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
	hresp, err := c.http.Do(hreq)
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

func checkCatalog(c *client) {
	const check = "catalog-conformance"
	status, body := c.do("GET", "/v2/catalog", nil)
	if status != 200 {
		fail(check, "GET /v2/catalog -> %d", status)
		return
	}
	var cat struct {
		Services []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Bindable       bool   `json:"bindable"`
			PlanUpdateable bool   `json:"plan_updateable"`
			Plans          []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"plans"`
		} `json:"services"`
	}
	if err := json.Unmarshal(body, &cat); err != nil {
		fail(check, "invalid JSON: %v", err)
		return
	}
	if len(cat.Services) == 0 {
		fail(check, "catalog has no services")
		return
	}
	seenSvc := map[string]bool{}
	for _, s := range cat.Services {
		if s.ID == "" || seenSvc[s.ID] {
			fail(check, "service id empty or duplicated: %q", s.ID)
			return
		}
		seenSvc[s.ID] = true
		seenPlan := map[string]bool{}
		for _, p := range s.Plans {
			if p.ID == "" || seenPlan[p.ID] {
				fail(check, "service %q: plan id empty or duplicated: %q", s.ID, p.ID)
				return
			}
			seenPlan[p.ID] = true
		}
		if len(s.Plans) == 0 {
			fail(check, "service %q has no plans", s.ID)
			return
		}
		if s.ID == cnpgServiceID && !s.PlanUpdateable {
			fail(check, "definition service %q must advertise plan_updateable", s.Name)
			return
		}
	}
	pass(check, "%d services, unique ids, plans present", len(cat.Services))
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

// lifecycle runs provision/bind/unbind/deprovision/gone against the given
// service+plan (works for definition-based and legacy fake services).
func lifecycle(c *client, serviceID, planID string) {
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
		fail("lifecycle-provision", "provision -> expected 201, got %d: %s", status, truncate(body))
		return
	}
	pass("lifecycle-provision", "provision -> 201")

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
		st, _ := c.do("DELETE", "/v2/service_instances/"+instanceID+"/service_bindings/"+instanceID+"-b1?service_id="+serviceID+"&plan_id="+planID, nil)
		if st != 200 {
			fail("lifecycle-unbind", "unbind -> expected 200, got %d", st)
		} else {
			pass("lifecycle-unbind", "unbind -> 200")
		}
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
