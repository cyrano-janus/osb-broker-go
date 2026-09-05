// Package checks traegt die Konformitaetspruefungen von osb-gate gegen einen
// OSB-2.17-Broker. Exit-Vertrag: Run() liefert die Zahl der Fehlschlaege.
//
// Die Pruefungen selbst sind nicht broker-spezifisch: keine Service-ID, kein
// Plan und kein Katalogeintrag dieses Repos steht in diesem Paket. Was hier
// geprueft wird, gilt fuer jeden OSB-Broker - was es zum *Gate* macht, ist
// allein, wo es laeuft: in der CI dieses Repos, blockierend, bei jedem Push.
//
// Die Zweitmeinung ist github.com/cyrano-janus/osb-checker, ein eigenes
// oeffentliches Werkzeug. Beide blockieren, und beide bleiben getrennt: zwei
// unabhaengig geschriebene Pruefer finden zusammen mehr als einer, der sich
// selbst bestaetigt.
//
// Dass die Pruefungen ueberhaupt anschlagen koennen, belegt die
// Mutationssuite in mockbroker_test.go - ein Gate, dessen Pruefungen
// wirkungslos sind, ist von einem gruenen nicht zu unterscheiden.
package checks

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
	"time"
)

// Config carries the checker run parameters.
type Config struct {
	BaseURL  string
	User     string
	Pass     string
	IDPrefix string

	// Timeout begrenzt jeden einzelnen HTTP-Request (Sekunden, 0 = Vorgabe).
	// Ohne diese Schranke haengt ein stummer Broker den ganzen Lauf: der
	// Client wartet unbegrenzt, und aus einem Befund wird ein Prozess, den
	// jemand von Hand abbrechen muss.
	Timeout int64

	// AsyncTimeout begrenzt das Warten auf einen asynchronen Vorgang
	// (Sekunden, 0 = Vorgabe). Ein Broker, der ewig `in progress` meldet,
	// ist ein Befund und kein Geduldsspiel.
	AsyncTimeout int64

	// TLS options. CACert verifies the broker certificate;
	// ClientCert/ClientKey authenticate the checker via mTLS.
	CACert     string
	ClientCert string
	ClientKey  string
	Insecure   bool

	// ServiceID und PlanID waehlen aus, was der Lifecycle-Audit
	// provisioniert. Leer heisst: automatisch waehlen, siehe pickService.
	//
	// Wer den Broker gezielt pruefen will, setzt sie: die OSB-IDs einer
	// ServiceDefinition sind auf Dauer stabil, der Katalog ist es nicht.
	ServiceID string
	PlanID    string

	// UpdateParameterKey/Value benennen einen Parameter, den der gepruefte
	// Plan erlaubt. Ohne diese Angabe sondiert update-parameters mit einem
	// erfundenen Schluessel - und ein Broker mit Allowlist lehnt den zu Recht
	// ab, womit die Pruefung nichts aussagen kann und uebersprungen wird.
	// Wer den Update-Pfad wirklich belegen will, nennt hier einen gueltigen
	// Schluessel.
	UpdateParameterKey   string
	UpdateParameterValue string
}

// demoServiceIDs sind Angebote, die ein Lifecycle-Audit nie pruefen darf: sie
// beweisen nichts ueber den Broker, weil hinter ihnen kein Operator steht.
//
// Wer den ersten Katalogeintrag nimmt, prueft immer sie und nie eine
// ServiceDefinition - genau deshalb ist lange niemandem aufgefallen, dass der
// Definitions-Pfad seine Bindings nicht persistiert.
//
// Dieser Broker stellt sie nicht mehr aus; sein Katalog besteht nur noch aus
// ServiceDefinitions. Die Liste bleibt trotzdem, aus zwei Gruenden: der
// Checker laeuft auch gegen fremde Broker, und der naechste Demo-Service soll
// nicht wieder unbemerkt zum Prueffall werden.
var demoServiceIDs = map[string]bool{"service-1": true, "service-2": true}

// Report ist das Ergebnis eines Laufs. Die Zahl allein genuegt nicht: eine
// Mutationssuite muss belegen, dass genau die zustaendige Pruefung anschlaegt
// und nicht irgendeine - und ein Aufrufer, der den Checker einbettet, will
// wissen, welche.
type Report struct {
	// Failed und Passed tragen die Namen der Pruefungen in Laufreihenfolge.
	// Eine Pruefung kann mehrfach auftauchen: sie prueft mehrere Zusagen.
	Failed  []string
	Passed  []string
	Skipped []string
}

