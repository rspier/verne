package main

import (
	"context" // Added for OTel
	"errors"  // Added for OTel
	"fmt"     // Added for OTel error formatting
	"log"
	"net/http" // For http.ErrServerClosed
	"os"
	"time" // Added for OTel

	"nntp-web/internal/cache"      // Added
	"nntp-web/internal/config"
	"nntp-web/internal/database"   // Added
	"nntp-web/internal/nntpclient" // Added
	"nntp-web/internal/ratelimit"  // Added
	"nntp-web/internal/server"     // Added

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0" // Use a specific version
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// initTracerProvider initializes an OTLP tracer provider.
func initTracerProvider(cfg *config.Config) (*sdktrace.TracerProvider, error) {
	if cfg.OTelExporterOTLPTracesEndpoint == "" {
		log.Println("OTel Tracing is disabled: OTelExporterOTLPTracesEndpoint is not set.")
		// Return a no-op provider or nil if you handle it upstream
		// For simplicity, otel.SetTracerProvider with a nil will default to no-op.
		return nil, nil
	}

	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.OTelServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Set up a connection to the OTLP exporter
	conn, err := grpc.NewClient(cfg.OTelExporterOTLPTracesEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()), // Use insecure for local/dev; use TLS in production
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP gRPC connection to %s: %w", cfg.OTelExporterOTLPTracesEndpoint, err)
	}

	// Set up a trace exporter
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Register the trace exporter with a B същоatchSpanProcessor to send spans in batches
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Sample all traces for now
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tp)

	// Set global propagator to W3C Trace Context (standard)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	log.Printf("OTel Tracing initialized with OTLP endpoint: %s, Service: %s", cfg.OTelExporterOTLPTracesEndpoint, cfg.OTelServiceName)
	return tp, nil
}

// initMeterProvider initializes a Prometheus meter provider.
func initMeterProvider(cfg *config.Config) (*metric.MeterProvider, error) {
	if !cfg.OTelMetricsEnabled {
		log.Println("OTel Metrics is disabled via configuration.")
		// otel.SetMeterProvider with a nil will default to no-op
		return nil, nil
	}

	ctx := context.Background()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.OTelServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource for metrics: %w", err)
	}

	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}

	mp := metric.NewMeterProvider(
		metric.WithReader(exporter),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	log.Printf("OTel Metrics initialized with Prometheus exporter. Ready to be scraped on /metrics (once server configures it).")
	return mp, nil
}

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

	// Initialize OpenTelemetry Tracing
	tp, err := initTracerProvider(cfg)
	if err != nil {
		log.Printf("Error initializing OTel Tracer Provider: %v. Tracing will be disabled.", err)
		// No need to exit, tracing is optional. tp might be nil.
	} else if tp != nil {
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Printf("Error shutting down OTel Tracer Provider: %v", err)
			}
		}()
		log.Println("OTel Tracer Provider initialized and shutdown deferred.")
	}

	// Initialize OpenTelemetry Metrics
	mp, err := initMeterProvider(cfg)
	if err != nil {
		log.Printf("Error initializing OTel Meter Provider: %v. Metrics will be disabled.", err)
		// No need to exit, metrics are optional. mp might be nil.
	} else if mp != nil {
		defer func() {
			if err := mp.Shutdown(context.Background()); err != nil {
				log.Printf("Error shutting down OTel Meter Provider: %v", err)
			}
		}()
		log.Println("OTel Meter Provider initialized and shutdown deferred.")
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

	// Initialize NNTP Client
	// Use context.Background() for initialization phase client setup
	nntpCtx := context.Background()
	nntpCli, err := nntpclient.NewClient(nntpCtx, cfg)
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
