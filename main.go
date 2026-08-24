package main

import (
	"log"
	"os"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/handlers"
	"github.com/example/osb-broker/internal/store"
)

func main() {
	// Initialize in-memory store
	serviceStore := store.NewInMemoryStore()

	// Initialize broker with default catalog
	b := broker.New(serviceStore, nil)

	// Initialize handlers
	h := handlers.New(b)

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