// Failures ist die Zahl der Fehlschlaege und zugleich der Exit-Code.
func (r *Report) Failures() int { return len(r.Failed) }

// HasFailed meldet, ob eine Pruefung dieses Namens angeschlagen hat.
func (r *Report) HasFailed(check string) bool {
	for _, n := range r.Failed {
		if n == check {
			return true
		}
	}
	return false
}

// Der Zustand haengt am Lauf, nicht am Paket. Eine Paketvariable machte zwei
// gleichzeitige Laeufe unmoeglich und verhinderte, dass ein Test das Ergebnis
// eines Laufs fuer sich betrachtet.
func (c *client) fail(check string, format string, args ...interface{}) {
	c.rep.Failed = append(c.rep.Failed, check)
	msg := fmt.Sprintf("FAIL [%s]: %s", check, fmt.Sprintf(format, args...))
	fmt.Println(msg)
	// GitHub Actions annotation: appears in the run UI and annotations API.
	fmt.Printf("::error::%s\n", msg)
}

func (c *client) pass(check string, format string, args ...interface{}) {
	c.rep.Passed = append(c.rep.Passed, check)
	fmt.Printf("PASS [%s]: %s\n", check, fmt.Sprintf(format, args...))
}

func (c *client) skip(check string, format string, args ...interface{}) {
	c.rep.Skipped = append(c.rep.Skipped, check)
	fmt.Printf("SKIP [%s]: %s\n", check, fmt.Sprintf(format, args...))
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
	rep  *Report
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

	to := requestTimeout(cfg)
	return &http.Client{Transport: &http.Transport{TLSClientConfig: authedTLS}, Timeout: to},
		&http.Client{Transport: &http.Transport{TLSClientConfig: anonTLS}, Timeout: to},
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
func Run(cfg Config) int { return RunReport(cfg).Failures() }

// RunReport fuehrt denselben Lauf und gibt zurueck, welche Pruefungen
// bestanden, fehlgeschlagen und uebersprungen wurden.
//
// Die Zahl allein genuegt nicht: eine Mutationssuite muss belegen, dass genau
// die zustaendige Pruefung anschlaegt und nicht irgendeine.
func RunReport(cfg Config) *Report {
	rep := &Report{}

	authed, anon, err := newHTTPClients(cfg)
	if err != nil {
		c := &client{cfg: cfg, rep: rep}
		c.fail("tls-setup", "%v", err)
		return rep
	}
	c := &client{http: authed, anon: anon, cfg: cfg, rep: rep}

	c.checkAuthEnforcement()
	svcs := c.checkCatalogStructure()
	c.checkErrorMapping(svcs)

	// Full lifecycle + fetch + update audit (mirrors the standalone
	// osb-checker's provision/bind/update/fetch categories).
	serviceID, planID := c.pickService(svcs)
	if serviceID == "" {
		c.skip("lifecycle", "kein pruefbarer Service im Katalog")
	} else {
		inst := runLifecycleAudit(c, serviceID, planID)
		if inst != "" {
			runFetchAudit(c, inst, serviceID, planID)
			runUpdateAudit(c, inst, serviceID, planID)
			cleanupAudit(c, inst, serviceID, planID)
		}
	}
	return rep
}

// awaitOperation pollt last_operation, bis der Vorgang abgeschlossen ist.
//
// Der Zustand kommt aus dem CR des Operators, nicht aus einer Buchhaltung im
// Broker - "in progress" heisst also wirklich, dass der Dienst noch entsteht.
func awaitOperation(c *client, instanceID, serviceID, planID string) bool {
	const check = "lifecycle-provision-async"
	path := fmt.Sprintf("/v2/service_instances/%s/last_operation?service_id=%s&plan_id=%s&operation=provision",
		instanceID, serviceID, planID)

	limit := c.asyncDeadline()
	deadline := time.Now().Add(limit)
	wait := time.Second
	for {
		status, body := c.do("GET", path, nil)
		if status != 200 {
			c.fail(check, "last_operation -> expected 200, got %d: %s", status, truncate(body))
			return false
		}
		var resp struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			c.fail(check, "last_operation: invalid JSON: %s", truncate(body))
			return false
		}
		switch resp.State {
		case "succeeded":
			c.pass(check, "last_operation -> succeeded")
			return true
		case "failed":
			c.fail(check, "last_operation -> failed: %s", truncate(body))
			return false
		}
		if time.Now().After(deadline) {
			c.fail(check, "last_operation still %q after %s", resp.State, limit)
			return false
		}
		time.Sleep(wait)
		if wait < 5*time.Second {
			wait += time.Second
		}
	}
}

