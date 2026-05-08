package transform_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/connectors/transform"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

func ctxWith(t string) context.Context {
	return storage.WithTenant(context.Background(), t)
}

func TestInfo(t *testing.T) {
	c := &transform.Connector{}
	if c.Info().Name != transform.ConnectorName {
		t.Errorf("name = %q", c.Info().Name)
	}
	if !c.Info().SupportsLineage {
		t.Error("expected SupportsLineage")
	}
}

func TestRegistration(t *testing.T) {
	if _, err := shared.Default.Build(transform.ConnectorName); err != nil {
		t.Fatalf("registry: %v", err)
	}
}

func TestValidateRequiresManifestPath(t *testing.T) {
	c := &transform.Connector{}
	if err := c.Validate(context.Background(), shared.Config{}); err == nil {
		t.Error("expected error when manifest_path missing")
	}
}

func TestCrawlEmitsAssetsAndEdges(t *testing.T) {
	store := memory.New()
	ctx := ctxWith("acme")
	sink := shared.NewBatchedSink(store, 50)

	c := &transform.Connector{}
	if err := c.Crawl(ctx, shared.Config{
		Values: map[string]any{
			"manifest_path": filepath.Join("testdata", "sample_manifest.json"),
		},
	}, sink); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	stats := sink.Stats()
	// 4 assets (2 sources + 2 models; test skipped by default)
	if stats.Assets != 4 {
		t.Errorf("assets = %d, want 4", stats.Assets)
	}
	// 3 lineage edges:
	//   raw.orders → daily_orders
	//   raw.customers → customer_summary
	//   daily_orders → customer_summary
	if stats.Edges != 3 {
		t.Errorf("edges = %d, want 3", stats.Edges)
	}
	if stats.UnresolvedQN != 0 {
		t.Errorf("unresolved = %d, want 0", stats.UnresolvedQN)
	}

	got, err := store.GetAssetByQualifiedName(ctx, "transform://demo/prod/mart/daily_orders")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Description != "Daily aggregated orders." {
		t.Errorf("description = %q", got.Description)
	}
	if got.Properties["resource_type"] != "model" {
		t.Errorf("resource_type = %v", got.Properties["resource_type"])
	}
	if got.Properties["raw_sql_hash"] == "" {
		t.Errorf("raw_sql_hash not set")
	}
}

func TestCrawlIncludeTests(t *testing.T) {
	store := memory.New()
	ctx := ctxWith("acme")
	sink := shared.NewBatchedSink(store, 50)

	c := &transform.Connector{}
	if err := c.Crawl(ctx, shared.Config{
		Values: map[string]any{
			"manifest_path": filepath.Join("testdata", "sample_manifest.json"),
			"include_tests": true,
		},
	}, sink); err != nil {
		t.Fatal(err)
	}

	if sink.Stats().Assets != 5 {
		t.Errorf("assets with tests = %d, want 5", sink.Stats().Assets)
	}

	test, err := store.GetAssetByQualifiedName(ctx, "transform://demo/prod/mart/unique_customer_id")
	if err != nil {
		t.Fatalf("test lookup: %v", err)
	}
	if test.Type != graph.AssetTypeGlossaryTerm {
		t.Errorf("test asset type = %s, want glossary_term", test.Type)
	}
}

func TestCrawlAppliesRunResults(t *testing.T) {
	store := memory.New()
	ctx := ctxWith("acme")
	sink := shared.NewBatchedSink(store, 50)

	c := &transform.Connector{}
	if err := c.Crawl(ctx, shared.Config{
		Values: map[string]any{
			"manifest_path":    filepath.Join("testdata", "sample_manifest.json"),
			"run_results_path": filepath.Join("testdata", "sample_run_results.json"),
		},
	}, sink); err != nil {
		t.Fatal(err)
	}

	good, _ := store.GetAssetByQualifiedName(ctx, "transform://demo/prod/mart/daily_orders")
	if good.Properties["last_run_status"] != "success" {
		t.Errorf("daily_orders status = %v, want success", good.Properties["last_run_status"])
	}

	bad, _ := store.GetAssetByQualifiedName(ctx, "transform://demo/prod/mart/customer_summary")
	if bad.Properties["last_run_status"] != "error" {
		t.Errorf("customer_summary status = %v, want error", bad.Properties["last_run_status"])
	}
	if bad.Properties["last_run_message"] != "compilation error in SQL" {
		t.Errorf("error message not propagated: %v", bad.Properties["last_run_message"])
	}
}

func TestParseManifestRejectsEmpty(t *testing.T) {
	if _, err := transform.ParseManifest(strings.NewReader(`{"nodes": {}}`)); err == nil {
		t.Error("expected error on empty nodes")
	}
}

func TestParseManifestRejectsMalformed(t *testing.T) {
	if _, err := transform.ParseManifest(strings.NewReader(`{not json`)); err == nil {
		t.Error("expected decode error")
	}
}
