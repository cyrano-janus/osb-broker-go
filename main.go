package main

import (
	"crypto/tls"
	"log"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/config"
	"github.com/example/osb-broker/internal/handlers"
	"github.com/example/osb-broker/internal/server"
	"github.com/example/osb-broker/internal/store"
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

	// Initialize in-memory catalog store
	serviceStore := store.NewInMemoryStore()

	// State store selection (Phase 1.1): "k8s" persists instances/bindings
	// in a ConfigMap so they survive pod restarts; "memory" is the default
	// for local runs and tests.
	var stateStore broker.StateStore
	switch cfg.StoreBackend {
	case "k8s":
		restCfg, err := k8sconfig.GetConfig()
		if err != nil {
			log.Fatalf("load kubeconfig: %v", err)
		}
		k8sClient, err := broker.NewK8sClient(restCfg)
		if err != nil {
			log.Fatalf("build k8s client: %v", err)
		}
		stateStore = broker.NewK8sStateStore(k8sClient, cfg.PodNamespace)
		log.Printf("State store: kubernetes ConfigMap in namespace %q", cfg.PodNamespace)
	default:
		stateStore = broker.NewInMemoryStateStore()
		log.Printf("WARNING: STORE_BACKEND unset - using in-memory state (lost on restart)")
	}

	// Initialize broker
	b := broker.New(serviceStore, stateStore)

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

	// Prometheus metrics (Phase 4.3). Enabled by default; METRICS_ENABLED=0
	// turns collection and the /metrics endpoint off.
	if cfg.MetricsEnabled {
		h.SetMetrics(handlers.NewMetrics())
	}

	// Basic Auth credentials (Phase 1.2). In Kubernetes these come from a
	// Secret mounted as env vars. Both empty = auth disabled.
	if cfg.Auth.Basic.User != "" || cfg.Auth.Basic.Password != "" {
		h.SetBasicAuthCredentials(cfg.Auth.Basic.User, cfg.Auth.Basic.Password)
		log.Printf("Basic Auth enabled for user %q", cfg.Auth.Basic.User)
	}

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
