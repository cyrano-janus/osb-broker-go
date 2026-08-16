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
	b := broker.New(serviceStore)

	// Initialize handlers
	h := handlers.New(b)

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