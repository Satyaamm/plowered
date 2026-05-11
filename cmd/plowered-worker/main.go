// Command plowered-worker consumes async jobs (pipeline runs, quality
// checks) from Redis via Asynq and executes them. Runs as its own
// horizontally-scalable deployment.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	// Register the Snowflake driver with database/sql so the worker can
	// dial customer warehouses on crawl jobs. Side-effect import.
	_ "github.com/snowflakedb/gosnowflake"

	"github.com/Satyaamm/plowered/internal/adapters/bigquery_source"
	"github.com/Satyaamm/plowered/internal/adapters/postgres_source"
	"github.com/Satyaamm/plowered/internal/adapters/snowflake_source"
	"github.com/Satyaamm/plowered/internal/config"
	"github.com/Satyaamm/plowered/internal/core/classifier"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/qualitycheck"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/taskdeps"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/core/search"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/storage/postgres"
	"github.com/Satyaamm/plowered/internal/worker"
	"github.com/Satyaamm/plowered/pkg/llm/local"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, logger); err != nil {
		logger.Error("plowered-worker exited", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	if err := config.LoadDefault(); err != nil {
		return err
	}

	dbURL := os.Getenv("PLOWERED_DATABASE_URL")
	if dbURL == "" {
		return errors.New("PLOWERED_DATABASE_URL required for worker")
	}
	redisURL := os.Getenv("PLOWERED_REDIS_URL")
	if redisURL == "" {
		return errors.New("PLOWERED_REDIS_URL required for worker")
	}
	addr, db, password, err := parseRedisURL(redisURL)
	if err != nil {
		return fmt.Errorf("parse redis url: %w", err)
	}

	connectCtx, cancelConnect := context.WithTimeout(ctx, 10*time.Second)
	defer cancelConnect()
	pool, err := pgxpool.New(connectCtx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	pipelineStore := postgres.NewPipelineStore(pool)
	qualityStore := postgres.NewQualityStore(pool)

	qScheduler := quality.NewScheduler(quality.NewRunner(), quality.SchedulerConfig{})
	bus := events.NewMemoryBus() // worker-local bus; cluster bus lands in batch 5

	resolver := func(_ context.Context, _, _ string) (quality.DataSource, error) {
		return postgres.NewPoolDataSource(pool), nil
	}

	// Crawl wiring — same shape as the API process so a /v1/connections/
	// {id}/crawl job dispatched from there lands here ready to run.
	catalog := postgres.New(pool)
	connections := postgres.NewConnectionStore(pool)
	vault, err := buildVault(logger, pool)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	sources := worker.NewSourceRegistry()
	sources.Register(connection.TypePostgres, postgres_source.NewCrawler())
	sources.Register(connection.TypeSnowflake, snowflake_source.NewCrawler())
	sources.Register(connection.TypeBigQuery, bigquery_source.NewCrawler())

	registry := pipeline.NewRegistry()
	tasks.RegisterAll(registry, tasks.Deps{
		ConnFactory:     newConnFactory(connections, vault),
		QualityStore:    qualityStore,
		QualityRunner:   qScheduler,
		QualityResolver: qualitycheck.Resolver(resolver),
		ColumnLineage:   postgres.NewColumnLineageStore(pool),
	})
	logStore := postgres.NewLogStore(pool)
	runner := &pipeline.Runner{
		Store:    pipelineStore,
		Registry: registry,
		Events:   bus,
		Logs:     logStore,
		Logger:   logger,
		Now:      time.Now,
	}

	jobsStore := postgres.NewJobsStore(pool)
	classifyOrch := &classifier.Orchestrator{
		Catalog: postgres.NewClassifyCatalog(pool),
		Sampler: &classifier.Sampler{
			Dialer:     newConnFactory(connections, vault),
			SampleSize: 200,
		},
		Sink: postgres.Sink{
			Store:   postgres.NewClassificationStore(pool),
			Catalog: postgres.NewClassifyCatalog(pool),
		},
		Logger: logger,
	}
	searchIndexer := &search.Indexer{
		Catalog:  catalog,
		Provider: local.New(),
		Store:    postgres.NewEmbeddingStore(pool),
	}

	handlers := &worker.Handlers{
		Logger:         logger,
		Pipelines:      pipelineStore,
		Quality:        qualityStore,
		Scheduler:      qScheduler,
		Runner:         runner,
		Resolver:       resolver,
		Catalog:        catalog,
		Connections:    connections,
		Vault:          vault,
		SourceRegistry: sources,
		Events:         bus,
		Jobs:           jobsStore,
		Classifier:     classifyOrch,
		SearchIndexer:  searchIndexer,
	}

	cfg := worker.AsynqConfig{
		RedisAddr:     addr,
		RedisDB:       db,
		RedisPassword: password,
		Concurrency:   envIntDefault("PLOWERED_WORKER_CONCURRENCY", 10),
	}
	srv := worker.NewAsynqServer(cfg, handlers, logger)

	logger.Info("plowered-worker started",
		"redis", addr, "concurrency", cfg.Concurrency,
		"postgres", redactDSN(dbURL))

	<-ctx.Done()
	logger.Info("plowered-worker shutting down")
	srv.Stop()
	srv.Shutdown()
	return nil
}

// newConnFactory wires the connection.Repo + secrets.Vault into a closure
// the SQL/transform/copy executors can call to dial customer Postgres.
// Mirrors cmd/plowered's newConnFactory so both binaries support the same
// task types identically.
func newConnFactory(conns connection.Repo, vault secrets.Vault) taskdeps.ConnFactory {
	if conns == nil || vault == nil {
		return nil
	}
	return func(ctx context.Context, tenantID, connID string) (*pgx.Conn, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, fmt.Errorf("load connection %q: %w", connID, err)
		}
		if c.Type != connection.TypePostgres {
			return nil, fmt.Errorf("connection %q: only postgres is supported in v0 (got %s)", connID, c.Type)
		}
		var secret []byte
		if c.SecretURN != "" {
			b, err := vault.Get(ctx, tenantID, c.SecretURN)
			if err != nil {
				return nil, fmt.Errorf("read vault: %w", err)
			}
			secret = b
		}
		dsn, err := postgres_source.BuildDSN(c.Config, secret)
		if err != nil {
			return nil, err
		}
		return pgx.Connect(ctx, dsn)
	}
}

// buildVault mirrors the API process. Master key resolution:
//  1. PLOWERED_SECRETS_MASTER_KEY env var
//  2. PLOWERED_SECRETS_MASTER_KEY_FILE — read from a mounted file
//  3. (dev only) generate ephemeral key + warn
//
// Production fails closed when 1 + 2 are both empty.
func buildVault(logger *slog.Logger, pool *pgxpool.Pool) (secrets.Vault, error) {
	key := os.Getenv("PLOWERED_SECRETS_MASTER_KEY")
	if key == "" {
		if path := os.Getenv("PLOWERED_SECRETS_MASTER_KEY_FILE"); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read master key file %q: %w", path, err)
			}
			key = strings.TrimSpace(string(data))
		}
	}
	if key == "" {
		if os.Getenv("PLOWERED_ENV") == "production" {
			return nil, fmt.Errorf("PLOWERED_SECRETS_MASTER_KEY (or _FILE) is required in production")
		}
		k, err := secrets.GenerateMasterKey()
		if err != nil {
			return nil, err
		}
		logger.Warn("secrets: PLOWERED_SECRETS_MASTER_KEY (or _FILE) unset — generated ephemeral dev key. Restarts will leave existing secrets unreadable.")
		key = k
	}
	return secrets.NewAESVault(key, postgres.NewSecretsStore(pool))
}

// parseRedisURL splits redis://[:password@]host:port/db into Asynq fields.
func parseRedisURL(s string) (addr string, db int, password string, err error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", 0, "", err
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return "", 0, "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	addr = u.Host
	if pw, ok := u.User.Password(); ok {
		password = pw
	}
	if p := strings.TrimPrefix(u.Path, "/"); p != "" {
		_, err = fmt.Sscanf(p, "%d", &db)
		if err != nil {
			return "", 0, "", fmt.Errorf("redis db must be int, got %q", p)
		}
	}
	return addr, db, password, nil
}

func envIntDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return def
	}
	return n
}

func redactDSN(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		if at := strings.Index(dsn[i+3:], "@"); at >= 0 {
			return dsn[:i+3] + "***@" + dsn[i+3+at+1:]
		}
	}
	return dsn
}
