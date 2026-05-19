package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/profile"
	"github.com/Satyaamm/plowered/internal/core/warehouse"
)

// ErrModeUnimplemented is returned when RunPlan is asked to execute a
// mode (incremental, cdc) that's designed but not yet wired. Surfaces
// to the UI as a clear "not available yet" rather than a panic.
var ErrModeUnimplemented = errors.New("migration: mode not implemented in this build")

// ConnectionReader is the smallest surface we need from the connection
// store for dialect selection.
type ConnectionReader interface {
	Get(ctx context.Context, tenantID, connectionID string) (*connection.Connection, error)
}

// Service runs migration plans. Plan CRUD is delegated to the Store
// (one less responsibility on this struct). The Service is the only
// type the HTTP layer should hold.
type Service struct {
	Store     Store
	Warehouse *warehouse.MultiFactory
	Conns     ConnectionReader
	Logger    *slog.Logger

	// BatchSize controls how many rows we INSERT per dest round-trip.
	// 500 is a comfortable default — small enough that one bad batch
	// is cheap to retry, large enough to amortise round-trip latency.
	BatchSize int

	// MaxRows caps the total rows a single Run will move. 0 = no cap;
	// set this in dev / preview to prevent a multi-billion-row source
	// from running away.
	MaxRows int64

	// RunTimeout caps the wall-clock for one Run. Defaults to 30
	// minutes; anything longer should probably be incremental, not
	// snapshot.
	RunTimeout time.Duration
}

// ---- Plan CRUD (thin pass-through; the Service owns Run semantics) --

func (s *Service) CreatePlan(ctx context.Context, p *Plan) (*Plan, error) {
	if p == nil {
		return nil, errors.New("migration: nil plan")
	}
	if p.Mode == "" {
		p.Mode = ModeSnapshot
	}
	if p.WriteMode == "" {
		p.WriteMode = WriteModeTruncate
	}
	if err := validatePlan(p); err != nil {
		return nil, err
	}
	return s.Store.CreatePlan(ctx, p)
}

func (s *Service) GetPlan(ctx context.Context, tenantID, planID string) (*Plan, error) {
	return s.Store.GetPlan(ctx, tenantID, planID)
}

func (s *Service) ListPlans(ctx context.Context, tenantID string) ([]*Plan, error) {
	return s.Store.ListPlans(ctx, tenantID)
}

func (s *Service) DeletePlan(ctx context.Context, tenantID, planID string) error {
	return s.Store.DeletePlan(ctx, tenantID, planID)
}

func (s *Service) ListRuns(ctx context.Context, tenantID, planID string) ([]*Run, error) {
	return s.Store.ListRuns(ctx, tenantID, planID)
}

// ---- Run (the actual data movement) ---------------------------------

// RunPlan kicks off one execution of the plan. Returns the persisted
// Run row with terminal status. The executor is synchronous; for
// long-running plans the HTTP layer is expected to wrap this in a
// jobs queue (designed; not wired this session).
//
// Failure semantics: the Run is recorded as Failed with the error
// string, but the function returns the error to the caller too — so
// the API can surface it inline AND the audit trail is complete.
func (s *Service) RunPlan(ctx context.Context, tenantID, planID string) (*Run, error) {
	if s.Store == nil || s.Warehouse == nil || s.Conns == nil {
		return nil, errors.New("migration: service not fully configured")
	}
	plan, err := s.Store.GetPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}
	if plan.Mode != ModeSnapshot {
		return nil, fmt.Errorf("%w: %s", ErrModeUnimplemented, plan.Mode)
	}

	timeout := s.RunTimeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	run, err := s.Store.StartRun(ctx, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("start run: %w", err)
	}

	rowsRead, rowsWritten, runErr := s.runSnapshot(runCtx, tenantID, plan)

	status := RunStatusSucceeded
	errStr := ""
	if runErr != nil {
		status = RunStatusFailed
		errStr = runErr.Error()
	}
	if finErr := s.Store.FinishRun(ctx, tenantID, run.ID, status, rowsRead, rowsWritten, errStr); finErr != nil {
		s.logger().WarnContext(ctx, "migration: finish run", "err", finErr)
	}

	run.Status = status
	run.RowsRead = rowsRead
	run.RowsWritten = rowsWritten
	if runErr != nil {
		run.Error = runErr.Error()
	}
	now := time.Now().UTC()
	run.FinishedAt = &now
	return run, runErr
}

