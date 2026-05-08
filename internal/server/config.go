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
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Env:           getenvDefault("PLOWERED_ENV", "dev"),
		Version:       getenvDefault("PLOWERED_VERSION", "dev"),
		GRPCAddr:      getenvDefault("PLOWERED_GRPC_ADDR", ":9090"),
		HTTPAddr:      getenvDefault("PLOWERED_HTTP_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("PLOWERED_DATABASE_URL"),
		ShutdownGrace: parseDuration("PLOWERED_SHUTDOWN_GRACE", 10*time.Second),
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
