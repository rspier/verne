package config

import (
	"fmt"
	// "os" // No longer needed for Getenv if jnovack/flag handles env vars automatically
	// "strconv" // No longer needed for Atoi if jnovack/flag handles env vars automatically
	"time" // Added for time.Duration

	flag "github.com/jnovack/flag"
)

// Config holds the application configuration.
type Config struct {
	DBHost             string
	DBPort             int
	DBUser             string
	DBPass             string
	DBName             string
	ServerPort         int
	NNTPServer         string // Expected format: "host:port"
	CacheTTLSeconds    int    // Cache TTL in seconds
	LogSQLQueries      bool   // For verbose SQL query logging

	// Bot Protection (optional)
	BotProtectionEnabled bool
	RecaptchaSecret      string
	RecaptchaSiteKey     string
	RateLimitThreshold   int
	RateLimitPeriod      time.Duration

	// OpenTelemetry (optional)
	OTelServiceName                string
	OTelExporterOTLPTracesEndpoint string // e.g., "localhost:4317" for gRPC
	OTelMetricsEnabled             bool
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

	// Bot protection flags
	recaptchaSecret := flag.String("recaptcha-secret", "", "Google reCAPTCHA secret key (env: RECAPTCHA_SECRET)")
	recaptchaSiteKey := flag.String("recaptcha-sitekey", "", "Google reCAPTCHA site key (env: RECAPTCHA_SITEKEY)")
	rateLimitThreshold := flag.Int("ratelimit-threshold", 100, "Number of requests per period before CAPTCHA (env: RATELIMIT_THRESHOLD)")
	rateLimitPeriod := flag.Duration("ratelimit-period", 15*time.Minute, "Time period for rate limit (e.g., 15m, 1h) (env: RATELIMIT_PERIOD)")

	// OpenTelemetry flags
	otelServiceName := flag.String("otel-service-name", "nntp-web", "OpenTelemetry service name (env: OTEL_SERVICE_NAME)")
	otelExporterOTLPTracesEndpoint := flag.String("otel-exporter-otlp-traces-endpoint", "", "OpenTelemetry OTLP traces exporter endpoint (e.g., localhost:4317 for gRPC). If empty, tracing is disabled. (env: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT)")
	otelMetricsEnabled := flag.Bool("otel-metrics-enabled", true, "Enable OpenTelemetry metrics endpoint (/metrics) (env: OTEL_METRICS_ENABLED)")

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

	// Assign bot protection values and determine if it's enabled
	cfg.RecaptchaSecret = *recaptchaSecret
	cfg.RecaptchaSiteKey = *recaptchaSiteKey
	cfg.RateLimitThreshold = *rateLimitThreshold
	cfg.RateLimitPeriod = *rateLimitPeriod

	// Assign OpenTelemetry values
	cfg.OTelServiceName = *otelServiceName
	cfg.OTelExporterOTLPTracesEndpoint = *otelExporterOTLPTracesEndpoint
	cfg.OTelMetricsEnabled = *otelMetricsEnabled

	if cfg.RecaptchaSecret != "" && cfg.RecaptchaSiteKey != "" {
		cfg.BotProtectionEnabled = true
	} else {
		cfg.BotProtectionEnabled = false
	}

	// Basic validation
	if cfg.NNTPServer == "" {
		return nil, fmt.Errorf("nntp-server address is required (set via -nntp-server flag or NNTP_SERVER env var)")
	}
	if cfg.CacheTTLSeconds < 0 {
		return nil, fmt.Errorf("cache-ttl must be non-negative")
	}

	// Validate bot protection flags only if it's intended to be enabled
	if cfg.BotProtectionEnabled {
		if cfg.RateLimitThreshold <= 0 {
			return nil, fmt.Errorf("ratelimit-threshold must be positive when bot protection is enabled")
		}
		if cfg.RateLimitPeriod <= 0 {
			return nil, fmt.Errorf("ratelimit-period must be positive when bot protection is enabled")
		}
	} else {
		// If either key is provided but not both, it's a misconfiguration
		if cfg.RecaptchaSecret != "" && cfg.RecaptchaSiteKey == "" {
			return nil, fmt.Errorf("recaptcha-sitekey must be provided if recaptcha-secret is set (or omit both to disable bot protection)")
		}
		if cfg.RecaptchaSecret == "" && cfg.RecaptchaSiteKey != "" {
			return nil, fmt.Errorf("recaptcha-secret must be provided if recaptcha-sitekey is set (or omit both to disable bot protection)")
		}
	}

	return cfg, nil
}
