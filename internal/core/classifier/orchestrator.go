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
//
// Sampler is an interface (see sampler.go) — in production it's a
// MultiSampler that routes by connection type, but tests substitute a
// stub that returns canned Results.
type Orchestrator struct {
	Catalog CatalogReader
	Sampler Sampler
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

// ListConnectionScope returns every table the catalog knows about for
// this connection. The wizard uses this to populate Schema + Table
// multi-select dropdowns so the operator picks from real names instead
// of typing them blind. The orchestrator is the right home for this:
// it already depends on the same CatalogReader used during sampling,
// so the dropdown is guaranteed to match what classify will actually
// scan.
func (o *Orchestrator) ListConnectionScope(
	ctx context.Context,
	tenantID, connectionID string,
) ([]TableRef, error) {
	if o.Catalog == nil {
		return nil, fmt.Errorf("classifier: orchestrator not fully configured")
	}
	return o.Catalog.TablesForConnection(ctx, tenantID, connectionID)
}

// PreviewOptions filters which tables PreviewConnection scans. An empty
// slice matches everything; otherwise the filter is case-insensitive
// includes-only. Future fields (sample size, detector filter) live here
// so callers don't grow the method signature.
type PreviewOptions struct {
	Schemas []string
	Tables  []string
}

// Proposal is the read-only output of a preview run. Nothing has been
// written; the UI iterates Tables and lets the operator accept/reject
// per-column before calling ApplyDecisions.
type Proposal struct {
	Tables  []ProposalTable
	Skipped []ProposalSkip
}

// ProposalTable carries the table identity plus its per-column verdicts.
type ProposalTable struct {
	AssetID string
	Schema  string
	Name    string
	Columns []ProposalColumn
}

// ProposalColumn is the per-column read-out the wizard renders: how
// many values were sampled, per-detector hit counts (so the UI can
// show "195/200 matched email"), and the tags the classifier would
// apply if the user accepts as-is.
type ProposalColumn struct {
	AssetID      string
	Name         string
	Sampled      int
	Hits         map[string]int
	ProposedTags []string
}

// ProposalSkip records a table that couldn't be sampled (driver error,
// permission denied, unsupported warehouse) so the UI can surface it
// instead of silently dropping rows.
type ProposalSkip struct {
	Table  string
	Reason string
}

// PreviewConnection runs the sampler over the connection's catalog and
// returns the proposed tag set for every column. It performs NO writes.
// Filtering happens in Postgres (catalog reader) for tables and in this
// function for schemas / specific table names.
func (o *Orchestrator) PreviewConnection(
	ctx context.Context,
	tenantID, connectionID string,
	opts PreviewOptions,
) (*Proposal, error) {
	if o.Catalog == nil || o.Sampler == nil {
		return nil, fmt.Errorf("classifier: orchestrator not fully configured")
	}
	logger := o.logger()
	tables, err := o.Catalog.TablesForConnection(ctx, tenantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	schemaSet := lowerSet(opts.Schemas)
	tableSet := lowerSet(opts.Tables)

	proposal := &Proposal{}
	for _, t := range tables {
		if len(schemaSet) > 0 && !schemaSet[strings.ToLower(t.Schema)] {
			continue
		}
		if len(tableSet) > 0 && !tableSet[strings.ToLower(t.Name)] {
			continue
		}
		cols, err := o.Catalog.ColumnsForTable(ctx, tenantID, t.AssetID)
		if err != nil {
			logger.WarnContext(ctx, "classifier: list columns", "table", t.Name, "err", err)
			proposal.Skipped = append(proposal.Skipped, ProposalSkip{
				Table:  qualify(t.Schema, t.Name),
				Reason: err.Error(),
			})
			continue
		}
		if len(cols) == 0 {
			continue
		}
		colNames := make([]string, len(cols))
		for i, c := range cols {
			colNames[i] = c.Name
		}
		results, err := o.Sampler.SampleTable(ctx, tenantID, connectionID, t.Schema, t.Name, colNames)
		if err != nil {
			logger.WarnContext(ctx, "classifier: sample failed", "table", t.Name, "err", err)
			proposal.Skipped = append(proposal.Skipped, ProposalSkip{
				Table:  qualify(t.Schema, t.Name),
				Reason: err.Error(),
			})
			continue
		}
		byName := map[string]Result{}
		for _, r := range results {
			byName[strings.ToLower(r.Column)] = r
		}
		pt := ProposalTable{AssetID: t.AssetID, Schema: t.Schema, Name: t.Name}
		for _, col := range cols {
			r := byName[strings.ToLower(col.Name)]
			pt.Columns = append(pt.Columns, ProposalColumn{
				AssetID:      col.AssetID,
				Name:         col.Name,
				Sampled:      r.Sampled,
				Hits:         r.Hits,
				ProposedTags: r.Tags,
			})
		}
		proposal.Tables = append(proposal.Tables, pt)
	}
	return proposal, nil
}

// Decision is one row of the apply payload: the column's catalog UUID
// and the exact tag set to write. Tags not present here are not
// applied, even if the preview proposed them.
type Decision struct {
	ColumnAssetID string
	Tags          []string
}

// ApplyResult counts what ApplyDecisions wrote. Applied counts the
// number of (column, tag) classification rows touched.
type ApplyResult struct {
	Applied        int
	ColumnsUpdated int
}

// ApplyDecisions persists the user-approved tags. Each Decision merges
// into data_classifications + assets.tags. Decisions with an empty Tags
// slice are skipped — the v0 API doesn't support unsetting tags here
// (use the asset tag-management endpoints for that).
func (o *Orchestrator) ApplyDecisions(
	ctx context.Context,
	tenantID, appliedBy string,
	decisions []Decision,
) (*ApplyResult, error) {
	if o.Sink == nil {
		return nil, fmt.Errorf("classifier: orchestrator not fully configured")
	}
	logger := o.logger()
	out := &ApplyResult{}
	for _, d := range decisions {
		if d.ColumnAssetID == "" || len(d.Tags) == 0 {
			continue
		}
		out.ColumnsUpdated++
		for _, tag := range d.Tags {
			if err := o.Sink.Apply(ctx, tenantID, d.ColumnAssetID, tag, appliedBy); err != nil {
				logger.WarnContext(ctx, "classifier: apply", "asset_id", d.ColumnAssetID, "tag", tag, "err", err)
				continue
			}
			out.Applied++
		}
		if err := o.Sink.MergeAssetTags(ctx, tenantID, d.ColumnAssetID, d.Tags); err != nil {
			logger.WarnContext(ctx, "classifier: merge tags", "asset_id", d.ColumnAssetID, "err", err)
		}
	}
	return out, nil
}

func lowerSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]bool, len(items))
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out[strings.ToLower(s)] = true
	}
	return out
}

func qualify(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}
