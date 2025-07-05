package config

import (
	"fmt"
	// "os" // No longer needed for Getenv if jnovack/flag handles env vars automatically
	// "strconv" // No longer needed for Atoi if jnovack/flag handles env vars automatically

	flag "github.com/jnovack/flag"
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
	LogSQLQueries   bool   // For verbose SQL query logging
}

// Load parses command-line flags and environment variables to populate the Config struct.
// github.com/jnovack/flag is expected to handle environment variables automatically.
// For a flag like -db-host, it should check for an environment variable DB_HOST.
func Load() (*Config, error) {
	cfg := &Config{}

	// Define flags using the standard signature. jnovack/flag will handle env var lookup.
	// These return pointers, so we'll dereference them.
	dbHost := flag.String("db-host", "localhost", "Database host (env: DB_HOST)")
	dbPort := flag.Int("db-port", 3306, "Database port (env: DB_PORT)")
	dbUser := flag.String("db-user", "user", "Database user (env: DB_USER)")
	dbPass := flag.String("db-pass", "password", "Database password (env: DB_PASS)")
	dbName := flag.String("db-name", "nntp_cache", "Database name (env: DB_NAME)")
	serverPort := flag.Int("server-port", 8080, "HTTP server port (env: SERVER_PORT)")
	nntpServer := flag.String("nntp-server", "news.example.com:119", "NNTP server address (host:port) (env: NNTP_SERVER)")
	cacheTTLSeconds := flag.Int("cache-ttl", 300, "Default cache TTL in seconds (e.g., 300 for 5 minutes) (env: CACHE_TTL)")
	logSQLQueries := flag.Bool("log-sql", false, "Enable verbose logging of SQL queries (env: LOG_SQL)")

	flag.Parse()

	// Assign parsed values
	cfg.DBHost = *dbHost
	cfg.DBPort = *dbPort
	cfg.DBUser = *dbUser
	cfg.DBPass = *dbPass
	cfg.DBName = *dbName
	cfg.ServerPort = *serverPort
	cfg.NNTPServer = *nntpServer
	cfg.CacheTTLSeconds = *cacheTTLSeconds
	cfg.LogSQLQueries = *logSQLQueries

	// Basic validation
	if cfg.NNTPServer == "" {
		return nil, fmt.Errorf("nntp-server address is required (set via -nntp-server flag or NNTP_SERVER env var)")
	}
	if cfg.CacheTTLSeconds < 0 {
		return nil, fmt.Errorf("cache-ttl must be non-negative")
	}

	return cfg, nil
}