// Vorgabewerte, wenn die Konfiguration schweigt. Ein CloudNativePG-Cluster
// braucht auf kind gut eine Minute; laenger als vier Minuten ist ein Befund.
const (
	defaultRequestTimeout = 30 * time.Second
	defaultAsyncDeadline  = 4 * time.Minute
)

func (c *client) asyncDeadline() time.Duration {
	if c.cfg.AsyncTimeout > 0 {
		return time.Duration(c.cfg.AsyncTimeout) * time.Second
	}
	return defaultAsyncDeadline
}

func requestTimeout(cfg Config) time.Duration {
	if cfg.Timeout > 0 {
		return time.Duration(cfg.Timeout) * time.Second
	}
	return defaultRequestTimeout
}

// pickService waehlt Service und Plan fuer den Lifecycle-Audit.
//
// Vorrang hat, was der Aufrufer per --service-id vorgibt; ein unbekannter Wert
// ist ein Fehlschlag und kein stiller Rueckfall, sonst prueft die CI klaglos
// etwas anderes als gemeint. Ohne Vorgabe wird der erste Service genommen, der
// KEIN Demo-Angebot ist - also einer aus einer ServiceDefinition. Nur wenn es
// keinen gibt, faellt die Wahl auf das Demo-Angebot; das ist der Fall, wenn der
// Broker ohne DEFINITIONS_DIR laeuft.
func (c *client) pickService(svcs []catalogService) (serviceID, planID string) {
	const check = "lifecycle-service-selection"
	cfg := c.cfg

	if cfg.ServiceID != "" {
		for _, s := range svcs {
			if s.ID != cfg.ServiceID {
				continue
			}
			plan, err := resolvePlan(s, cfg.PlanID)
			if err != nil {
				c.fail(check, "service %q: %v", s.ID, err)
				return "", ""
			}
			c.pass(check, "service %q (%s), plan %q - vorgegeben", s.ID, s.Name, plan)
			return s.ID, plan
		}
		c.fail(check, "service %q steht nicht im Katalog", cfg.ServiceID)
		return "", ""
	}

	for _, s := range svcs {
		if demoServiceIDs[s.ID] || len(s.Plans) == 0 {
			continue
		}
		plan, err := resolvePlan(s, "")
		if err != nil {
			continue
		}
		c.pass(check, "service %q (%s), plan %q - erste ServiceDefinition im Katalog", s.ID, s.Name, plan)
		return s.ID, plan
	}

	for _, s := range svcs {
		plan, err := resolvePlan(s, "")
		if err != nil {
			continue
		}
		c.pass(check, "service %q (%s), plan %q - nur Demo-Angebote im Katalog", s.ID, s.Name, plan)
		return s.ID, plan
	}
	return "", ""
}

// resolvePlan liefert den zu pruefenden Plan: den vorgegebenen, wenn es ihn
// gibt, sonst den ersten des Service.
func resolvePlan(s catalogService, wanted string) (string, error) {
	if wanted != "" {
		for _, p := range s.Plans {
			if p.ID == wanted {
				return p.ID, nil
			}
		}
		return "", fmt.Errorf("plan %q gehoert nicht dazu", wanted)
	}
	if len(s.Plans) == 0 {
		return "", fmt.Errorf("kein Plan im Katalog")
	}
	return s.Plans[0].ID, nil
}

