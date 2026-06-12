package server

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the typed view over environment variables that drives the
// server. Build with LoadConfig(); never read os.Getenv inside packages
// other than this one.
type Config struct {
	Env         string        // "dev" | "staging" | "production"
	Version     string        // build version (set via -ldflags)
	GRPCAddr    string        // host:port for the gRPC listener
	HTTPAddr    string        // host:port for the HTTP listener (health, metrics, REST gateway later)
	DatabaseURL string        // PostgreSQL connection string; if empty, in-memory store
	ShutdownGrace time.Duration

	// Rate limit defaults (per tenant)
	RateLimitPerSecond float64
	RateLimitBurst     int

	// CORSAllowedOrigins is a comma-separated list of EXACT origins
	// permitted to hit the HTTP API from a browser. Wildcards are not
	// supported (credentialled CORS forbids them anyway). Empty leaves
	// CORS disabled — appropriate when the API is reached only by
	// server-side callers (BFFs / curl / SDKs) and never directly by a
	// browser on a different origin.
	CORSAllowedOrigins string

	// CORSAllowCredentials decides whether the API echoes
	// Access-Control-Allow-Credentials. Must be true for cookie-based
	// session auth across origins; harmless when only bearer tokens
	// are used.
	CORSAllowCredentials bool

	// GatewaySecret is a shared secret nginx (or a frontend BFF) must
	// send as X-Gateway-Auth on every request. When set, requests
	// missing or mismatching the header get 401 before any other
	// middleware runs. Use to keep random scanners off the API even
	// when the auth handler itself is on a public surface.
	GatewaySecret string
	// GatewaySecretSkipPaths is a comma-separated list of path prefixes
	// the gateway-auth check skips. /healthz + /readyz + /metrics must
	// stay reachable for the load balancer; default lists them.
	GatewaySecretSkipPaths string
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Env:           getenvDefault("PLOWERED_ENV", "dev"),
		Version:       getenvDefault("PLOWERED_VERSION", "dev"),
		GRPCAddr:      getenvDefault("PLOWERED_GRPC_ADDR", ":9090"),
		HTTPAddr:      getenvDefault("PLOWERED_HTTP_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("PLOWERED_DATABASE_URL"),
		ShutdownGrace:      parseDuration("PLOWERED_SHUTDOWN_GRACE", 10*time.Second),
		CORSAllowedOrigins: os.Getenv("PLOWERED_CORS_ALLOWED_ORIGINS"),
		CORSAllowCredentials: getenvDefault("PLOWERED_CORS_ALLOW_CREDENTIALS", "true") == "true",
		GatewaySecret:        os.Getenv("PLOWERED_GATEWAY_SECRET"),
		GatewaySecretSkipPaths: getenvDefault(
			"PLOWERED_GATEWAY_SKIP_PATHS",
			"/healthz,/readyz,/metrics,/docs,/openapi.yaml",
		),
	}

	rps, err := parseFloat("PLOWERED_RATE_LIMIT_PER_SECOND", 50)
	if err != nil {
		return cfg, err
	}
	cfg.RateLimitPerSecond = rps

	burst, err := parseInt("PLOWERED_RATE_LIMIT_BURST", 100)
	if err != nil {
		return cfg, err
	}
	cfg.RateLimitBurst = burst

	return cfg, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func parseFloat(key string, def float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}

func parseInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return i, nil
}
