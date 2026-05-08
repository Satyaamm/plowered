// Command plowered runs the API server: gRPC + HTTP (health, REST gateway later).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Satyaamm/plowered/internal/api/middleware"
	"github.com/Satyaamm/plowered/internal/server"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
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
	cfg, err := server.LoadConfig()
	if err != nil {
		return err
	}
	authCfg, err := middleware.LoadAuthConfigFromEnv()
	if err != nil {
		return err
	}

	store, err := buildStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer store.Close()

	return server.Run(ctx, cfg, server.Deps{
		Logger: logger,
		Store:  store,
		Auth:   authCfg,
	})
}

// buildStore picks a Store impl based on configuration. v0 wires the
// in-memory store; the postgres adapter (on the storage branch) plugs in
// behind the same interface once both branches merge.
func buildStore(_ context.Context, cfg server.Config) (storage.Store, error) {
	if cfg.DatabaseURL != "" {
		// TODO: when postgres adapter merges, return postgres.New(...) here.
		slog.Warn("PLOWERED_DATABASE_URL set but postgres adapter not yet wired on this branch; falling back to memory store")
	}
	return memory.New(), nil
}
