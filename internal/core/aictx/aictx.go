// Package aictx assembles LLM-ready context blocks for catalog assets.
// Both the Describer (auto-write a description for one asset) and the
// Asker (answer an NL question against a connection) need the same
// raw material: table name, column list with types, sample values,
// tags, and glossary terms. Centralising the assembly here means the
// two services produce prompts with consistent shape — useful for
// debugging, evals, and prompt versioning.
//
// Design notes:
//
//   - The Builder is dependency-injected. Storage, profile cache, and
//     glossary lookup are all behind small interfaces — this package
//     has zero direct knowledge of Postgres.
//   - Output is *plain text*, not structured JSON. LLMs handle text
//     reliably; JSON output is fine for tool-using agents but our use
//     cases want the model to write prose.
//   - Token budget: every section is bounded (max columns, max sample
//     values, max sibling tables). The full block targets ~2000
//     tokens which fits even modest context windows.
package aictx

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/core/profile"
)

// Limits caps each section of the rendered context. Defaults are
// tuned to land under ~2000 prompt tokens on a 50-column table.
type Limits struct {
	MaxColumns        int // 0 → 50
	MaxSampleValuesPerColumn int // 0 → 5
	MaxSiblingTables  int // 0 → 0 (disabled — sibling lookup not wired yet)
}

func (l Limits) maxColumns() int {
	if l.MaxColumns <= 0 {
		return 50
	}
	return l.MaxColumns
}
func (l Limits) maxSamples() int {
	if l.MaxSampleValuesPerColumn <= 0 {
		return 5
	}
	return l.MaxSampleValuesPerColumn
}

// AssetReader fetches an asset by ID. Implementations are typically the
// storage.Store from this codebase (a Postgres pool query). The
// interface is intentionally narrow so tests can stub it.
type AssetReader interface {
	GetAsset(ctx context.Context, tenantID, assetID string) (*graph.Asset, error)
}

// TableSchemaReader returns the column list + warehouse coordinates
// for a table asset. profile.AssetReader already provides exactly
// this — Postgres impl satisfies both interfaces.
type TableSchemaReader interface {
	ReadTable(ctx context.Context, tenantID, tableAssetID string) (*profile.TableInfo, error)
}

// ProfileLookup is optional — when present, sample top-values get
// inlined into the context. Falls back to "no samples available" when
// profile hasn't run yet.
type ProfileLookup interface {
	Get(ctx context.Context, tenantID, tableAssetID string) (*profile.Report, error)
}

// Builder is the single entry point. Construct once with the
// dependencies wired in, call BuildForTable / BuildForColumn from the
// feature services.
type Builder struct {
	Assets   AssetReader
	Tables   TableSchemaReader
	Profiles ProfileLookup // optional
	Limits   Limits
}

// TableContext is the structured intermediate result. Callers usually
// render it via Render() but it's exposed so tests / debug endpoints
// can inspect the assembled facts.
type TableContext struct {
	AssetID      string
	Schema       string
	Table        string
	TableTags    []string
	Description  string
	Columns      []ColumnContext
}

// ColumnContext is one entry in the table's column list. SampleValues
// is the top-N from the profile cache when available.
type ColumnContext struct {
	Name         string
	DataType     string
	Tags         []string
	NullPct      *float64 // null fraction 0..1 if profile available
	SampleValues []string
}

