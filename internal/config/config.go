// Package config loads the broker configuration from the environment into a
// single validated struct (Phase 4.5). Before this package every setting was
// read ad-hoc via os.Getenv in main.go, which made the TLS/mTLS combinations
// impossible to validate in one place or to test without touching the
// process environment.
package config

import (
	"crypto/tls"
	"fmt"
	"os"
	"time"
)

// Auth method names. The order of these constants is the order in which
// authenticators are registered, and therefore the order of the
// WWW-Authenticate challenges on a 401.
const (
	MethodBasic = "basic"
	MethodMTLS  = "mtls"
)

// State-Store-Backends (Phase 5).
const (
	// BackendCRD haelt jeden Datensatz als eigenes Custom Resource.
	BackendCRD = "crd"
	// BackendMemory verliert den Zustand beim Neustart; fuer lokale Laeufe
	// und Tests.
	BackendMemory = "memory"
	// backendK8sLegacy war der Name des ConfigMap-Stores. Er wird als Alias
	// auf BackendCRD akzeptiert, damit bestehende Deployments beim Upgrade
	// nicht still auf In-Memory zurueckfallen.
	backendK8sLegacy = "k8s"
)

// Config is the fully resolved broker configuration.
type Config struct {
	Port           string
	StoreBackend   string
	PodNamespace   string
	DefinitionsDir string
	MetricsEnabled bool
	// LogRequests schaltet das Zugriffsprotokoll. Standardmaessig an.
	LogRequests bool

	Server ServerConfig
	TLS    TLSConfig
	Auth   AuthConfig

	// Warnings carries non-fatal findings for main() to log. Configuration
	// that is legal but weak (no auth at all, mTLS without an allowlist)
	// must be visible in the log, not silently accepted.
	Warnings []string
}

// ServerConfig holds the http.Server tuning knobs. The broker ran without
// any timeouts before Phase 4.5.
type ServerConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// TLSConfig describes the HTTPS listener.
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
	// ClientCAFile mirrors Auth.MTLS.ClientCAFile; the TLS layer needs it to
	// build the client CA pool, the auth layer to authorise the identity.
	ClientCAFile   string
	MinVersion     uint16
	ReloadInterval time.Duration
	// ClientAuth is derived, never read from the environment directly. See
	// deriveClientAuth for the rules.
	ClientAuth tls.ClientAuthType
}

// AuthConfig holds every authentication method and the shared realm.
type AuthConfig struct {
	Realm   string
	Methods []string
	Basic   BasicConfig
	MTLS    MTLSConfig
}

// BasicConfig carries the HTTP Basic credentials. Both empty = basic auth
// not configured.
type BasicConfig struct {
	User     string
	Password string
}

// MTLSConfig carries client-certificate authentication settings. An empty
// allowlist accepts any certificate the configured CA signed.
type MTLSConfig struct {
	Enabled         bool
	Require         bool
	ClientCAFile    string
	AllowedCNs      []string
	AllowedDNSNames []string
	AllowedURIs     []string
}

// Load reads the configuration from the process environment.
func Load() (*Config, error) { return LoadFrom(os.Getenv) }

