package relational_test

import (
	"context"
	"os"
	"testing"

	"github.com/Satyaamm/plowered/internal/connectors/relational"
	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

// Integration tests run when PLOWERED_TEST_DATABASE_URL is set.

func TestInfo(t *testing.T) {
	c := &relational.Connector{}
	info := c.Info()
	if info.Name != relational.ConnectorName {
		t.Errorf("name = %q", info.Name)
	}
	if !info.SupportsLineage {
		t.Error("expected SupportsLineage = true")
	}
}

func TestValidateRequiresDSN(t *testing.T) {
	c := &relational.Connector{}
	if err := c.Validate(context.Background(), shared.Config{}); err == nil {
		t.Error("expected error when dsn missing")
	}
}

func TestCrawlAgainstLiveDB(t *testing.T) {
	dsn := os.Getenv("PLOWERED_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PLOWERED_TEST_DATABASE_URL not set")
	}
	store := memory.New()
	ctx := storage.WithTenant(context.Background(), "test-rel")

	sink := shared.NewBatchedSink(store, 50)
	c := &relational.Connector{}
	if err := c.Crawl(ctx, shared.Config{
		Values: map[string]any{"dsn": dsn, "with_lineage": false},
	}, sink); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	stats := sink.Stats()
	if stats.Assets == 0 {
		t.Errorf("expected some assets, got 0")
	}

	// Sanity: at least one schema asset should be present.
	got, _, err := store.ListAssets(ctx, storage.ListAssetsOptions{
		Type: graph.AssetTypeSchema, PageSize: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Errorf("expected at least one schema asset")
	}
}
