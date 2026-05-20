// Command plowered runs the API server: gRPC + HTTP (health, REST gateway later).
package main

import (
	"context"
	"database/sql"
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

	// Register the Snowflake driver with database/sql so the Snowflake
	// adapter can dial customer warehouses. Blank-import: side-effect
	// only — the driver registers itself in its init().
	_ "github.com/snowflakedb/gosnowflake"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/api/middleware"
	"github.com/Satyaamm/plowered/internal/adapters/athena_source"
	// bigquery_driver init() registers itself with bigquery_source as the
	// active driver; it also exposes NewExecutor for the warehouse factory.
	"github.com/Satyaamm/plowered/internal/adapters/bigquery_driver"
	"github.com/Satyaamm/plowered/internal/adapters/bigquery_source"
	"github.com/Satyaamm/plowered/internal/adapters/dynamodb_source"
	"github.com/Satyaamm/plowered/internal/adapters/mongodb_source"
	"github.com/Satyaamm/plowered/internal/adapters/postgres_source"
	"github.com/Satyaamm/plowered/internal/adapters/snowflake_source"
	"github.com/Satyaamm/plowered/internal/config"
	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/classifier"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/dsr"
	emailpkg "github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/identity"
	"github.com/Satyaamm/plowered/internal/core/jobs"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/lineage"
	"github.com/Satyaamm/plowered/internal/core/notify"
	"github.com/Satyaamm/plowered/internal/core/outbox"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/qualitycheck"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/taskdeps"
	"github.com/Satyaamm/plowered/internal/core/aictx"
	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/internal/core/asker"
	"github.com/Satyaamm/plowered/internal/core/blob"
	"github.com/Satyaamm/plowered/internal/core/certification"
	"github.com/Satyaamm/plowered/internal/core/contract"
	"github.com/Satyaamm/plowered/internal/core/cost"
	"github.com/Satyaamm/plowered/internal/core/describer"
	"github.com/Satyaamm/plowered/internal/core/migration"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/profile"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/core/search"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/core/warehouse"
	"github.com/Satyaamm/plowered/internal/obs"
	"github.com/Satyaamm/plowered/internal/scheduler"
	"github.com/Satyaamm/plowered/pkg/llm/local"
	"github.com/Satyaamm/plowered/internal/server"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
	"github.com/Satyaamm/plowered/internal/storage/postgres"
	"github.com/Satyaamm/plowered/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if len(os.Args) >= 2 && os.Args[1] != "serve" {
		logger.Error("unknown command", "cmd", os.Args[1])
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, logger); err != nil {
		logger.Error("plowered exited with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	if err := config.LoadDefault(); err != nil {
		return err
	}
	cfg, err := server.LoadConfig()
	if err != nil {
		return err
	}
	authCfg, err := middleware.LoadAuthConfigFromEnv()
	if err != nil {
		return err
	}

	deps, cleanup, err := buildDeps(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	deps.Auth = authCfg
	deps.Logger = logger

	startScheduler(ctx, logger, deps)
	startOutboxRelay(ctx, logger, deps)
	startContractRunner(ctx, logger, deps)
	startCostWatcher(ctx, logger, deps)
	return server.Run(ctx, cfg, deps)
}

// startCostWatcher periodically checks each tenant's rolling cost
// against their monthly budget and fires CheckFailed events when the
// warn/hard thresholds are crossed. Disabled when no budgets are
// configured (no rows in cost_budgets).
func startCostWatcher(ctx context.Context, logger *slog.Logger, deps server.Deps) {
	if deps.CostBudgets == nil || deps.CostTenants == nil || deps.Events == nil {
		return
	}
	if os.Getenv("PLOWERED_COST_WATCHER_DISABLED") == "1" {
		logger.Info("cost watcher: disabled via PLOWERED_COST_WATCHER_DISABLED=1")
		return
	}
	interval := 15 * time.Minute
	if v := os.Getenv("PLOWERED_COST_WATCHER_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	w := &cost.Watcher{
		Budgets:  deps.CostBudgets,
		Tenants:  deps.CostTenants,
		Events:   deps.Events,
		Interval: interval,
		Logger:   logger,
	}
	w.Start(ctx)
	logger.Info("cost watcher: armed", "interval", interval)
}

// startContractRunner spins up the periodic contract evaluator. Reads
// the interval from PLOWERED_CONTRACT_INTERVAL (default 5m). Disabled
// when no Contract service is wired (memory mode).
func startContractRunner(ctx context.Context, logger *slog.Logger, deps server.Deps) {
	if deps.Contract == nil || deps.ContractTenants == nil {
		return
	}
	if os.Getenv("PLOWERED_CONTRACT_RUNNER_DISABLED") == "1" {
		logger.Info("contract runner: disabled via PLOWERED_CONTRACT_RUNNER_DISABLED=1")
		return
	}
	interval := 5 * time.Minute
	if v := os.Getenv("PLOWERED_CONTRACT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	runner := &contract.Runner{
		Service:  deps.Contract,
		Tenants:  deps.ContractTenants,
		Interval: interval,
		Logger:   logger,
	}
	runner.Start(ctx)
	logger.Info("contract runner: armed", "interval", interval)
}

// startOutboxRelay spins up the relay loop that reads unprocessed rows
// from the outbox table and forwards them to NATS (or LogPublisher when
// NATS is unset). The relay is always-safe to run — when no rows exist
// it ticks idle. Set PLOWERED_OUTBOX_DISABLED=1 to disable, e.g. in
// test deployments where multiple replicas would over-publish.
func startOutboxRelay(ctx context.Context, logger *slog.Logger, deps server.Deps) {
	if os.Getenv("PLOWERED_OUTBOX_DISABLED") == "1" {
		logger.Info("outbox: disabled via PLOWERED_OUTBOX_DISABLED=1")
		return
	}
	if deps.OutboxReader == nil {
		logger.Info("outbox: no reader configured — relay skipped (memory backend)")
		return
	}
	pub := buildOutboxPublisher(logger)
	relay := outbox.Relay{
		Reader:    deps.OutboxReader,
		Publisher: pub,
		Logger:    logger,
		Cfg: outbox.Config{
			BatchSize:    envIntDefault("PLOWERED_OUTBOX_BATCH", 100),
			TickInterval: envDuration("PLOWERED_OUTBOX_TICK", 2*time.Second),
		},
	}
	go relay.Run(ctx)
}

func buildOutboxPublisher(logger *slog.Logger) outbox.Publisher {
	natsURL := os.Getenv("PLOWERED_NATS_URL")
	if natsURL == "" {
		logger.Info("outbox: PLOWERED_NATS_URL unset — using LogPublisher")
		return outbox.LogPublisher{Logger: logger}
	}
	logger.Info("outbox: NATSPublisher armed", "url", natsURL)
	return outbox.NewNATSPublisher(natsURL)
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

// startScheduler kicks off the cron + reaper loops in a goroutine. Disabled
// by setting PLOWERED_SCHEDULER_DISABLED=1 (useful in test deploys with
// multiple replicas where you don't want every pod scheduling).
func startScheduler(ctx context.Context, logger *slog.Logger, deps server.Deps) {
	if os.Getenv("PLOWERED_SCHEDULER_DISABLED") == "1" {
		logger.Info("scheduler: disabled via PLOWERED_SCHEDULER_DISABLED=1")
		return
	}
	if deps.Pipelines == nil || deps.Enqueuer == nil {
		logger.Warn("scheduler: pipelines or enqueuer missing; skipping")
		return
	}
	s := scheduler.New(deps.Pipelines, deps.Enqueuer)
	s.Logger = logger
	s.Config = scheduler.Config{
		CronInterval:   envDuration("PLOWERED_SCHEDULER_CRON_INTERVAL", 30*time.Second),
		ReaperInterval: envDuration("PLOWERED_SCHEDULER_REAPER_INTERVAL", time.Minute),
		StuckAfter:     envDuration("PLOWERED_SCHEDULER_STUCK_AFTER", 5*time.Minute),
	}
	go s.Run(ctx)
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// buildDeps wires either the in-memory or Postgres-backed stores plus the
// async work enqueuer. The returned cleanup must be called before exit.
func buildDeps(ctx context.Context, cfg server.Config, logger *slog.Logger) (server.Deps, func(), error) {
	bus := events.NewMemoryBus()
	metrics, err := obs.NewMetrics()
	if err != nil {
		return server.Deps{}, nil, fmt.Errorf("obs: %w", err)
	}

	if cfg.DatabaseURL == "" {
		logger.Info("storage: using in-memory backend (PLOWERED_DATABASE_URL unset)")
		mem := memory.New()
		pStore := pipeline.NewMemoryStore()
		qStore := quality.NewMemoryStore()
		auditW := audit.NewMemoryWriter()

		// Memory mode still spins a vault — tests that exercise the
		// connection routes need somewhere to store secrets even when
		// we're not talking to Postgres. There's no in-memory connection
		// repo yet, so the SQL/transform/copy executors will fail at
		// execute time in this mode (with a clear error). Memory mode is
		// a dev fallback only; production always runs the pg branch.
		memVaultKey, _ := secrets.GenerateMasterKey()
		memVault, _ := secrets.NewAESVault(memVaultKey, secrets.NewMemoryStorage())
		memRegistry := connection.NewRegistry()
		memRegistry.Register(connection.TypePostgres, postgres_source.New())

		handlers := buildWorkerHandlers(logger, pStore, qStore, nil, bus, nil, memVault, nil, workerExtras{})
		enq := worker.NewSyncEnqueuer(handlers)
		return server.Deps{
			Store:       mem,
			Pipelines:   pStore,
			Quality:     qStore,
			Notify:      notify.NewMemoryStore(),
			Policies:    policy.NewMemoryRuleStore(),
			Audit:       auditW,
			AuditWriter: auditW,
			Deleted:     deleted.NewMemoryRepo(),
			LegalHolds:  legalhold.NewMemoryRepo(),
			DSR:         dsr.NewMemoryRepo(),
			Identity:    identity.NewMemoryRepo(),
			Email:       emailpkg.LogSender{Logger: logger},
			AuthCfg:     buildAuthCfg(logger),
			Vault:       memVault,
			ConnRegistry: memRegistry,
			Enqueuer:    enq,
			Events:      bus,
			Metrics:     metrics,
		}, func() { _ = mem.Close() }, nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectCtx, cfg.DatabaseURL)
	if err != nil {
		return server.Deps{}, nil, errors.Join(errors.New("storage: connect postgres"), err)
	}

	if err := postgres.Migrate(connectCtx, pool); err != nil {
		pool.Close()
		return server.Deps{}, nil, errors.Join(errors.New("storage: migrate"), err)
	}

	cat := postgres.New(pool)
	pStore := postgres.NewPipelineStore(pool)
	qStore := postgres.NewQualityStore(pool)
	logger.Info("storage: postgres backend ready", "url", redactDSN(cfg.DatabaseURL))

	// Build vault + connection repo before the enqueuer so the in-process
	// SyncEnqueuer's pipeline runner can dial customer datasources for
	// SQL / transform_run / connector_sync tasks.
	vault, err := buildVault(logger, pool)
	if err != nil {
		pool.Close()
		return server.Deps{}, nil, err
	}
	connectionStore := postgres.NewConnectionStore(pool)
	registry := connection.NewRegistry()
	registry.Register(connection.TypePostgres, postgres_source.New())
	registry.Register(connection.TypeSnowflake, snowflake_source.New())
	registry.Register(connection.TypeBigQuery, bigquery_source.New())
	registry.Register(connection.TypeMongoDB, mongodb_source.New())
	registry.Register(connection.TypeDynamoDB, dynamodb_source.New())
	registry.Register(connection.TypeAthena, athena_source.New())

	logStore := postgres.NewLogStore(pool)
	jobsStore := postgres.NewJobsStore(pool)
	classifyOrch := &classifier.Orchestrator{
		Catalog: postgres.NewClassifyCatalog(pool),
		Sampler: newMultiSampler(connectionStore, vault),
		Sink: postgres.Sink{
			Store:   postgres.NewClassificationStore(pool),
			Catalog: postgres.NewClassifyCatalog(pool),
		},
		Logger: logger,
	}
	// Profiler runs per-column profile jobs. Sits on top of the new
	// warehouse abstraction so it works against Postgres / Snowflake /
	// MySQL / Redshift without further per-driver code here.
	profileStore := postgres.NewProfileStore(pool)
	warehouseFactory := newWarehouseFactory(connectionStore, vault)
	objectStore := buildObjectStore(ctx, logger)
	profilerSvc := &profile.Service{
		Reader:    profileStore,
		Cache:     profileStore,
		Warehouse: warehouseFactory,
		Mirror:    objectStore,
		Logger:    logger,
	}
	// Describer: AI-driven description suggestions. Holds the resolver
	// (tenant → chat provider) + a context builder that mixes catalog
	// metadata with profile samples. Both reuse infra above; no new
	// dependencies on this row.
	aiProvidersRepo := postgres.NewAIProviderStore(pool)
	aiResolver := aiprovider.NewResolver(aiProvidersRepo, vault)
	aiDescStore := postgres.NewAIDescriptionStore(pool, cat)
	contextBuilder := &aictx.Builder{
		Assets:   aiDescStore,
		Tables:   profileStore,
		Profiles: profileStore,
	}
	costStore := postgres.NewCostStore(pool)
	contractStore := postgres.NewContractStore(pool)
	describerSvc := &describer.Service{
		Context:  contextBuilder,
		Resolver: aiResolver,
		Log:      aiDescStore,
		Cost:     costStore,
		Mirror:   objectStore,
		Logger:   logger,
	}
	embeddingStore := postgres.NewEmbeddingStore(pool)
	searchIndexer := &search.Indexer{
		Catalog:  cat,
		Provider: local.New(),
		Store:    embeddingStore,
	}
	// Asker: Text-to-SQL. Reuses every shared piece — context builder,
	// resolver, warehouse factory — and adds the semantic searcher
	// for top-K table selection.
	askSearcher := &search.Searcher{Catalog: cat, Provider: local.New(), Store: embeddingStore}
	askerSvc := &asker.Service{
		Context:   contextBuilder,
		Resolver:  aiResolver,
		Search:    postgres.NewAISemanticSearcher(pool, askSearcher, connectionStore),
		Conns:     connectionStore,
		Warehouse: warehouseFactory,
		Log:       postgres.NewAIQueryStore(pool),
		Cost:      costStore,
		Mirror:    objectStore,
		Logger:    logger,
	}
	// Migration: SQL → SQL data movement. Reuses the warehouse factory
	// (no new driver code) and lives alongside profile/describer/asker
	// in the same "needs warehouse access" tier.
	//
	// Object storage: when PLOWERED_S3_BUCKET is set we use S3 for
	// durable artifact storage (migration checkpoints today, profile
	// snapshots + AI logs in follow-ups). Otherwise an in-memory
	// store keeps single-process dev working without AWS creds.
	migrationSvc := &migration.Service{
		Store:      postgres.NewMigrationStore(pool),
		Warehouse:  warehouseFactory,
		Conns:      connectionStore,
		Checkpoint: migration.NewBlobCheckpointStore(objectStore),
		Events:     bus,
		Cost:       costStore,
		Logger:     logger,
	}
	extras := workerExtras{
		Jobs:              jobsStore,
		Classifier:        classifyOrch,
		Indexer:           searchIndexer,
		MigrationExecutor: migrationSvc,
	}
	enq, enqClose, err := buildEnqueuer(logger, pStore, qStore, pool, bus, connectionStore, vault, logStore, extras)
	if err != nil {
		pool.Close()
		return server.Deps{}, nil, err
	}

	auditStore := postgres.NewAuditStore(pool)
	// LegalHolds: persisted impl awaits the user/tenant signup flow that
	// populates `tenants(id)` rows the legal_holds FK depends on. Until
	// then we run an in-memory repo so the API surface and the gate are
	// both live — issuances simply don't survive a restart.
	// LegalHolds and DSR were on MemoryRepo until the signup flow started
	// populating tenants(id). Now that's real, both move to Postgres so
	// holds and DSR clocks survive a restart and an audit can re-read
	// them six years later.
	holdsRepo := postgres.NewLegalHoldStore(pool)
	dsrRepo := postgres.NewDSRStore(pool)
	outboxStore := postgres.NewOutboxStore(pool)
	identityStore := postgres.NewIdentityStore(pool)
	emailSender := buildEmailSender(logger)
	authCfg := buildAuthCfg(logger)
	notifyStore := postgres.NewNotifyStore(pool)
	// Dispatcher subscribes to the event bus and turns matching events
	// into Delivery rows on the configured channels. Without this wiring
	// notify rules persist but never fire, which was the gap until this
	// session — now check/migration lifecycle events route through here.
	notifyDispatcher := notify.NewDispatcher(notifyStore)
	notifyDispatcher.Logger = logger
	notifyDispatcher.Workers = envIntDefault("PLOWERED_NOTIFY_WORKERS", 4)
	notifyDispatcher.QueueSize = envIntDefault("PLOWERED_NOTIFY_QUEUE_SIZE", 512)
	notifyDispatcher.Register(&notify.LogChannel{Logger: logger})
	notifyDispatcher.Register(notify.NewWebhookChannel())
	notifyDispatcher.Register(notify.NewSlackChannel())
	notifyDispatcher.Register(&notify.EmailChannel{Sender: emailSender, DefaultFrom: authCfg.FromAddress})
	notifyDispatcher.Start(ctx)
	bus.Subscribe(notifyDispatcher)
	return server.Deps{
			Store:       cat,
			Pipelines:   pStore,
			Quality:     qStore,
			Notify:      notifyStore,
			Policies:    postgres.NewPolicyStore(pool),
			Audit:       auditStore,
			AuditWriter: auditStore,
			Deleted:     postgres.NewDeletedStore(pool),
			LegalHolds:  holdsRepo,
			DSR:         dsrRepo,
			Identity:    identityStore,
			Email:       emailSender,
			AuthCfg:     authCfg,
			Connections: connectionStore,
			ConnRegistry: registry,
			Vault:       vault,
			OutboxWriter: outboxStore,
			OutboxReader: outboxStore,
			Enqueuer:    enq,
			Events:      bus,
			Metrics:     metrics,
			Logs:        logStore,
			ColumnLineage: postgres.NewColumnLineageStore(pool),
			Glossary:    postgres.NewGlossaryStore(pool),
			Classifier:      classifyOrch,
			Classifications: postgres.NewClassificationStore(pool),
			Profiler:        profilerSvc,
			Describer:       describerSvc,
			Asker:           askerSvc,
			Migrator:        migrationSvc,
			SearchIndexer:   searchIndexer,
			SearchSearcher: &search.Searcher{
				Catalog:  cat,
				Provider: local.New(),
				Store:    embeddingStore,
			},
			Jobs:        jobsStore,
			AIProviders: postgres.NewAIProviderStore(pool),
			Certification: &certification.Service{
				Store: postgres.NewCertificationStore(pool),
			},
			Cost:        costStore,
			CostBudgets: costStore,
			CostTenants: costStore,
			Contract: func() *contract.Service {
				return &contract.Service{
					Store:   contractStore,
					Profile: profileStore,
					Events:  bus,
					Logger:  logger,
				}
			}(),
			ContractTenants: contractStore,
		}, func() {
			if enqClose != nil {
				_ = enqClose()
			}
			pool.Close()
			_ = cat.Close()
		}, nil
}

// buildWorkerHandlers constructs the in-process Handlers struct shared by
// the SyncEnqueuer and (in the worker binary) the Asynq server. The
// supplied bus is what the runner publishes lifecycle events on; the API
// process subscribes the metrics recorder to the same bus.
//
// conns + vault may be nil — only the SQL/transform/copy task executors
// need them, and they fail with a clear "no connection factory" error
// at execute time when missing. logs may be nil — the runner falls back
// to a no-op sink so older binaries keep working.
// workerExtras carries optional dependencies for the long-running async
// jobs (classify + reindex). They're only needed when the sync or worker
// path executes those task types; the API process can omit them.
type workerExtras struct {
	Jobs              jobs.Repo
	Classifier        *classifier.Orchestrator
	Indexer           *search.Indexer
	MigrationExecutor worker.MigrationExecutor
}

func buildWorkerHandlers(logger *slog.Logger, pStore pipeline.Repo, qStore quality.Store, pool *pgxpool.Pool, bus events.Bus, conns connection.Repo, vault secrets.Vault, logs pipeline.LogSink, extras workerExtras) *worker.Handlers {
	qScheduler := quality.NewScheduler(quality.NewRunner(), quality.SchedulerConfig{})
	resolver := func(_ context.Context, _, _ string) (quality.DataSource, error) {
		if pool == nil {
			return nil, fmt.Errorf("no pool available — datasource resolution requires postgres backend")
		}
		return postgres.NewPoolDataSource(pool), nil
	}
	registry := pipeline.NewRegistry()
	var colSink lineage.ColumnSink
	if pool != nil {
		colSink = postgres.NewColumnLineageStore(pool)
	}
	tasks.RegisterAll(registry, tasks.Deps{
		ConnFactory:     newConnFactory(conns, vault),
		QualityStore:    qStore,
		QualityRunner:   qScheduler,
		QualityResolver: qualitycheck.Resolver(resolver),
		ColumnLineage:   colSink,
	})
	runner := &pipeline.Runner{
		Store:    pStore,
		Registry: registry,
		Events:   bus,
		Logs:     logs,
		Logger:   logger,
		Now:      time.Now,
	}
	return &worker.Handlers{
		Logger:            logger,
		Pipelines:         pStore,
		Quality:           qStore,
		Scheduler:         qScheduler,
		Runner:            runner,
		Resolver:          resolver,
		Events:            bus,
		Jobs:              extras.Jobs,
		Classifier:        extras.Classifier,
		SearchIndexer:     extras.Indexer,
		MigrationExecutor: extras.MigrationExecutor,
	}
}

// newConnFactory wires the connection.Repo + secrets.Vault into a closure
// the SQL/transform/copy executors can call to dial customer Postgres.
// Returns nil when either dep is nil so the executors error out cleanly
// instead of panicking on a half-wired runner.
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

// newMultiSampler wires a connection-type-aware classifier.Sampler.
// Postgres reuses newConnFactory (pgx.Conn per call). Snowflake opens a
// fresh *sql.DB per call; gosnowflake handles internal pooling and the
// sampler tears the DB down via its own caller-owned lifecycle.
// BigQuery returns ErrDriverNotInstalled because v0 ships without a
// BigQuery driver.
func newMultiSampler(conns connection.Repo, vault secrets.Vault) *classifier.MultiSampler {
	if conns == nil || vault == nil {
		return nil
	}
	resolver := func(ctx context.Context, tenantID, connID string) (connection.Type, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return "", err
		}
		return c.Type, nil
	}
	pgDialer := func(ctx context.Context, tenantID, connID string) (*pgx.Conn, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, fmt.Errorf("load connection %q: %w", connID, err)
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
	sfDialer := func(ctx context.Context, tenantID, connID string) (*sql.DB, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, fmt.Errorf("load connection %q: %w", connID, err)
		}
		var secret []byte
		if c.SecretURN != "" {
			b, err := vault.Get(ctx, tenantID, c.SecretURN)
			if err != nil {
				return nil, fmt.Errorf("read vault: %w", err)
			}
			secret = b
		}
		dsn, err := snowflake_source.BuildDSN(c.Config, secret)
		if err != nil {
			return nil, err
		}
		return sql.Open("snowflake", dsn)
	}
	return &classifier.MultiSampler{
		ResolveType: resolver,
		Postgres:    &classifier.PostgresSampler{Dialer: pgDialer, SampleSize: 200},
		Snowflake:   &classifier.SnowflakeSampler{Dialer: sfDialer, SampleSize: 200},
		BigQuery:    classifier.BigQuerySampler{},
	}
}

// newWarehouseFactory wires the single SQL-execution surface every
// AI-driven feature depends on: profile, describe (later), text-to-SQL
// (later). It registers one Factory per supported connection.Type;
// adding a new SQL warehouse (Redshift, MySQL, BigQuery, Athena) is a
// matter of dropping in another factory case here — no other code in
// the platform changes.
func newWarehouseFactory(conns connection.Repo, vault secrets.Vault) *warehouse.MultiFactory {
	if conns == nil || vault == nil {
		return nil
	}
	resolver := func(ctx context.Context, tenantID, connID string) (string, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return "", err
		}
		return string(c.Type), nil
	}
	loadSecret := func(ctx context.Context, c *connection.Connection) ([]byte, error) {
		if c.SecretURN == "" {
			return nil, nil
		}
		return vault.Get(ctx, c.TenantID, c.SecretURN)
	}

	mf := warehouse.NewMultiFactory(resolver)

	mf.Register(string(connection.TypePostgres), func(ctx context.Context, tenantID, connID string) (warehouse.Executor, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, err
		}
		secret, err := loadSecret(ctx, c)
		if err != nil {
			return nil, err
		}
		dsn, err := postgres_source.BuildDSN(c.Config, secret)
		if err != nil {
			return nil, err
		}
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return nil, err
		}
		return warehouse.NewPostgresExecutor(conn), nil
	})

	// Redshift speaks the Postgres wire protocol — same dialer.
	// Per-source config keys differ (Redshift uses cluster endpoint),
	// but postgres_source.BuildDSN already handles host/port/db/user
	// uniformly so the same builder works.
	mf.Register(string(connection.TypeRedshift), func(ctx context.Context, tenantID, connID string) (warehouse.Executor, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, err
		}
		secret, err := loadSecret(ctx, c)
		if err != nil {
			return nil, err
		}
		dsn, err := postgres_source.BuildDSN(c.Config, secret)
		if err != nil {
			return nil, err
		}
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return nil, err
		}
		return warehouse.NewPostgresExecutor(conn), nil
	})

	mf.Register(string(connection.TypeSnowflake), func(ctx context.Context, tenantID, connID string) (warehouse.Executor, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, err
		}
		secret, err := loadSecret(ctx, c)
		if err != nil {
			return nil, err
		}
		dsn, err := snowflake_source.BuildDSN(c.Config, secret)
		if err != nil {
			return nil, err
		}
		db, err := sql.Open("snowflake", dsn)
		if err != nil {
			return nil, err
		}
		return warehouse.NewSQLExecutor(db), nil
	})

	// Athena uses the AWS SDK + StartQueryExecution; the executor
	// drives the poll loop and paginates GetQueryResults.
	mf.Register(string(connection.TypeAthena), func(ctx context.Context, tenantID, connID string) (warehouse.Executor, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, err
		}
		secret, err := loadSecret(ctx, c)
		if err != nil {
			return nil, err
		}
		return athena_source.NewExecutor(ctx, c.Config, secret)
	})

	// BigQuery uses the GCP Go client (cloud.google.com/go/bigquery).
	// The driver package's init() registers itself with bigquery_source
	// so Tester/Crawler also work; here we plug the warehouse path.
	mf.Register(string(connection.TypeBigQuery), func(ctx context.Context, tenantID, connID string) (warehouse.Executor, error) {
		c, err := conns.Get(ctx, tenantID, connID)
		if err != nil {
			return nil, err
		}
		secret, err := loadSecret(ctx, c)
		if err != nil {
			return nil, err
		}
		return bigquery_driver.NewExecutor(ctx, c.Config, secret)
	})

	// SQL drivers not compiled this build register as stubs so the
	// dispatcher returns ErrDriverNotInstalled rather than a confusing
	// "unsupported type" — clearer signal to the operator.
	for _, t := range []connection.Type{
		connection.TypeMySQL, // MySQL driver not compiled this session
	} {
		typ := t
		mf.Register(string(typ), func(_ context.Context, _, _ string) (warehouse.Executor, error) {
			return warehouse.NotInstalledExecutor{Type: string(typ)}, nil
		})
	}

	return mf
}

