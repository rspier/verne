package main

import (
	"context"
	// "fmt" // Was unused
	"log"
	"net/http" // For http.ErrServerClosed
	"os"
	"os/signal"
	"syscall"
	// "time" // No longer used directly in this file after OTel init changes

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"nntp-web/internal/cache"      // Added
	"nntp-web/internal/config"
	"nntp-web/internal/database"   // Added
	"nntp-web/internal/nntpclient" // Added
	"nntp-web/internal/ratelimit"  // Added
	"nntp-web/internal/server"     // Added
)

const (
	serviceName = "nntp-web"
)

// initTracerProvider initializes an OTLP exporter and configures the trace provider.
func initTracerProvider(ctx context.Context) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			// The service name used to display traces in backends
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	// Set up a trace exporter
	// OTLP gRPC Exporter
	// Get endpoint from environment variable OTEL_EXPORTER_OTLP_ENDPOINT
	// Default to localhost:4317 if not set
	otelGRPCEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otelGRPCEndpoint == "" {
		otelGRPCEndpoint = "localhost:4317"
	}

	log.Printf("Initializing OTLP gRPC exporter with endpoint: %s", otelGRPCEndpoint)
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(), // Use insecure connection for simplicity, configure TLS in production
		otlptracegrpc.WithEndpoint(otelGRPCEndpoint),
	)
	if err != nil {
		return nil, err
	}

	// Register the trace exporter with a BATCH span processor to aggregate spans before export.
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // For development/testing, sample all traces
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)

	// Set global propagator to W3C Trace Context (the default)
	// and W3C Baggage, as recommended in the OpenTelemetry specs.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	// Return a shutdown function to ensure all spans are flushed before the application exits.
	return tracerProvider.Shutdown, nil
}

func main() {
	// Set up OpenTelemetry Tracing
	// Handle shutdown signals to ensure telemetry is flushed.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	shutdownTracer, err := initTracerProvider(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize OpenTelemetry tracer provider: %v", err)
	}
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil { // Use a new context for shutdown
			log.Printf("Error shutting down tracer provider: %v", err)
		}
		log.Println("Tracer provider shut down.")
	}()
	log.Println("OpenTelemetry tracer provider initialized.")

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
