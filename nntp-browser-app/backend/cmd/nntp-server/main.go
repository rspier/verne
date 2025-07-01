package main

import (
	"fmt"
	"log"
	"net/http"
	"nntp-browser-app/backend/pkg/api"
	"nntp-browser-app/backend/pkg/config"
	"os"
	"strconv"

	"github.com/rs/cors" // Import the cors package
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
	// router := api.Router() // Old way

	// Initialize APIHandler
	apiHandler, err := api.NewAPIHandler(cfg)
	if err != nil {
		// err is already logged by NewAPIHandler if it's just a warning about client init.
		// If NewAPIHandler were to return a fatal error, we might stop here.
		// For now, we assume it can proceed even if client is nil (handlers will check).
		log.Printf("Continuing to start server even if API handler init had issues: %v", err)
	}
	defer apiHandler.Close() // Ensure NNTP client connection is closed on exit

	// Initialize router with the API handler
	httpRouter := api.Router(apiHandler)


	// Setup CORS
	// This allows requests from Vite's default dev server (localhost:5173)
	// and common React dev ports.
	// For production, you'd want to restrict this to your frontend's actual domain.
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"http://localhost:5173", "http://localhost:3000"}, // Add other origins if needed
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		// Debug: true, // Enable for debugging
	})
	handler := c.Handler(httpRouter) // Use httpRouter here

	// Start server
	log.Printf("Starting server on port %d", cfg.ServerPort)
	log.Printf("CORS enabled for http://localhost:5173 and http://localhost:3000")
	log.Printf("API Endpoints available at http://localhost:%d/api/", cfg.ServerPort)
	log.Printf(" - GET /api/groups")
	log.Printf(" - GET /api/groups/{groupName}/messages")
	log.Printf(" - GET /api/groups/{groupName}/messages/{messageId}")

	// err was already declared by apiHandler, err := api.NewAPIHandler(cfg)
	err = http.ListenAndServe(fmt.Sprintf(":%d", cfg.ServerPort), handler) // Use the CORS wrapped handler
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
