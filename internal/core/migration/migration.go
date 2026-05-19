// Package migration moves data between connected sources. It's the
// "Data Migration" pillar from the platform's capability sheet, sitting
// on top of the warehouse.Executor abstraction so it works across
// every SQL warehouse Plowered supports.
//
// Design choices:
//
//   - Plan/Run separation. A Plan is a persistent declaration of
//     "move X to Y"; a Run is one execution attempt. Decoupling them
//     lets the same plan be re-run (with the same column mapping) and
//     gives us a history surface for free.
//   - Snapshot mode only this session. Incremental + CDC modes are
//     designed as enum values but not implemented yet — adding them
//     later is a new mode value + a different RunPlan path; nothing
//     else moves.
//   - Identity column mapping for v0. The Plan's ColumnMap accepts
//     {source_col → dest_col} pairs but the UI v0 just passes the
//     intersection of source + dest schemas. Transform expressions
//     ("CAST(col AS TEXT)" etc.) are designed but not exposed.
//   - Source SQL is generated, NOT supplied by the user. The platform
//     decides "SELECT cols FROM src.table". Letting the operator
//     write arbitrary SQL would re-create the Asker safety problem;
//     migration plans are a managed surface.
//
// The runtime depends only on warehouse.MultiFactory and the Store
// — no direct database access or driver imports here.
package migration

import "time"

// Mode names how a Plan moves data over time.
type Mode string

const (
	// ModeSnapshot copies the source table to dest once per Run.
	// Implemented in this session.
	ModeSnapshot Mode = "snapshot"
	// ModeIncremental copies rows newer than the last cursor value.
	// Designed; not implemented yet — RunPlan will return
	// ErrModeUnimplemented for this mode.
	ModeIncremental Mode = "incremental"
	// ModeCDC streams changes from the source's change-data-capture
	// surface. Designed; same status as incremental.
	ModeCDC Mode = "cdc"
)

// WriteMode controls how the destination handles existing data.
type WriteMode string

const (
	// WriteModeTruncate clears the destination table before writing.
	// Right default for snapshot replication.
	WriteModeTruncate WriteMode = "truncate_and_replace"
	// WriteModeAppend adds rows without clearing. Right for an
	// audit/event-log destination.
	WriteModeAppend WriteMode = "append"
)

// ColumnMap pairs a source column name to a destination column name.
// SourceCol == DestCol is the common case (identity mapping).
type ColumnMap struct {
	SourceCol string `json:"source_col"`
	DestCol   string `json:"dest_col"`
}

// Plan is the persistent declaration of a migration. Tenant-scoped.
type Plan struct {
	ID                 string      `json:"id"`
	TenantID           string      `json:"tenant_id"`
	Name               string      `json:"name"`
	SourceConnectionID string      `json:"source_connection_id"`
	SourceSchema       string      `json:"source_schema"`
	SourceTable        string      `json:"source_table"`
	DestConnectionID   string      `json:"dest_connection_id"`
	DestSchema         string      `json:"dest_schema"`
	DestTable          string      `json:"dest_table"`
	ColumnMap          []ColumnMap `json:"column_map"`
	Mode               Mode        `json:"mode"`
	WriteMode          WriteMode   `json:"write_mode"`

	// CursorColumn is the monotonic column (e.g. updated_at, id) the
	// runner sorts + paginates by in incremental mode. Required when
	// Mode == ModeIncremental. Ignored for snapshot mode.
	CursorColumn string `json:"cursor_column,omitempty"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RunStatus is the lifecycle of one execution attempt.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

// Run records one execution of a Plan. RowsRead / RowsWritten are
// snapshots taken at completion; for in-progress runs they're zero
// (the executor will update them on finish).
type Run struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	PlanID        string     `json:"plan_id"`
	Status        RunStatus  `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	RowsRead      int64      `json:"rows_read"`
	RowsWritten   int64      `json:"rows_written"`
	CheckpointURI string     `json:"checkpoint_uri,omitempty"`
	Error         string     `json:"error,omitempty"`
}