// BuildForTable assembles full context for a table asset. Failures in
// any optional source (profile, glossary) degrade gracefully — the
// context is still produced with whatever facts were available. Only
// the asset lookup itself is hard-required.
func (b *Builder) BuildForTable(ctx context.Context, tenantID, tableAssetID string) (*TableContext, error) {
	if b.Assets == nil || b.Tables == nil {
		return nil, fmt.Errorf("aictx: builder not fully configured")
	}
	asset, err := b.Assets.GetAsset(ctx, tenantID, tableAssetID)
	if err != nil {
		return nil, fmt.Errorf("read asset: %w", err)
	}
	info, err := b.Tables.ReadTable(ctx, tenantID, tableAssetID)
	if err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}

	out := &TableContext{
		AssetID:     tableAssetID,
		Schema:      info.Schema,
		Table:       info.Table,
		TableTags:   asset.Tags,
		Description: asset.Description,
	}

	maxCols := b.Limits.maxColumns()
	cols := info.Columns
	if len(cols) > maxCols {
		cols = cols[:maxCols]
	}
	// Sample values pull from the profile cache when one exists. We
	// never block on a fresh profile run — if it's missing, the column
	// just ships without samples.
	var profileByName map[string]profile.Column
	if b.Profiles != nil {
		if rep, perr := b.Profiles.Get(ctx, tenantID, tableAssetID); perr == nil && rep != nil {
			profileByName = make(map[string]profile.Column, len(rep.Columns))
			for _, c := range rep.Columns {
				profileByName[strings.ToLower(c.Name)] = c
			}
		}
	}
	maxSamples := b.Limits.maxSamples()
	for _, c := range cols {
		cc := ColumnContext{Name: c.Name, DataType: c.DataType}
		if p, ok := profileByName[strings.ToLower(c.Name)]; ok {
			if p.RowsSampled > 0 {
				pct := float64(p.NullCount) / float64(p.RowsSampled)
				cc.NullPct = &pct
			}
			for i, tv := range p.TopValues {
				if i >= maxSamples {
					break
				}
				cc.SampleValues = append(cc.SampleValues, tv.Value)
			}
		}
		out.Columns = append(out.Columns, cc)
	}
	return out, nil
}

// BuildForColumn is a convenience for the Describer when the user
// asked to describe a column rather than a table. It returns the
// parent table context with the target column emphasised. The
// underlying asset row gives us the column's tags + parent table id.
//
// Convention in this codebase: a column asset's parent table id lives
// in asset.Properties["parent_id"]. If absent we degrade to the
// column-only context.
func (b *Builder) BuildForColumn(ctx context.Context, tenantID, columnAssetID string) (*TableContext, error) {
	asset, err := b.Assets.GetAsset(ctx, tenantID, columnAssetID)
	if err != nil {
		return nil, fmt.Errorf("read column asset: %w", err)
	}
	parentID, _ := asset.Properties["parent_id"].(string)
	if parentID == "" {
		// Standalone column context — no parent visible.
		return &TableContext{
			AssetID:     columnAssetID,
			Description: asset.Description,
			TableTags:   asset.Tags,
			Columns: []ColumnContext{
				{
					Name:     asset.Name,
					DataType: stringProp(asset.Properties, "data_type"),
					Tags:     asset.Tags,
				},
			},
		}, nil
	}
	return b.BuildForTable(ctx, tenantID, parentID)
}

// Render formats a TableContext as a plain-text block suitable for
// LLM prompts. Output is deterministic — same input always produces
// the same string — so prompt diffs are reviewable.
func (tc *TableContext) Render() string {
	var sb strings.Builder
	tableName := tc.Table
	if tc.Schema != "" {
		tableName = tc.Schema + "." + tc.Table
	}
	fmt.Fprintf(&sb, "Table: %s\n", tableName)
	if tc.Description != "" {
		fmt.Fprintf(&sb, "Existing description: %s\n", tc.Description)
	}
	if len(tc.TableTags) > 0 {
		sort.Strings(tc.TableTags)
		fmt.Fprintf(&sb, "Tags: %s\n", strings.Join(tc.TableTags, ", "))
	}
	if len(tc.Columns) == 0 {
		return sb.String()
	}
	fmt.Fprint(&sb, "Columns:\n")
	for _, c := range tc.Columns {
		fmt.Fprintf(&sb, "  - %s (%s)", c.Name, c.DataType)
		if c.NullPct != nil {
			fmt.Fprintf(&sb, "  nulls=%.1f%%", *c.NullPct*100)
		}
		if len(c.Tags) > 0 {
			fmt.Fprintf(&sb, "  tags=%s", strings.Join(c.Tags, ","))
		}
		if len(c.SampleValues) > 0 {
			fmt.Fprintf(&sb, "  samples=%s", strings.Join(quoteAll(c.SampleValues), ", "))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func quoteAll(vs []string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		// Truncate very long sample values; LLM doesn't need 4KB blobs.
		if len(v) > 80 {
			v = v[:77] + "..."
		}
		out[i] = `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return out
}

func stringProp(props map[string]any, key string) string {
	if v, ok := props[key].(string); ok {
		return v
	}
	return ""
}
