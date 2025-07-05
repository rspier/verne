package main

import (
	// "fmt" // Was unused
	"log"
	"net/http" // For http.ErrServerClosed
	"os"

	"nntp-web/internal/cache"      // Added
	"nntp-web/internal/config"
	"nntp-web/internal/database"   // Added
	"nntp-web/internal/nntpclient" // Added
	"nntp-web/internal/ratelimit"  // Added
	"nntp-web/internal/server"     // Added
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Error loading configuration: %v\n", err)
		// Print usage if config loading fails (e.g. missing required flag)
		// Note: The flag package prints default usage on error if -h or --help is not used.
		// For more custom messages, you might need to handle flag.ErrHelp explicitly or use a different flag library.
		// For now, a simple error message is sufficient.
		os.Exit(1)
	}

	// Print the loaded configuration for verification
	log.Printf("Configuration loaded successfully:")
	log.Printf("  DB Host: %s", cfg.DBHost)
	log.Printf("  DB Port: %d", cfg.DBPort)
	log.Printf("  DB User: %s", cfg.DBUser)
	// Consider redacting DBPass in logs for security: log.Printf("  DB Pass: %s", "****")
	log.Printf("  DB Pass: %s", cfg.DBPass)
	log.Printf("  DB Name: %s", cfg.DBName)
	log.Printf("  Server Port: %d", cfg.ServerPort)
	log.Printf("  NNTP Server: %s", cfg.NNTPServer)
	log.Printf("  Bot Protection Enabled: %t", cfg.BotProtectionEnabled)
	if cfg.BotProtectionEnabled {
		log.Printf("    Rate Limit Threshold: %d", cfg.RateLimitThreshold)
		log.Printf("    Rate Limit Period: %s", cfg.RateLimitPeriod)
		// Do not log RecaptchaSecret or RecaptchaSiteKey directly for security
		log.Printf("    Recaptcha Site Key: %s", "**** (configured)") // Example of masking
	}

	// Initialize RateLimiter if bot protection is enabled
	var rl *ratelimit.RateLimiter
	if cfg.BotProtectionEnabled {
		log.Println("Bot protection is enabled. Initializing rate limiter.")
		rl = ratelimit.NewRateLimiter(cfg.RateLimitThreshold, cfg.RateLimitPeriod)
		defer rl.Stop() // Ensure cleanup goroutine is stopped on shutdown
	} else {
		log.Println("Bot protection is disabled.")
	}

	// Initialize Cache
	appCache := cache.NewCache()
	log.Println("In-memory cache initialized.")

	// Initialize Database Connection
	db, err := database.New(cfg, appCache) // Pass cache to DB
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()
	log.Println("Successfully connected to the database.")

	// Initialize NNTP Client (placeholder)
	nntpCli, err := nntpclient.NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create NNTP client: %v", err)
	}
	defer func() {
		if err := nntpCli.Close(); err != nil {
			log.Printf("Error closing NNTP client: %v", err)
		}
	}()
	log.Println("NNTP client initialized.")


	// Initialize HTTP Server
	srv, err := server.NewServer(cfg, db, nntpCli, rl) // Pass RateLimiter (can be nil)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start HTTP Server
	log.Printf("Starting server on port %d. Access at http://localhost:%d/group/", cfg.ServerPort, cfg.ServerPort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
	}
	log.Println("Server shut down gracefully.")
}
