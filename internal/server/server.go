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
	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/dsr"
	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/glossary"
	"github.com/Satyaamm/plowered/internal/core/identity"
	"github.com/Satyaamm/plowered/internal/core/jobs"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/notify"
	"github.com/Satyaamm/plowered/internal/core/outbox"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/core/search"
	"github.com/Satyaamm/plowered/internal/obs"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/worker"
)

// Deps bundles concrete dependencies the server needs at construction time.
// Catalog Store is required; the orchestration repos are optional — pass nil
// to skip registering those routes.
type Deps struct {
	Logger    *slog.Logger
	Store     storage.Store
	Auth      middleware.AuthConfig
	Pipelines pipeline.Repo
	Quality   quality.Store
	Notify    notify.Repo
	Policies  policy.RuleRepo
	Audit       audit.Reader
	AuditWriter audit.Writer
	Deleted     deleted.Repo
	LegalHolds  legalhold.Repo
	DSR         dsr.Repo
	Identity    identity.Repo
	Email       email.Sender
	AuthCfg     apihttp.AuthConfig
	Connections connection.Repo
	ConnRegistry *connection.Registry
	Vault       secrets.Vault
	OutboxWriter outbox.Writer
	OutboxReader outbox.Reader
	Enqueuer    worker.Enqueuer
	Events    events.Bus // optional; wired so the metrics recorder can subscribe
	Metrics   *obs.Metrics // optional; when nil, /metrics is not exposed
	Logs      pipeline.LogReader // optional; powers /v1/runs/{id}/logs and the SSE tail
	ColumnLineage apihttp.ColumnLineageReader // optional
	Glossary  glossary.Repo // optional; powers /v1/glossary/*
	Classifier      apihttp.Classifier
	Classifications apihttp.ClassificationReader
	Profiler        apihttp.Profiler
	Describer       apihttp.Describer
	Asker           apihttp.Asker
	SearchIndexer   *search.Indexer
	SearchSearcher  *search.Searcher
	Jobs            jobs.Repo
	AIProviders     aiprovider.Repo
}

// Run starts both listeners and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg Config, deps Deps) error {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Store == nil {
		return errors.New("server: Store dependency required")
	}
	if deps.Metrics != nil && deps.Events != nil {
		deps.Events.Subscribe(obs.EventRecorder{M: deps.Metrics})
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
	if deps.Metrics != nil {
		mux.Handle("/metrics", deps.Metrics.Handler())
	}
	// Public docs: OpenAPI spec + Swagger UI. Not under /v1/ so the
	// auth chain skips them by default.
	apihttp.DocsHandlers(mux)

	apiMux := apihttp.NewMux(apihttp.Deps{
		Catalog:   deps.Store,
		Pipelines: deps.Pipelines,
		Quality:   deps.Quality,
		Notify:    deps.Notify,
		Policies:  deps.Policies,
		Audit:       deps.Audit,
		AuditWriter: deps.AuditWriter,
		Deleted:     deps.Deleted,
		LegalHolds:  deps.LegalHolds,
		DSR:         deps.DSR,
		Identity:    deps.Identity,
		Email:       deps.Email,
		AuthCfg:     deps.AuthCfg,
		Connections: deps.Connections,
		ConnRegistry: deps.ConnRegistry,
		Vault:       deps.Vault,
		Enqueuer:    deps.Enqueuer,
		Logs:        deps.Logs,
		ColumnLineage: deps.ColumnLineage,
		Glossary:    deps.Glossary,
		Classifier:        deps.Classifier,
		Classifications:   deps.Classifications,
		Profiler:          deps.Profiler,
		Describer:         deps.Describer,
		Asker:             deps.Asker,
		SearchIndexer:     deps.SearchIndexer,
		SearchSearcher:    deps.SearchSearcher,
		Jobs:              deps.Jobs,
		AIProviders:       deps.AIProviders,
	})
	// Public endpoints — never require auth. /v1/auth/me + /v1/auth/logout
	// deliberately omitted: those need an active session.
	skipAuth := []string{
		"/healthz",
		"/readyz",
		"/metrics",
		"/v1/auth/signup",
		"/v1/auth/login",
		"/v1/auth/verify",
		"/v1/auth/resend-verification",
		"/v1/auth/invite-info",
		"/v1/auth/accept-invite",
		"/v1/auth/forgot-password",
		"/v1/auth/reset-password",
	}
	verify := buildHTTPVerifier(deps.Auth)

	// Pick session-aware auth when identity is wired; fall back to bearer-only
	// AuthMW for memory-mode dev (no DB → no sessions).
	var authMW apihttp.Middleware
	if deps.Identity != nil {
		authMW = apihttp.SessionAuthMW(deps.Identity, deps.AuthCfg.CookieName, verify, skipAuth...)
	} else {
		authMW = apihttp.AuthMW(verify, skipAuth...)
	}

	chain := apihttp.Chain(apiMux,
		apihttp.RecoveryMW(deps.Logger),
		apihttp.RequestIDMW(),
		apihttp.LoggingMW(deps.Logger),
		// HSTS / CSP / X-Content-Type-Options / Frame / Referrer /
		// Permissions / COOP / CORP. Standard hardening expected by
		// SOC2 CC6.1 and OWASP secure-headers checklist.
		apihttp.SecurityHeadersMW(),
		apihttp.CORSMW(splitCSV(cfg.CORSAllowedOrigins)),
		// Per-IP rate limit on the credential-mutation endpoints. 5/min,
		// burst 8 — generous for a human, lethal for a brute-force bot.
		// Lives BEFORE auth so unauthenticated probes can be throttled.
		apihttp.AuthRateLimitMW(5, 8),
		authMW,
		apihttp.TenantMW(skipAuth...),
		// Per-principal rate limit on the authenticated surface.
		// Reads 120/min, writes 30/min. Lives AFTER auth so the key is
		// the principal ID (not IP), letting legitimate users behind a
		// NAT keep working at full speed.
		apihttp.APIRateLimitMW(120, 30, skipAuth...),
		apihttp.AuditMW(deps.AuditWriter, "plowered", cfg.Version, skipAuth...),
	)
	if deps.Metrics != nil {
		chain = deps.Metrics.HTTPMiddleware(chain)
	}
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