// buildObjectStore returns the durable artifact store. If
// PLOWERED_S3_BUCKET is set, we wire an S3-backed store using the
// standard AWS credential chain. Otherwise we fall back to an
// in-memory store — fine for single-process dev, NOT for production.
//
// Optional env: PLOWERED_S3_REGION (default us-east-1),
// PLOWERED_S3_ENDPOINT (MinIO / R2 compatibility),
// PLOWERED_S3_PATH_STYLE=1 (path-style addressing for MinIO).
func buildObjectStore(ctx context.Context, logger *slog.Logger) blob.ObjectStore {
	bucket := os.Getenv("PLOWERED_S3_BUCKET")
	if bucket == "" {
		logger.Info("object_store: PLOWERED_S3_BUCKET unset; using in-memory store (NOT durable)")
		return blob.NewInMem()
	}
	region := os.Getenv("PLOWERED_S3_REGION")
	if region == "" {
		region = "us-east-1"
	}
	s3Store, err := blob.NewS3(ctx, blob.S3Config{
		Bucket:       bucket,
		Region:       region,
		BaseEndpoint: os.Getenv("PLOWERED_S3_ENDPOINT"),
		UsePathStyle: os.Getenv("PLOWERED_S3_PATH_STYLE") == "1",
	})
	if err != nil {
		logger.Error("object_store: S3 init failed; falling back to in-memory", "err", err)
		return blob.NewInMem()
	}
	logger.Info("object_store: S3 ready", "bucket", bucket, "region", region)
	return s3Store
}

