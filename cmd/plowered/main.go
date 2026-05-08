// Command plowered runs the API server: gRPC + REST gateway + web UI mount.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error("plowered exited with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	slog.Info("plowered starting", "version", version())
	// TODO(M1): wire storage, gRPC server, REST gateway, search index, NATS.
	<-ctx.Done()
	slog.Info("plowered shutting down")
	return nil
}

func version() string {
	if v, ok := os.LookupEnv("PLOWERED_VERSION"); ok {
		return v
	}
	return fmt.Sprintf("dev")
}
