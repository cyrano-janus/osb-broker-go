// Command osb-broker ist der Einstiegspunkt: er verdrahtet Konfiguration,
// Zustandsspeicher, Engine, Authentifizierung und Server und startet ihn.
// Fachlogik gehoert hier nicht hinein.
//
// Die Reihenfolge ist bewusst fail-fast: config.Load bricht bei einer
// unbrauchbaren Umgebung ab, und eine nicht parsende ServiceDefinition
// beendet den Start - ein Broker mit halbem Katalog ist schlimmer als einer,
// der gar nicht hochkommt.
package main

import (
	"crypto/tls"
	"log"

	"github.com/cyrano-janus/osb-broker-go/internal/auth"
	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/config"
	"github.com/cyrano-janus/osb-broker-go/internal/handlers"
	"github.com/cyrano-janus/osb-broker-go/internal/reconcile"
	"github.com/cyrano-janus/osb-broker-go/internal/server"
	k8sconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	// Configuration (Phase 4.5): one validated struct instead of scattered
	// os.Getenv calls. Load fails fast on impossible combinations.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	for _, w := range cfg.Warnings {
		log.Printf("WARNING: %s", w)
	}

	// Cancelled on SIGTERM so a rollout drains in-flight requests instead
	// of cutting them off.
	ctx, stop := server.SignalContext()
	defer stop()

	// State store selection (Phase 5): "crd" haelt jeden Datensatz als
	// eigenes Custom Resource; "memory" ist fuer lokale Laeufe und Tests.
	// Ein unbekannter Wert wurde bereits in config.Load abgelehnt.
	var stateStore broker.StateStore
	switch cfg.StoreBackend {
	case config.BackendCRD:
		restCfg, err := k8sconfig.GetConfig()
		if err != nil {
			log.Fatalf("load kubeconfig: %v", err)
		}
		k8sClient, err := broker.NewK8sClient(restCfg)
		if err != nil {
			log.Fatalf("build k8s client: %v", err)
		}
		stateStore = broker.NewCRDStateStore(k8sClient, cfg.PodNamespace)
		log.Printf("State store: OSBServiceInstance/OSBServiceBinding in namespace %q", cfg.PodNamespace)
	default:
		stateStore = broker.NewInMemoryStateStore()
	}

	// Initialize broker
	b := broker.New(stateStore)

	// Generic Engine (Phase 2): load ServiceDefinitions and wire them into
	// the handler layer. DEFINITIONS_DIR empty = definition-based services
	// disabled.
	engineHolder, err := handlers.NewEngineHolder(cfg.DefinitionsDir, cfg.PodNamespace, stateStore)
	if err != nil {
		log.Fatalf("load service definitions: %v", err)
	}

	// Initialize handlers
	h := handlers.New(b)
	h.SetEngine(engineHolder)

	// Zugriffsprotokoll (LOG_REQUESTS). Standard an - ein Broker, der stumm
	// laeuft, ist im Fehlerfall nicht nachvollziehbar.
	h.SetLogRequests(cfg.LogRequests)

	// Prometheus metrics (Phase 4.3). Enabled by default; METRICS_ENABLED=0
	// turns collection and the /metrics endpoint off.
	var metrics *handlers.Metrics
	if cfg.MetricsEnabled {
		metrics = handlers.NewMetrics()
		h.SetMetrics(metrics)
	}

	// Authentication (Phase 4.5). Any one configured method succeeding is
	// enough; an empty chain leaves the OSB endpoints open.
	h.SetAuthenticator(buildAuthChain(cfg))

	// Setup router
	router := h.SetupRouter()

	// TLS (Phase 4.5). The certificate is served through the reloader's
	// callbacks, so cert-manager renewals are picked up without a restart.
	var tlsConfig *tls.Config
	if cfg.TLS.Enabled {
		reloader, err := server.NewCertReloader(cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.ClientCAFile)
		if err != nil {
			log.Fatalf("tls: %v", err)
		}
		reloader.Start(ctx, cfg.TLS.ReloadInterval)
		tlsConfig = server.BuildTLSConfig(cfg.TLS, reloader)
	}

	srv := server.New(server.Options{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		TLS:               tlsConfig,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	})

	// Periodischer Abgleich bestehender Instanzen gegen die geladenen
	// Definitionen. Ausdruecklich einzuschalten (RECONCILE_INTERVAL), weil er
	// als Einziges ohne einen Request in fremde Namespaces schreibt.
	//
	// Der Zustandsspeicher muss aufzaehlen koennen. Kann er es nicht, bleibt
	// der Abgleich aus statt eine leere Liste als "nichts zu tun" zu lesen -
	// er meldete sonst jeden Durchlauf als Erfolg, ohne etwas geprueft zu
	// haben.
	if cfg.Reconcile.Interval > 0 {
		lister, ok := stateStore.(broker.Lister)
		switch {
		case !ok:
			log.Printf("reconcile: abgeschaltet - der Zustandsspeicher kann nicht aufzaehlen")
		case engineHolder == nil || engineHolder.Engine == nil:
			log.Printf("reconcile: abgeschaltet - es sind keine ServiceDefinitions geladen")
		default:
			r := reconcile.New(lister, engineHolder.Engine, reconcile.Options{
				Interval: cfg.Reconcile.Interval,
				Observer: metrics.ObserveReconcile,
			})
			go r.Run(ctx)
			log.Printf("Abgleich aktiv, alle %s", cfg.Reconcile.Interval)
		}
	}

	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	log.Printf("Starting OSB Broker on port %s (%s, auth: %v)", cfg.Port, scheme, cfg.Auth.Methods)
	if err := server.Run(ctx, srv, cfg.Server.ShutdownTimeout); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	log.Printf("OSB Broker stopped")
}

// buildAuthChain assembles the authenticators the configuration selected.
// The order follows cfg.Auth.Methods, which config normalises, so the
// WWW-Authenticate challenge order does not depend on how the operator
// spelled AUTH_METHODS.
func buildAuthChain(cfg *config.Config) *auth.Chain {
	var authenticators []auth.Authenticator
	for _, method := range cfg.Auth.Methods {
		switch method {
		case config.MethodBasic:
			authenticators = append(authenticators,
				auth.NewBasic(cfg.Auth.Basic.User, cfg.Auth.Basic.Password, cfg.Auth.Realm))
			log.Printf("Basic Auth enabled for user %q", cfg.Auth.Basic.User)
		case config.MethodMTLS:
			authenticators = append(authenticators,
				auth.NewMTLS(cfg.Auth.MTLS.AllowedCNs, cfg.Auth.MTLS.AllowedDNSNames, cfg.Auth.MTLS.AllowedURIs))
			log.Printf("mTLS enabled (client CA %s)", cfg.Auth.MTLS.ClientCAFile)
		}
	}
	return auth.NewChain(authenticators...)
}
