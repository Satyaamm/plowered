// Package server wires the gRPC and HTTP listeners, applies the interceptor
// chain, and exposes a single Run function that blocks until shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	nethttp "net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/api/middleware"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/storage"
)

// Deps bundles concrete dependencies the server needs at construction time.
type Deps struct {
	Logger *slog.Logger
	Store  storage.Store
	Auth   middleware.AuthConfig
}

// Run starts both listeners and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg Config, deps Deps) error {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Store == nil {
		return errors.New("server: Store dependency required")
	}

	health := newHealthState()
	skip := skipMethods()

	grpcSrv := buildGRPCServer(cfg, deps, skip)
	// TODO(after `buf generate`): register service handlers here, e.g.
	//   catalogv1.RegisterCatalogServiceServer(grpcSrv, catalogHandler)
	_ = grpcSrv

	httpSrv := &nethttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           buildHTTPHandler(cfg, deps, health),
		ReadHeaderTimeout: 5 * time.Second,
	}

	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", cfg.GRPCAddr, err)
	}

	deps.Logger.Info("plowered listening",
		"grpc", cfg.GRPCAddr,
		"http", cfg.HTTPAddr,
		"env", cfg.Env,
		"version", cfg.Version,
	)

	if err := pingStore(ctx, deps.Store); err != nil {
		deps.Logger.Warn("initial store ping failed; serving anyway", "err", err)
	} else {
		health.markReady()
	}

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := grpcSrv.Serve(grpcLis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errs <- fmt.Errorf("grpc serve: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			errs <- fmt.Errorf("http serve: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		deps.Logger.Info("plowered shutting down")
	case err := <-errs:
		deps.Logger.Error("plowered listener exited", "err", err)
		shutdown(grpcSrv, httpSrv, cfg.ShutdownGrace)
		wg.Wait()
		return err
	}

	health.markNotReady()
	shutdown(grpcSrv, httpSrv, cfg.ShutdownGrace)
	wg.Wait()
	return nil
}

func buildGRPCServer(cfg Config, deps Deps, skip map[string]bool) *grpc.Server {
	authMW := middleware.Auth(deps.Auth)
	return grpc.NewServer(
		grpc.Creds(insecure.NewCredentials()),
		grpc.ChainUnaryInterceptor(
			middleware.Recovery(),
			middleware.RequestID(),
			middleware.Logging(deps.Logger),
			middleware.RateLimit(middleware.RateLimitConfig{
				PerSecond:   cfg.RateLimitPerSecond,
				Burst:       cfg.RateLimitBurst,
				SkipMethods: skip,
			}),
			authMW,
			middleware.Tenant(skip),
		),
		grpc.ChainStreamInterceptor(
			middleware.StreamRecovery(),
			middleware.StreamRequestID(),
		),
	)
}

func buildHTTPHandler(cfg Config, deps Deps, health *healthState) nethttp.Handler {
	mux := nethttp.NewServeMux()
	mux.Handle("/healthz", healthHandler(health, cfg.Version))
	mux.Handle("/readyz", healthHandler(health, cfg.Version))

	apiMux := apihttp.Mux(deps.Store)
	verify := buildHTTPVerifier(deps.Auth)
	chain := apihttp.Chain(apiMux,
		apihttp.RecoveryMW(deps.Logger),
		apihttp.RequestIDMW(),
		apihttp.LoggingMW(deps.Logger),
		apihttp.CORSMW(splitCSV(cfg.CORSAllowedOrigins)),
		apihttp.AuthMW(verify, "/healthz", "/readyz"),
		apihttp.TenantMW("/healthz", "/readyz"),
	)
	mux.Handle("/v1/", chain)
	return mux
}

// buildHTTPVerifier adapts the gRPC AuthConfig into an HTTP TokenVerifier so
// JWT semantics stay identical across both surfaces.
func buildHTTPVerifier(cfg middleware.AuthConfig) apihttp.TokenVerifier {
	if cfg.DevPrincipal != nil {
		dev := *cfg.DevPrincipal
		return func(_ string) (auth.Principal, error) { return dev, nil }
	}
	return func(token string) (auth.Principal, error) {
		return middleware.VerifyToken(cfg, token)
	}
}

func skipMethods() map[string]bool {
	return map[string]bool{
		"/grpc.health.v1.Health/Check":                                   true,
		"/grpc.health.v1.Health/Watch":                                   true,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":      true,
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": true,
	}
}

func shutdown(grpcSrv *grpc.Server, httpSrv *nethttp.Server, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-ctx.Done():
		grpcSrv.Stop()
	}
	_ = httpSrv.Shutdown(ctx)
}

func pingStore(ctx context.Context, s pingable) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.Ping(ctx)
}

func splitCSV(s string) []string {
	if s == "" {
		return []string{"*"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
