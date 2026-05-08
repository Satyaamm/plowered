// Command ploweredctl is the operator CLI: migrations, debug, admin.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/config"
	"github.com/Satyaamm/plowered/internal/storage/postgres"
)

func main() {
	_ = config.LoadDefault()
	if err := run(os.Args[1:]); err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("ploweredctl", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		usage()
		return fmt.Errorf("missing command")
	}
	cmd, rest := args[0], args[1:]

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch cmd {
	case "version":
		fmt.Println("ploweredctl dev")
		return nil
	case "migrate":
		return runMigrate(ctx, rest)
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func runMigrate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dbURL := fs.String("db",
		os.Getenv("PLOWERED_DATABASE_URL"),
		"PostgreSQL connection string (or set PLOWERED_DATABASE_URL)",
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	sub := fs.Arg(0)
	if sub == "" {
		sub = "up"
	}
	if sub != "up" {
		return fmt.Errorf("only `migrate up` supported in v0; got %q", sub)
	}
	if *dbURL == "" {
		return fmt.Errorf("--db or PLOWERED_DATABASE_URL is required")
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(connectCtx, *dbURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	fmt.Println("migrations applied")
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: ploweredctl <command>

Commands:
  migrate up   Apply pending DB migrations (uses --db or PLOWERED_DATABASE_URL)
  version      Print build version
  help         Show this message`)
}
