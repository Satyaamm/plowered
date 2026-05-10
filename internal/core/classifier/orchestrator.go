package classifier

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// CatalogReader reads the existing catalog so the orchestrator knows
// which tables and columns exist for a given connection. The Postgres
// storage.Store implements this transparently — the orchestrator only
// uses the small slice it needs to drive sampling.
type CatalogReader interface {
	// TablesForConnection returns one record per table-asset that
	// belongs to the connection. Schema + Name are the database-level
	// identifiers used to build the SELECT; AssetID is the catalog UUID
	// keyed in data_classifications.
	TablesForConnection(ctx context.Context, tenantID, connectionID string) ([]TableRef, error)
	// ColumnsForTable returns one record per column belonging to the
	// table. AssetID is the column's UUID; the orchestrator stamps
	// classifications and merged tags against it.
	ColumnsForTable(ctx context.Context, tenantID, tableAssetID string) ([]ColumnRef, error)
}

// TableRef identifies a table the catalog knows about.
type TableRef struct {
	AssetID string
	Schema  string
	Name    string
}

// ColumnRef identifies one column in the catalog.
type ColumnRef struct {
	AssetID string
	Name    string
	// QualifiedName is filled by the orchestrator for diagnostic logs
	// only; storage callers don't have to populate it.
	QualifiedName string
}

// ClassificationSink persists detected `class:*` tags. Implementations
// usually write to:
//
//	data_classifications  — one row per (asset_id, classification)
//	assets.tags           — merge into the existing tag set so the UI
//	                        overview tab shows it automatically
type ClassificationSink interface {
	Apply(ctx context.Context, tenantID, assetID, classification, appliedBy string) error
	MergeAssetTags(ctx context.Context, tenantID, assetID string, tags []string) error
}

// Run is one classification job result. It's small on purpose — the
// orchestrator returns aggregate counts; per-column breakdowns sit on
// the asset detail surface (data_classifications rows).
type Run struct {
	Tables     int
	Columns    int
	Tagged     int
	Skipped    int
	DurationMs int64
}

// Orchestrator wires a CatalogReader, a Sampler, and a sink together.
// The transformation is straightforward: for every table the catalog
// knows about, sample N rows, classify each column, persist tags.
type Orchestrator struct {
	Catalog CatalogReader
	Sampler *Sampler
	Sink    ClassificationSink
	Logger  *slog.Logger
}

func (o *Orchestrator) ClassifyConnection(
	ctx context.Context,
	tenantID, connectionID, appliedBy string,
) (*Run, error) {
	if o.Catalog == nil || o.Sampler == nil || o.Sink == nil {
		return nil, fmt.Errorf("classifier: orchestrator not fully configured")
	}
	logger := o.logger()
	tables, err := o.Catalog.TablesForConnection(ctx, tenantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	run := &Run{Tables: len(tables)}
	for _, t := range tables {
		cols, err := o.Catalog.ColumnsForTable(ctx, tenantID, t.AssetID)
		if err != nil {
			logger.WarnContext(ctx, "classifier: list columns", "table", t.Name, "err", err)
			run.Skipped++
			continue
		}
		if len(cols) == 0 {
			run.Skipped++
			continue
		}
		colNames := make([]string, len(cols))
		for i, c := range cols {
			colNames[i] = c.Name
		}
		results, err := o.Sampler.SampleTable(ctx, tenantID, connectionID, t.Schema, t.Name, colNames)
		if err != nil {
			logger.WarnContext(ctx, "classifier: sample failed", "table", t.Name, "err", err)
			run.Skipped++
			continue
		}
		// Map column-name → Result so we can apply by AssetID.
		byName := map[string]Result{}
		for _, r := range results {
			byName[strings.ToLower(r.Column)] = r
		}
		for _, col := range cols {
			run.Columns++
			r, ok := byName[strings.ToLower(col.Name)]
			if !ok || len(r.Tags) == 0 {
				continue
			}
			run.Tagged++
			for _, tag := range r.Tags {
				if err := o.Sink.Apply(ctx, tenantID, col.AssetID, tag, appliedBy); err != nil {
					logger.WarnContext(ctx, "classifier: apply", "asset_id", col.AssetID, "tag", tag, "err", err)
				}
			}
			if err := o.Sink.MergeAssetTags(ctx, tenantID, col.AssetID, r.Tags); err != nil {
				logger.WarnContext(ctx, "classifier: merge tags", "asset_id", col.AssetID, "err", err)
			}
		}
	}
	return run, nil
}

func (o *Orchestrator) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}