// buildEnqueuer picks Asynq when PLOWERED_REDIS_URL is set, sync fallback
// otherwise. The returned closer is non-nil only for AsynqEnqueuer.
func buildEnqueuer(logger *slog.Logger, pStore pipeline.Repo, qStore quality.Store, pool *pgxpool.Pool, bus events.Bus, conns connection.Repo, vault secrets.Vault, logs pipeline.LogSink, extras workerExtras) (worker.Enqueuer, func() error, error) {
	redisURL := os.Getenv("PLOWERED_REDIS_URL")
	if redisURL == "" {
		logger.Info("worker: PLOWERED_REDIS_URL unset; using in-process sync enqueuer")
		return worker.NewSyncEnqueuer(buildWorkerHandlers(logger, pStore, qStore, pool, bus, conns, vault, logs, extras)), nil, nil
	}
	addr, db, password, err := parseRedisURL(redisURL)
	if err != nil {
		return nil, nil, fmt.Errorf("worker: parse redis url: %w", err)
	}
	enq := worker.NewAsynqEnqueuer(worker.AsynqConfig{
		RedisAddr: addr, RedisDB: db, RedisPassword: password,
	})
	logger.Info("worker: asynq enqueuer ready", "redis", addr)
	return enq, enq.Close, nil
}

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

