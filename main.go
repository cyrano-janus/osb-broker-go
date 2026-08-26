package main

import (
	"log"
	"os"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/handlers"
	"github.com/example/osb-broker/internal/store"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	// Initialize in-memory catalog store
	serviceStore := store.NewInMemoryStore()

	// State store selection (Phase 1.1): "k8s" persists instances/bindings
	// in a ConfigMap so they survive pod restarts; "memory" is the default
	// for local runs and tests.
	var stateStore broker.StateStore
	switch os.Getenv("STORE_BACKEND") {
	case "k8s":
		namespace := os.Getenv("POD_NAMESPACE")
		if namespace == "" {
			log.Fatal("STORE_BACKEND=k8s requires POD_NAMESPACE")
		}
		cfg, err := config.GetConfig()
		if err != nil {
			log.Fatalf("load kubeconfig: %v", err)
		}
		k8sClient, err := broker.NewK8sClient(cfg)
		if err != nil {
			log.Fatalf("build k8s client: %v", err)
		}
		stateStore = broker.NewK8sStateStore(k8sClient, namespace)
		log.Printf("State store: kubernetes ConfigMap in namespace %q", namespace)
	default:
		stateStore = broker.NewInMemoryStateStore()
		log.Printf("WARNING: STORE_BACKEND unset - using in-memory state (lost on restart)")
	}

	// Initialize broker
	b := broker.New(serviceStore, stateStore)

	// Generic Engine (Phase 2): load ServiceDefinitions and wire them into
	// the handler layer. DEFINITIONS_DIR empty = definition-based services
	// disabled.
	engineHolder, err := handlers.NewEngineHolder(os.Getenv("DEFINITIONS_DIR"), os.Getenv("POD_NAMESPACE"), stateStore)
	if err != nil {
		log.Fatalf("load service definitions: %v", err)
	}

	// Initialize handlers
	h := handlers.New(b)
	h.SetEngine(engineHolder)

	// Prometheus metrics (Phase 4.3). Enabled by default; METRICS_ENABLED=0
	// turns collection and the /metrics endpoint off.
	if os.Getenv("METRICS_ENABLED") != "0" {
		h.SetMetrics(handlers.NewMetrics())
	}

	// Basic Auth credentials (Phase 1.2). In Kubernetes these come from a
	// Secret mounted as env vars. Both empty = auth disabled.
	authUser := os.Getenv("BROKER_AUTH_USER")
	authPass := os.Getenv("BROKER_AUTH_PASSWORD")
	if authUser != "" || authPass != "" {
		h.SetBasicAuthCredentials(authUser, authPass)
		log.Printf("Basic Auth enabled for user %q", authUser)
	} else {
		log.Printf("WARNING: Basic Auth disabled - set BROKER_AUTH_USER/BROKER_AUTH_PASSWORD for production use")
	}

	// Setup router
	router := h.SetupRouter()

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting OSB Broker on port %s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