// LoadFrom reads the configuration from an arbitrary lookup function and
// validates it. Tests use this to avoid mutating the process environment.
func LoadFrom(get func(string) string) (*Config, error) {
	l := &loader{get: get}

	c := &Config{
		Port:           l.str("PORT", "8080"),
		StoreBackend:   l.str("STORE_BACKEND", BackendMemory),
		PodNamespace:   l.str("POD_NAMESPACE", ""),
		DefinitionsDir: l.str("DEFINITIONS_DIR", ""),
		// Legacy contract from Phase 4.3: metrics are on unless the value is
		// exactly "0". Deliberately not ParseBool - "false" kept metrics on
		// before this package existed and must keep doing so.
		MetricsEnabled: get("METRICS_ENABLED") != "0",
		// Standardmaessig an: ein Broker, der stumm laeuft, ist im Fehlerfall
		// nicht nachvollziehbar. Abschaltbar, weil ein Zugriffsprotokoll je
		// Request bei hoher Last teuer wird und manche Betreiber es ohnehin
		// am Ingress fuehren.
		LogRequests: l.boolean("LOG_REQUESTS", true),

		Server: ServerConfig{
			ReadHeaderTimeout: l.duration("SERVER_READ_HEADER_TIMEOUT", 10*time.Second),
			ReadTimeout:       l.duration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:      l.duration("SERVER_WRITE_TIMEOUT", 60*time.Second),
			IdleTimeout:       l.duration("SERVER_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout:   l.duration("SERVER_SHUTDOWN_TIMEOUT", 15*time.Second),
		},

		TLS: TLSConfig{
			Enabled:        l.boolean("TLS_ENABLED", false),
			CertFile:       l.str("TLS_CERT_FILE", ""),
			KeyFile:        l.str("TLS_KEY_FILE", ""),
			ReloadInterval: l.duration("TLS_RELOAD_INTERVAL", 30*time.Second),
		},

		Auth: AuthConfig{
			Realm: l.str("AUTH_REALM", "osb-broker"),
			Basic: BasicConfig{
				User:     l.str("BROKER_AUTH_USER", ""),
				Password: l.str("BROKER_AUTH_PASSWORD", ""),
			},
			MTLS: MTLSConfig{
				Enabled:         l.boolean("MTLS_ENABLED", false),
				Require:         l.boolean("MTLS_REQUIRE", false),
				ClientCAFile:    l.str("MTLS_CLIENT_CA_FILE", ""),
				AllowedCNs:      l.csv("MTLS_ALLOWED_CNS"),
				AllowedDNSNames: l.csv("MTLS_ALLOWED_DNS_NAMES"),
				AllowedURIs:     l.csv("MTLS_ALLOWED_URIS"),
			},
		},
	}
	if l.err != nil {
		return nil, l.err
	}

	backend, legacyBackendName, err := normaliseStoreBackend(c.StoreBackend)
	if err != nil {
		return nil, err
	}
	c.StoreBackend = backend

	minVersion, err := parseMinVersion(l.str("TLS_MIN_VERSION", "1.2"))
	if err != nil {
		return nil, err
	}
	c.TLS.MinVersion = minVersion
	c.TLS.ClientCAFile = c.Auth.MTLS.ClientCAFile

	methods, err := resolveMethods(l.csv("AUTH_METHODS"), c)
	if err != nil {
		return nil, err
	}
	c.Auth.Methods = methods
	c.TLS.ClientAuth = c.deriveClientAuth()

	if err := c.Validate(); err != nil {
		return nil, err
	}
	c.collectWarnings()
	if legacyBackendName {
		c.Warnings = append(c.Warnings,
			"STORE_BACKEND=k8s bezeichnete den abgeloesten ConfigMap-Store und wird als \"crd\" gelesen; bitte auf STORE_BACKEND=crd umstellen")
	}
	return c, nil
}

// AuthEnabled reports whether any authentication method is active. False
// means every /v2 endpoint is open - the pre-Phase-1.2 behaviour, kept for
// local runs and tests.
func (c *Config) AuthEnabled() bool { return len(c.Auth.Methods) > 0 }

// basicConfigured reports whether Basic Auth credentials were supplied. One
// of the two being set is enough, matching the existing main.go check.
func (c *Config) basicConfigured() bool {
	return c.Auth.Basic.User != "" || c.Auth.Basic.Password != ""
}

// normaliseStoreBackend prueft den Wert und loest den Alias auf. Ein
// unbekannter Wert ist ein Fehler: vorher fiel jeder Tippfehler still auf
// In-Memory zurueck, der Broker lief scheinbar normal und verlor beim
// naechsten Neustart alle Instanzen.
func normaliseStoreBackend(v string) (backend string, wasLegacyName bool, err error) {
	switch v {
	case BackendCRD, BackendMemory:
		return v, false, nil
	case backendK8sLegacy:
		return BackendCRD, true, nil
	default:
		return "", false, fmt.Errorf("STORE_BACKEND: %q ist unbekannt (erlaubt: %s, %s)", v, BackendCRD, BackendMemory)
	}
}

func parseMinVersion(v string) (uint16, error) {
	switch v {
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("TLS_MIN_VERSION: %q is not supported (use 1.2 or 1.3)", v)
	}
}

// resolveMethods derives the active auth methods from what is configured, or
// validates an explicit AUTH_METHODS list against it. The returned order is
// always canonical (basic, mtls) so the challenge order does not depend on
// how the operator spelled the list.
func resolveMethods(requested []string, c *Config) ([]string, error) {
	available := map[string]bool{
		MethodBasic: c.basicConfigured(),
		MethodMTLS:  c.Auth.MTLS.Enabled,
	}

	if len(requested) == 0 {
		var out []string
		for _, m := range []string{MethodBasic, MethodMTLS} {
			if available[m] {
				out = append(out, m)
			}
		}
		return out, nil
	}

	wanted := map[string]bool{}
	for _, m := range requested {
		known, ok := available[m]
		if !ok {
			return nil, fmt.Errorf("AUTH_METHODS: unknown method %q (known: %s, %s)", m, MethodBasic, MethodMTLS)
		}
		if !known {
			return nil, fmt.Errorf("AUTH_METHODS: method %q is not configured", m)
		}
		wanted[m] = true
	}

	var out []string
	for _, m := range []string{MethodBasic, MethodMTLS} {
		if wanted[m] {
			out = append(out, m)
		}
	}
	return out, nil
}

// deriveClientAuth picks the crypto/tls client-certificate mode.
//
// RequireAndVerifyClientCert aborts the handshake for clients without a
// certificate - including kubelet probes and Prometheus scrapes - so it is
// only used when mTLS is the sole way in and the operator asked for it.
// Anything else uses VerifyClientCertIfGiven: a presented certificate is
// fully chain-verified, its absence leaves the door open for Basic Auth.
// RequireAnyClientCert is never used; it accepts unverified certificates.
func (c *Config) deriveClientAuth() tls.ClientAuthType {
	if !c.TLS.Enabled || !c.Auth.MTLS.Enabled {
		return tls.NoClientCert
	}
	if c.Auth.MTLS.Require && len(c.Auth.Methods) == 1 && c.Auth.Methods[0] == MethodMTLS {
		return tls.RequireAndVerifyClientCert
	}
	return tls.VerifyClientCertIfGiven
}

// Validate rejects configurations that cannot work, with the offending
// environment variable named.
func (c *Config) Validate() error {
	if c.StoreBackend == BackendCRD && c.PodNamespace == "" {
		return fmt.Errorf("STORE_BACKEND=%s requires POD_NAMESPACE", BackendCRD)
	}
	if c.TLS.Enabled {
		if c.TLS.CertFile == "" {
			return fmt.Errorf("TLS_ENABLED=true requires TLS_CERT_FILE")
		}
		if c.TLS.KeyFile == "" {
			return fmt.Errorf("TLS_ENABLED=true requires TLS_KEY_FILE")
		}
	}
	if c.Auth.MTLS.Enabled {
		if !c.TLS.Enabled {
			return fmt.Errorf("MTLS_ENABLED=true requires TLS_ENABLED=true")
		}
		if c.Auth.MTLS.ClientCAFile == "" {
			return fmt.Errorf("MTLS_ENABLED=true requires MTLS_CLIENT_CA_FILE")
		}
	}
	return nil
}

func (c *Config) collectWarnings() {
	if c.StoreBackend == BackendMemory {
		c.Warnings = append(c.Warnings,
			"state store is in-memory - instances and bindings are lost on restart; set STORE_BACKEND=crd for production use")
	}
	if !c.AuthEnabled() {
		c.Warnings = append(c.Warnings,
			"authentication is disabled - set BROKER_AUTH_USER/BROKER_AUTH_PASSWORD or MTLS_ENABLED for production use")
	}
	if !c.TLS.Enabled {
		c.Warnings = append(c.Warnings,
			"TLS is disabled - the broker serves plain HTTP and credentials travel in the clear")
	}
	if c.Auth.MTLS.Enabled && len(c.Auth.MTLS.AllowedCNs) == 0 &&
		len(c.Auth.MTLS.AllowedDNSNames) == 0 && len(c.Auth.MTLS.AllowedURIs) == 0 {
		c.Warnings = append(c.Warnings,
			"mTLS has no allowlist - every certificate signed by MTLS_CLIENT_CA_FILE is accepted; set MTLS_ALLOWED_CNS/_DNS_NAMES/_URIS")
	}
	if c.Auth.MTLS.Require && c.TLS.ClientAuth != tls.RequireAndVerifyClientCert {
		c.Warnings = append(c.Warnings,
			"MTLS_REQUIRE=true was downgraded because another auth method is active; requiring client certificates would lock those clients out at the handshake")
	}
}
