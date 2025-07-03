package config

import (
	"flag" // Standard library flag
	"fmt"
	"os"
	"strconv"
)

// Config holds the application configuration.
type Config struct {
	DBHost          string
	DBPort          int
	DBUser          string
	DBPass          string
	DBName          string
	ServerPort      int
	NNTPServer      string // Expected format: "host:port"
	CacheTTLSeconds int    // Cache TTL in seconds
}

// Load parses command-line flags and environment variables to populate the Config struct.
// Command-line flags take precedence over environment variables.
func Load() (*Config, error) {
	cfg := &Config{}

	// Define flags using standard library
	flag.StringVar(&cfg.DBHost, "db-host", "localhost", "Database host")
	flag.IntVar(&cfg.DBPort, "db-port", 3306, "Database port")
	flag.StringVar(&cfg.DBUser, "db-user", "user", "Database user")
	flag.StringVar(&cfg.DBPass, "db-pass", "password", "Database password")
	flag.StringVar(&cfg.DBName, "db-name", "nntp_cache", "Database name")
	flag.IntVar(&cfg.ServerPort, "server-port", 8080, "HTTP server port")
	flag.StringVar(&cfg.NNTPServer, "nntp-server", "news.example.com:119", "NNTP server address (host:port)")
	flag.IntVar(&cfg.CacheTTLSeconds, "cache-ttl", 300, "Default cache TTL in seconds (e.g., 300 for 5 minutes)")

	// Environment variable overrides
	if host := os.Getenv("NNTPWEB_DB_HOST"); host != "" {
		cfg.DBHost = host
	}
	if portStr := os.Getenv("NNTPWEB_DB_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.DBPort = port
		} else {
			return nil, fmt.Errorf("invalid NNTPWEB_DB_PORT: %w", err)
		}
	}
	if user := os.Getenv("NNTPWEB_DB_USER"); user != "" {
		cfg.DBUser = user
	}
	if pass := os.Getenv("NNTPWEB_DB_PASS"); pass != "" {
		cfg.DBPass = pass
	}
	if name := os.Getenv("NNTPWEB_DB_NAME"); name != "" {
		cfg.DBName = name
	}
	if portStr := os.Getenv("NNTPWEB_SERVER_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.ServerPort = port
		} else {
			return nil, fmt.Errorf("invalid NNTPWEB_SERVER_PORT: %w", err)
		}
	}
	if nntpServer := os.Getenv("NNTPWEB_NNTP_SERVER"); nntpServer != "" {
		cfg.NNTPServer = nntpServer
	}
	if ttlStr := os.Getenv("NNTPWEB_CACHE_TTL_SECONDS"); ttlStr != "" {
		if ttl, err := strconv.Atoi(ttlStr); err == nil {
			cfg.CacheTTLSeconds = ttl
		} else {
			return nil, fmt.Errorf("invalid NNTPWEB_CACHE_TTL_SECONDS: %w", err)
		}
	}

	// Parse flags after setting defaults and checking environment variables
	if !flag.Parsed() {
		flag.Parse()
	}

	// Basic validation
	if cfg.NNTPServer == "" {
		return nil, fmt.Errorf("nntp-server address is required")
	}
	if cfg.CacheTTLSeconds < 0 {
		return nil, fmt.Errorf("cache-ttl must be non-negative")
	}

	return cfg, nil
}