func (c *client) checkAuthEnforcement() {
	const check = "auth-enforcement"
	req, err := http.NewRequest("GET", strings.TrimSuffix(c.cfg.BaseURL, "/")+"/v2/catalog", nil)
	if err != nil {
		c.fail(check, "build request: %v", err)
		return
	}
	req.Header.Set("X-Broker-API-Version", "2.17")
	resp, err := c.anon.Do(req)
	if err != nil {
		c.fail(check, "request failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		c.fail(check, "expected 401 without credentials, got %d", resp.StatusCode)
		return
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		c.fail(check, "401 without WWW-Authenticate header")
		return
	}
	c.pass(check, "unauthenticated request -> 401 with WWW-Authenticate")

	hreq, _ := http.NewRequest("GET", strings.TrimSuffix(c.cfg.BaseURL, "/")+"/healthz", nil)
	hresp, err := c.anon.Do(hreq)
	if err != nil {
		c.fail(check, "healthz request failed: %v", err)
		return
	}
	hresp.Body.Close()
	if hresp.StatusCode != 200 {
		c.fail(check, "healthz expected 200 unauthenticated, got %d", hresp.StatusCode)
		return
	}
	c.pass(check, "/healthz reachable without credentials")
}

// checkCatalogStructure validates the catalog shape: unique ids, required
// fields, plans present — and (broker-specific) plan_updateable on the
// definition service. Returns the services for downstream audits.
func (c *client) checkCatalogStructure() []catalogService {
	const check = "catalog-conformance"
	svcs, ok := fetchCatalog(c)
	if !ok {
		c.fail(check, "GET /v2/catalog failed or returned invalid JSON")
		return nil
	}
	if len(svcs) == 0 {
		c.fail(check, "catalog has no services")
		return nil
	}
	seenSvc := map[string]bool{}
	for _, s := range svcs {
		if s.ID == "" || seenSvc[s.ID] {
			c.fail(check, "service id empty or duplicated: %q", s.ID)
			return svcs
		}
		seenSvc[s.ID] = true
		if s.Name == "" || s.Description == "" {
			c.fail(check, "service %q missing name/description", s.ID)
			return svcs
		}
		seenPlan := map[string]bool{}
		for _, p := range s.Plans {
			if p.ID == "" || seenPlan[p.ID] {
				c.fail(check, "service %q: plan id empty or duplicated: %q", s.ID, p.ID)
				return svcs
			}
			if p.Name == "" {
				c.fail(check, "service %q: plan %q missing name", s.ID, p.ID)
				return svcs
			}
			seenPlan[p.ID] = true
		}
		if len(s.Plans) == 0 {
			c.fail(check, "service %q has no plans", s.ID)
			return svcs
		}

	}
	c.pass(check, "%d services, unique ids, required fields, plans present", len(svcs))
	return svcs
}

func (c *client) checkErrorMapping(svcs []catalogService) {
	const check = "error-mapping"

	status, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-err-1", map[string]interface{}{
		"service_id": "does-not-exist",
		"plan_id":    "also-not",
	})
	if status != 400 {
		c.fail(check, "unknown service_id -> expected 400, got %d", status)
	} else {
		c.pass(check, "unknown service_id -> 400")
	}

	// Service und Plan kommen aus dem Katalog des geprueften Brokers, nicht
	// aus einer Konstante in dieser Datei. Hartverdrahtete IDs machen aus
	// einem allgemeinen Konformitaetswerkzeug eines, das nur gegen genau
	// diesen Broker etwas aussagt - gegen jeden anderen prueft es einen
	// unbekannten Service und misst damit die falsche Regel.
	sid, pid := anyServiceAndPlan(svcs)
	if sid == "" {
		c.skip(check, "kein Service im Katalog, deprovision-410 nicht pruefbar")
		return
	}
	status, body := c.do("DELETE", "/v2/service_instances/"+c.cfg.IDPrefix+"-err-2?service_id="+sid+"&plan_id="+pid, nil)
	if status != 410 {
		c.fail(check, "deprovision nonexistent -> expected 410, got %d (%s)", status, truncate(body))
	} else {
		c.pass(check, "deprovision nonexistent -> 410 Gone")
	}
}

// anyServiceAndPlan liefert irgendeinen Service mit Plan aus dem Katalog.
// Fuer Pruefungen, die zwar gueltige IDs brauchen, denen aber gleich ist,
// welche - etwa das Deprovision einer Instanz, die es nicht gibt.
func anyServiceAndPlan(svcs []catalogService) (string, string) {
	for _, s := range svcs {
		if len(s.Plans) > 0 {
			return s.ID, s.Plans[0].ID
		}
	}
	return "", ""
}

