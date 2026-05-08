// Command plowered-mcp speaks Model Context Protocol over stdio so any
// MCP-compliant local agent can query a Plowered catalog.
//
// Usage from a local MCP client config:
//
//	{
//	  "command": "plowered-mcp",
//	  "args": [],
//	  "env": {
//	    "PLOWERED_TENANT_ID": "acme",
//	    "PLOWERED_DATABASE_URL": "postgres://..."
//	  }
//	}
//
// The binary loads a Store implementation, registers Plowered tools on the
// MCP server, and serves the protocol on stdin/stdout. Logs go to stderr so
// they do not corrupt the JSON-RPC stream on stdout.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	mcphandlers "github.com/Satyaamm/plowered/internal/api/mcp"
	"github.com/Satyaamm/plowered/internal/config"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
	"github.com/Satyaamm/plowered/pkg/mcp"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("plowered-mcp exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDefault(); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	tenant := os.Getenv("PLOWERED_TENANT_ID")
	if tenant == "" {
		return fmt.Errorf("PLOWERED_TENANT_ID is required")
	}
	ctx = storage.WithTenant(ctx, tenant)

	store, err := buildStore()
	if err != nil {
		return err
	}
	defer store.Close()

	server := mcp.NewServer(mcp.ServerInfo{
		Name:    "plowered-mcp",
		Version: getenvDefault("PLOWERED_VERSION", "dev"),
	})
	if err := mcphandlers.Register(server.Tools, store); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}

	transport := mcp.NewStdioTransport(os.Stdin, os.Stdout)
	slog.Info("plowered-mcp ready", "tenant", tenant, "tools", len(server.Tools.List()))
	return server.Serve(ctx, transport)
}

// buildStore returns a Store. v0 uses memory; the postgres adapter (on the
// storage branch) plugs in once both branches merge.
func buildStore() (storage.Store, error) {
	if os.Getenv("PLOWERED_DATABASE_URL") != "" {
		slog.Warn("PLOWERED_DATABASE_URL set but postgres adapter not on this branch; using memory store")
	}
	return memory.New(), nil
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