// redactDSN strips username/password before logging the database URL.
func redactDSN(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		if at := strings.Index(dsn[i+3:], "@"); at >= 0 {
			return dsn[:i+3] + "***@" + dsn[i+3+at+1:]
		}
	}
	return dsn
}

// buildVault initialises the AES-GCM secrets vault. Master key
// resolution order (standard Docker convention):
//
//  1. PLOWERED_SECRETS_MASTER_KEY env var — takes precedence
//  2. PLOWERED_SECRETS_MASTER_KEY_FILE — read the file's contents
//  3. (dev only) generate ephemeral key and warn
//
// In production mode (PLOWERED_ENV=production) we fail closed if 1 and 2
// are both empty — sealed secrets that can't be re-opened across a
// restart are worse than no boot at all.
func buildVault(logger *slog.Logger, pool *pgxpool.Pool) (secrets.Vault, error) {
	key := os.Getenv("PLOWERED_SECRETS_MASTER_KEY")
	if key == "" {
		if path := os.Getenv("PLOWERED_SECRETS_MASTER_KEY_FILE"); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("vault: read master key from %q: %w", path, err)
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
			return nil, fmt.Errorf("vault: generate dev key: %w", err)
		}
		logger.Warn("secrets: PLOWERED_SECRETS_MASTER_KEY (or _FILE) unset — generated an ephemeral dev key. Set the env var or mount a key file to make stored secrets survive restarts.")
		key = k
	}
	storage := postgres.NewSecretsStore(pool)
	return secrets.NewAESVault(key, storage)
}

