package main

import (
	"fmt"
	"log"
	"net/http"
	"nntp-browser-app/backend/pkg/api"
	"nntp-browser-app/backend/pkg/config"
	"os"
	"strconv"
)

func main() {
	// Load configuration (basic for now)
	// In a real app, this would come from a file or env vars more robustly.
	cfg := config.AppConfig{
		NNTPServer: os.Getenv("NNTP_SERVER"), // e.g., "news.example.com:119"
		ServerPort: 8080,                     // Default backend server port
	}

	portStr := os.Getenv("BACKEND_PORT")
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			cfg.ServerPort = p
		} else {
			log.Printf("Warning: Invalid BACKEND_PORT value '%s', using default %d", portStr, cfg.ServerPort)
		}
	}

	if cfg.NNTPServer == "" {
		log.Println("Warning: NNTP_SERVER environment variable not set. Using mock data.")
		// For now, the handlers are mock, so this is just a placeholder warning.
		// Later, this might mean we can't connect to a real NNTP server.
	}


	// Setup router
	router := api.Router()

	// Start server
	log.Printf("Starting server on port %d", cfg.ServerPort)
	log.Printf("API Endpoints available at http://localhost:%d/api/", cfg.ServerPort)
	log.Printf(" - GET /api/groups")
	log.Printf(" - GET /api/groups/{groupName}/messages")
	log.Printf(" - GET /api/groups/{groupName}/messages/{messageId}")

	err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.ServerPort), router)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