// runSnapshot is the snapshot-mode body. Reads all of source, writes
// to dest in batches. Truncates dest first when WriteMode says so.
// Returns counters even on partial-failure so the operator sees how
// far we got.
func (s *Service) runSnapshot(ctx context.Context, tenantID string, plan *Plan) (int64, int64, error) {
	srcConn, err := s.Conns.Get(ctx, tenantID, plan.SourceConnectionID)
	if err != nil {
		return 0, 0, fmt.Errorf("load source conn: %w", err)
	}
	destConn, err := s.Conns.Get(ctx, tenantID, plan.DestConnectionID)
	if err != nil {
		return 0, 0, fmt.Errorf("load dest conn: %w", err)
	}
	if !srcConn.Type.IsSQL() || !destConn.Type.IsSQL() {
		return 0, 0, fmt.Errorf("migration: snapshot mode requires SQL source + dest (got src=%s dest=%s)",
			srcConn.Type, destConn.Type)
	}
	srcDialect, err := profile.PickDialect(srcConn.Type)
	if err != nil {
		return 0, 0, fmt.Errorf("source dialect: %w", err)
	}
	destDialect, err := profile.PickDialect(destConn.Type)
	if err != nil {
		return 0, 0, fmt.Errorf("dest dialect: %w", err)
	}

	srcExec, err := s.Warehouse.Open(ctx, tenantID, plan.SourceConnectionID)
	if err != nil {
		return 0, 0, fmt.Errorf("open source: %w", err)
	}
	destExec, err := s.Warehouse.Open(ctx, tenantID, plan.DestConnectionID)
	if err != nil {
		return 0, 0, fmt.Errorf("open dest: %w", err)
	}

	if plan.WriteMode == WriteModeTruncate {
		truncQ := buildTruncate(destDialect, plan.DestSchema, plan.DestTable)
		if _, err := destExec.Query(ctx, truncQ); err != nil {
			return 0, 0, fmt.Errorf("truncate dest: %w", err)
		}
	}

	srcCols, destCols := splitColumnMap(plan.ColumnMap)
	if len(srcCols) == 0 {
		return 0, 0, errors.New("migration: column_map is empty — no rows to move")
	}

	selectSQL := buildSelect(srcDialect, plan.SourceSchema, plan.SourceTable, srcCols, s.MaxRows)
	rows, err := srcExec.Query(ctx, selectSQL)
	if err != nil {
		return 0, 0, fmt.Errorf("select source: %w", err)
	}
	defer rows.Close()

	batchSize := s.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	batch := make([][]any, 0, batchSize)
	var rowsRead, rowsWritten int64

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		insertSQL := buildInsert(destDialect, plan.DestSchema, plan.DestTable, destCols, batch)
		if _, err := destExec.Query(ctx, insertSQL); err != nil {
			return fmt.Errorf("insert batch (size=%d, written=%d): %w", len(batch), rowsWritten, err)
		}
		rowsWritten += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		rowsRead++
		scanDest := make([]any, len(srcCols))
		scanPtrs := make([]any, len(srcCols))
		for i := range scanDest {
			scanPtrs[i] = &scanDest[i]
		}
		if err := rows.Scan(scanPtrs...); err != nil {
			return rowsRead, rowsWritten, fmt.Errorf("scan source row: %w", err)
		}
		batch = append(batch, scanDest)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return rowsRead, rowsWritten, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return rowsRead, rowsWritten, fmt.Errorf("source iterate: %w", err)
	}
	if err := flush(); err != nil {
		return rowsRead, rowsWritten, err
	}
	return rowsRead, rowsWritten, nil
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// ---- helpers ---------------------------------------------------------

func validatePlan(p *Plan) error {
	if p.TenantID == "" {
		return errors.New("plan: tenant_id required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("plan: name required")
	}
	if p.SourceConnectionID == "" || p.SourceTable == "" {
		return errors.New("plan: source connection + table required")
	}
	if p.DestConnectionID == "" || p.DestTable == "" {
		return errors.New("plan: dest connection + table required")
	}
	if len(p.ColumnMap) == 0 {
		return errors.New("plan: at least one column mapping required")
	}
	for _, c := range p.ColumnMap {
		if c.SourceCol == "" || c.DestCol == "" {
			return errors.New("plan: empty column name in mapping")
		}
	}
	if p.Mode != ModeSnapshot && p.Mode != ModeIncremental && p.Mode != ModeCDC {
		return fmt.Errorf("plan: unknown mode %q", p.Mode)
	}
	if p.WriteMode != WriteModeTruncate && p.WriteMode != WriteModeAppend {
		return fmt.Errorf("plan: unknown write_mode %q", p.WriteMode)
	}
	return nil
}

func splitColumnMap(cm []ColumnMap) (src, dest []string) {
	src = make([]string, len(cm))
	dest = make([]string, len(cm))
	for i, m := range cm {
		src[i] = m.SourceCol
		dest[i] = m.DestCol
	}
	return src, dest
}