// buildEmailSender returns a Resend-backed sender when PLOWERED_RESEND_API_KEY
// is set; otherwise a LogSender that writes the message to slog (link in
// the container logs is enough for local dev).
func buildEmailSender(logger *slog.Logger) emailpkg.Sender {
	key := os.Getenv("PLOWERED_RESEND_API_KEY")
	if key == "" {
		logger.Info("email: PLOWERED_RESEND_API_KEY unset — verification emails will be logged only")
		return emailpkg.LogSender{Logger: logger}
	}
	return emailpkg.NewResendSender(key)
}

// buildAuthCfg reads the cookie / from-address / web-base-url settings
// the auth handlers need. Defaults are local-dev friendly.
func buildAuthCfg(logger *slog.Logger) apihttp.AuthConfig {
	return apihttp.AuthConfig{
		WebBaseURL:   firstNonEmptyEnv("PLOWERED_WEB_BASE_URL", "http://localhost:3000"),
		FromAddress:  firstNonEmptyEnv("PLOWERED_EMAIL_FROM", "Plowered <onboarding@resend.dev>"),
		CookieName:   firstNonEmptyEnv("PLOWERED_SESSION_COOKIE", "plowered_session"),
		CookieDomain: os.Getenv("PLOWERED_SESSION_COOKIE_DOMAIN"),
		CookieSecure: os.Getenv("PLOWERED_SESSION_COOKIE_SECURE") == "1",
		Logger:       logger,
	}
}

func firstNonEmptyEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var _ storage.Store = (*memory.Store)(nil)