// runLifecycleAudit provisions and binds; returns the instance id for the
// follow-up audits ("" on failure).
func runLifecycleAudit(c *client, serviceID, planID string) string {
	instanceID := c.cfg.IDPrefix + "-lc"

	// accepts_incomplete gehoert in die Query, nicht in den Body. Ohne den
	// Parameter antwortet ein Broker, der nur asynchron kann, mit 422
	// AsyncRequired - und das ist die richtige Antwort, kein Fehler.
	status, body := c.do("PUT", "/v2/service_instances/"+instanceID+"?accepts_incomplete=true", map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
		"context": map[string]interface{}{
			"platform":   "cloudfoundry",
			"space_guid": "default",
		},
	})
	switch status {
	case 201:
		c.pass("lifecycle-provision", "provision -> 201 (synchron)")
	case 202:
		c.pass("lifecycle-provision", "provision -> 202 (asynchron)")
		// Erst wenn last_operation Vollzug meldet, ist der Dienst da. Wer
		// hier nicht wartet, bindet gegen Zugangsdaten, die es noch nicht
		// gibt - genau das hat der Checker frueher getan.
		if !awaitOperation(c, instanceID, serviceID, planID) {
			return ""
		}
	default:
		fmt.Printf("::error::PROVISION FULL RESPONSE (status %d): %s\n", status, string(body))
		c.fail("lifecycle-provision", "provision -> expected 201 or 202, got %d: %s", status, truncate(body))
		return ""
	}

	// Idempotent re-provision must succeed (200 per OSB spec).
	st2, _ := c.do("PUT", "/v2/service_instances/"+instanceID+"?accepts_incomplete=true", map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	if st2 != 200 {
		c.fail("lifecycle-provision-idempotent", "re-provision same params -> expected 200, got %d", st2)
	} else {
		c.pass("lifecycle-provision-idempotent", "re-provision same params -> 200")
	}

	// Missing ids must fail with 400.
	st3, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-miss-svc", map[string]interface{}{
		"plan_id": planID,
	})
	if st3 != 400 {
		c.fail("lifecycle-provision-missing-service", "provision without service_id -> expected 400, got %d", st3)
	} else {
		c.pass("lifecycle-provision-missing-service", "provision without service_id -> 400")
	}

	st4, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-miss-plan", map[string]interface{}{
		"service_id": serviceID,
	})
	if st4 != 400 {
		c.fail("lifecycle-provision-missing-plan", "provision without plan_id -> expected 400, got %d", st4)
	} else {
		c.pass("lifecycle-provision-missing-plan", "provision without plan_id -> 400")
	}

	bindingID := instanceID + "-b1"
	bindPath := "/v2/service_instances/" + instanceID + "/service_bindings/" + bindingID

	// Ein neuer Bind ist 201. 200 hiesse "gab es schon", und das ist fuer
	// eine frisch erzeugte binding_id falsch.
	//
	// Frueher stand hier ein switch, dessen default nur eine INFO-Zeile
	// druckte: ein fehlgeschlagener Bind zaehlte nicht als Fehlschlag und
	// nahm fuenf Folgepruefungen stumm mit. Genauso lagen die Folgepruefungen
	// im else-Zweig des Re-Binds - schlug der fehl, verschwanden sie
	// ebenfalls. Deshalb steht hier nichts mehr verschachtelt: jede Pruefung
	// laeuft und meldet fuer sich.
	bStatus, bBody := c.do("PUT", bindPath, map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	bindOK := bStatus == 201
	if !bindOK {
		c.fail("lifecycle-bind", "bind -> expected 201, got %d: %s", bStatus, truncate(bBody))
	} else if !hasCredentials(bBody) {
		bindOK = false
		c.fail("lifecycle-bind", "bind -> 201 without a credentials object: %s", truncate(bBody))
	} else {
		c.pass("lifecycle-bind", "bind -> 201 with credentials")
		c.checkServiceBindingSpec(bBody)
	}

	// Idempotent re-bind returns 200 with the same credentials.
	rbStatus, rbBody := c.do("PUT", bindPath, map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	switch {
	case rbStatus != 200:
		c.fail("lifecycle-bind-idempotent", "re-bind -> expected 200, got %d", rbStatus)
	case !hasCredentials(rbBody):
		c.fail("lifecycle-bind-idempotent", "re-bind response lacks credentials object")
	default:
		c.pass("lifecycle-bind-idempotent", "re-bind -> 200 with credentials")
	}

	// Bind validation errors.
	bm1, _ := c.do("PUT", "/v2/service_instances/"+instanceID+"/service_bindings/"+c.cfg.IDPrefix+"-bm1", map[string]interface{}{
		"plan_id": planID,
	})
	if bm1 != 400 {
		c.fail("lifecycle-bind-missing-service", "bind without service_id -> expected 400, got %d", bm1)
	} else {
		c.pass("lifecycle-bind-missing-service", "bind without service_id -> 400")
	}
	bm2, _ := c.do("PUT", "/v2/service_instances/"+instanceID+"/service_bindings/"+c.cfg.IDPrefix+"-bm2", map[string]interface{}{
		"service_id": serviceID,
	})
	if bm2 != 400 {
		c.fail("lifecycle-bind-missing-plan", "bind without plan_id -> expected 400, got %d", bm2)
	} else {
		c.pass("lifecycle-bind-missing-plan", "bind without plan_id -> 400")
	}
	bm3, _ := c.do("PUT", "/v2/service_instances/"+c.cfg.IDPrefix+"-noinst"+"/service_bindings/"+c.cfg.IDPrefix+"-bm3", map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	if bm3 != 404 {
		c.fail("lifecycle-bind-nonexistent-instance", "bind to nonexistent instance -> expected 404, got %d", bm3)
	} else {
		c.pass("lifecycle-bind-nonexistent-instance", "bind to nonexistent instance -> 404")
	}

	// GET auf eine bestehende Binding. Nur hier ist sie noch da - nach dem
	// Unbind laesst sich das nicht mehr pruefen, und geprueft wurde bislang
	// ausschliesslich der 404 fuer eine unbekannte.
	if bindOK {
		gbStatus, gbBody := c.do("GET", bindPath, nil)
		switch {
		case gbStatus != 200:
			c.fail("fetch-get-binding", "GET binding -> expected 200, got %d: %s", gbStatus, truncate(gbBody))
		case !hasCredentials(gbBody):
			c.fail("fetch-get-binding", "GET binding -> 200 without a credentials object")
		default:
			c.pass("fetch-get-binding", "GET binding -> 200 with credentials")
		}
	}

	st, _ := c.do("DELETE", bindPath+"?service_id="+serviceID+"&plan_id="+planID, nil)
	if st != 200 {
		c.fail("lifecycle-unbind", "unbind -> expected 200, got %d", st)
	} else {
		c.pass("lifecycle-unbind", "unbind -> 200")
	}
	return instanceID
}

// hasCredentials meldet, ob der Antwortkoerper ein credentials-Objekt traegt.
//
// Eine Textsuche nach "credentials" genuegte nicht: sie findet das Wort auch
// in einer Fehlermeldung und macht aus einer abgelehnten Bind-Anfrage eine
// bestandene Pruefung.
func hasCredentials(body []byte) bool {
	var resp struct {
		Credentials map[string]interface{} `json:"credentials"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	return len(resp.Credentials) > 0
}

// runFetchAudit exercises GET instance / GET binding / last_operation incl.
// the 404 paths for nonexistent resources.
func runFetchAudit(c *client, instanceID, serviceID, planID string) {
	const check = "fetch-get-instance"
	status, body := c.do("GET", "/v2/service_instances/"+instanceID+"?service_id="+serviceID+"&plan_id="+planID, nil)
	if status != 200 {
		c.fail(check, "GET instance -> expected 200, got %d: %s", status, truncate(body))
		return
	}
	var gi struct {
		ServiceID string `json:"service_id"`
		PlanID    string `json:"plan_id"`
	}
	json.Unmarshal(body, &gi)
	if gi.ServiceID != serviceID || gi.PlanID != planID {
		c.fail(check, "GET instance response missing/incorrect service_id/plan_id")
		return
	}
	c.pass(check, "GET instance -> 200 with service_id+plan_id")

	checkLO := "fetch-last-operation"
	stLO, loBody := c.do("GET", "/v2/service_instances/"+instanceID+"/last_operation?service_id="+serviceID+"&plan_id="+planID, nil)
	if stLO != 200 {
		c.fail(checkLO, "last_operation -> expected 200, got %d: %s", stLO, truncate(loBody))
	} else if !strings.Contains(string(loBody), "state") {
		c.fail(checkLO, "last_operation response lacks state field")
	} else {
		c.pass(checkLO, "last_operation -> 200 with state")
	}

	checkGhostInst := "fetch-nonexistent-instance"
	st404, _ := c.do("GET", "/v2/service_instances/"+c.cfg.IDPrefix+"-ghost?service_id="+serviceID+"&plan_id="+planID, nil)
	if st404 != 404 {
		c.fail(checkGhostInst, "GET nonexistent instance -> expected 404, got %d", st404)
	} else {
		c.pass(checkGhostInst, "GET nonexistent instance -> 404")
	}

	checkGhostBind := "fetch-nonexistent-binding"
	st404b, _ := c.do("GET", "/v2/service_instances/"+instanceID+"/service_bindings/"+c.cfg.IDPrefix+"-ghost-b?service_id="+serviceID+"&plan_id="+planID, nil)
	if st404b != 404 {
		c.fail(checkGhostBind, "GET nonexistent binding -> expected 404, got %d", st404b)
	} else {
		c.pass(checkGhostBind, "GET nonexistent binding -> 404")
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
		c.fail(check, "update -> expected 200, got %d: %s", status, truncate(body))
		return
	}
	c.pass(check, "update -> 200")

	checkGhost := "update-nonexistent-instance"
	st404, _ := c.do("PATCH", "/v2/service_instances/"+c.cfg.IDPrefix+"-ghost", map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    planID,
	})
	if st404 != 404 {
		c.fail(checkGhost, "update nonexistent instance -> expected 404, got %d", st404)
	} else {
		c.pass(checkGhost, "update nonexistent instance -> 404")
	}

	c.checkUpdateParameters(instanceID, serviceID, planID)
}

// checkUpdateParameters prueft den Weg von `cf update-service -c '{...}'`:
// ein PATCH, der nur Parameter traegt und kein plan_id.
//
// **Warum das eine eigene Pruefung ist.** Cloud Foundry auf Korifi - die
// Entwicklungsplattform dieses Brokers - reicht ein `cf update-service -c`
// ueberhaupt nicht an den Broker weiter: die CLI meldet Erfolg, ohne dass je
// ein PATCH ankommt. Ueber die Plattform ist dieser Pfad also nicht pruefbar,
// und ein Bruch faellt dort nicht auf. Auf einem Zielsystem faellt er auf.
// Deshalb wird er hier direkt gegen den Broker geprueft.
//
// Zwei Zusagen, beide aus OSB 2.17:
//
//  1. `plan_id` ist im PATCH optional. Ein Broker, der ihn verlangt, lehnt
//     eine Anfrage ab, die die Spezifikation erlaubt.
//  2. Was der Broker angenommen hat, muss er auch berichten - sofern er
//     GET /v2/service_instances mit `parameters` beantwortet. Tut er das
//     nicht, ist das erlaubt und wird uebersprungen, nicht bemaengelt: ohne
//     Rueckmeldung laesst sich die Zusage nicht pruefen.
func (c *client) checkUpdateParameters(instanceID, serviceID, planID string) {
	const check = "update-parameters"

	// Ohne Vorgabe: ein Schluessel, den kein Plan vorgibt. Ein Broker mit
	// Allowlist lehnt den zu Recht ab - dann sagt die Pruefung nichts und
	// wird uebersprungen. Mit Vorgabe ist sie belastbar.
	probeKey := "osbGateProbe"
	probe := fmt.Sprintf("v%d", time.Now().UnixNano()%100000)
	if c.cfg.UpdateParameterKey != "" {
		probeKey = c.cfg.UpdateParameterKey
		probe = c.cfg.UpdateParameterValue
	}

	status, body := c.do("PATCH", "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": serviceID,
		"parameters": map[string]interface{}{probeKey: probe},
	})
	switch status {
	case 200, 202:
		c.pass(check, "PATCH mit parameters und ohne plan_id -> %d", status)
	case 400, 422:
		// Ein Broker darf einen Parameter ablehnen, den er nicht kennt -
		// eine Allowlist ist zulaessig. Was er nicht darf, ist die Anfrage
		// allein wegen des fehlenden plan_id ablehnen. Das laesst sich von
		// aussen nicht sicher trennen, deshalb die Gegenprobe: dieselbe
		// Anfrage mit plan_id. Geht die durch, lag es am plan_id.
		c.probeMissingPlanID(check, instanceID, serviceID, planID, probeKey, probe, status, body)
		return
	default:
		c.fail(check, "PATCH mit parameters -> expected 200/202, got %d: %s", status, truncate(body))
		return
	}

	gStatus, gBody := c.do("GET", "/v2/service_instances/"+instanceID, nil)
	if gStatus != 200 {
		c.skip(check, "GET instance -> %d, die Rueckmeldung der Parameter ist nicht pruefbar", gStatus)
		return
	}
	var got struct {
		Parameters map[string]interface{} `json:"parameters"`
	}
	if err := json.Unmarshal(gBody, &got); err != nil {
		c.fail(check, "GET instance is not valid JSON: %s", truncate(gBody))
		return
	}
	if got.Parameters == nil {
		c.skip(check, "GET instance meldet kein parameters-Objekt - der Broker gibt Parameter nicht zurueck")
		return
	}
	if got.Parameters[probeKey] != probe {
		c.fail(check, "der Parameter %q wurde angenommen, aber nicht berichtet: %s",
			probeKey, truncate(gBody))
		return
	}
	c.pass(check, "der gesetzte Parameter steht in GET /v2/service_instances")
}

// probeMissingPlanID trennt "der Broker mag diesen Parameter nicht" von "der
// Broker verlangt ein plan_id, das die Spezifikation nicht verlangt".
func (c *client) probeMissingPlanID(check, instanceID, serviceID, planID, probeKey, probe string, firstStatus int, firstBody []byte) {
	plan := planID
	if plan == "" {
		c.skip(check, "PATCH ohne plan_id -> %d; ohne bekannten Plan nicht weiter eingrenzbar: %s",
			firstStatus, truncate(firstBody))
		return
	}
	status, _ := c.do("PATCH", "/v2/service_instances/"+instanceID, map[string]interface{}{
		"service_id": serviceID,
		"plan_id":    plan,
		"parameters": map[string]interface{}{probeKey: probe},
	})
	if status == 200 || status == 202 {
		c.fail(check, "PATCH ohne plan_id -> %d, mit plan_id -> %d: plan_id ist im PATCH optional (OSB 2.17)",
			firstStatus, status)
		return
	}
	c.skip(check, "der Broker nimmt %q nicht an (%d mit und ohne plan_id) - keine Aussage ueber plan_id; "+
		"mit --update-parameter key=wert einen erlaubten Schluessel nennen", probeKey, firstStatus)
}

// cleanupAudit unbinds and deprovisions the audit instance and asserts the
// subsequent deprovision yields 410 Gone.
func cleanupAudit(c *client, instanceID, serviceID, planID string) {
	st, _ := c.do("DELETE", "/v2/service_instances/"+instanceID+"/service_bindings/"+instanceID+"-b1?service_id="+serviceID+"&plan_id="+planID, nil)
	switch st {
	case 200, 404, 410:
		c.pass("cleanup-unbind", "unbind during cleanup -> %d", st)
	default:
		c.fail("cleanup-unbind", "unbind during cleanup -> unexpected %d", st)
	}

	st, dbody := c.do("DELETE", "/v2/service_instances/"+instanceID+"?service_id="+serviceID+"&plan_id="+planID, nil)
	if st != 200 {
		c.fail("lifecycle-deprovision", "deprovision -> expected 200, got %d: %s", st, truncate(dbody))
		return
	}
	c.pass("lifecycle-deprovision", "deprovision -> 200")

	st, _ = c.do("DELETE", "/v2/service_instances/"+instanceID+"?service_id="+serviceID+"&plan_id="+planID, nil)
	if st != 410 {
		c.fail("lifecycle-deprovision-gone", "second deprovision -> expected 410, got %d", st)
		return
	}
	c.pass("lifecycle-deprovision-gone", "second deprovision -> 410 Gone")
}

// wellKnownBindingKeys nennt je Diensttyp die Felder, die die CNCF Service
// Binding Specification als "well-known" fuehrt. Ein Konsument, der sich auf
// den Typ verlaesst, erwartet genau diese.
var wellKnownBindingKeys = map[string][]string{
	"postgresql": {"host", "port", "username", "password"},
	"mysql":      {"host", "port", "username", "password"},
	"mongodb":    {"host", "port", "username", "password"},
	"redis":      {"host", "port", "password"},
	"rabbitmq":   {"host", "port", "username", "password"},
	"kafka":      {"bootstrap-servers"},
	"s3":         {"bucket", "region", "access-key-id", "secret-access-key"},
}

// checkServiceBindingSpec prueft eine Bind-Antwort gegen die CNCF Service
// Binding Specification (Phase 6.5).
//
// Der Checker sieht nur die OSB-Schnittstelle, nicht den Cluster - das
// projizierte Secret und das status.binding.name-Feld kann er also nicht
// pruefen. Was er sehen kann, ist der Typ und ob die Credentials zu ihm
// passen. Ohne type ist der Dienst schlicht nicht spec-konform; das ist
// erlaubt und wird als SKIP gemeldet, nicht als Fehler - sonst waere jeder
// bestehende Broker ueber Nacht rot.
func (c *client) checkServiceBindingSpec(body []byte) {
	const check = "service-binding-spec"

	var resp struct {
		Credentials map[string]interface{} `json:"credentials"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		c.fail(check, "bind response is not valid JSON: %s", truncate(body))
		return
	}

	rawType, ok := resp.Credentials["type"]
	if !ok {
		c.skip(check, "binding carries no 'type' - service is not declared as specification-conformant")
		return
	}

	serviceType, ok := rawType.(string)
	if !ok || serviceType == "" {
		c.fail(check, "'type' must be a non-empty string, got %v", rawType)
		return
	}
	if serviceType != strings.ToLower(serviceType) {
		c.fail(check, "'type' %q must be lower-case", serviceType)
		return
	}
	c.pass(check, "binding declares type %q", serviceType)

	expected, known := wellKnownBindingKeys[serviceType]
	if !known {
		fmt.Printf("SKIP [%s]: %q is not a well-known type, no key expectations\n", check, serviceType)
		return
	}
	var missing []string
	for _, key := range expected {
		if _, ok := resp.Credentials[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		c.fail(check, "type %q is missing well-known keys: %s", serviceType, strings.Join(missing, ", "))
		return
	}
	c.pass(check, "type %q carries all well-known keys", serviceType)
}
