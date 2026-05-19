package migration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/events"
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
	Store      Store
	Warehouse  *warehouse.MultiFactory
	Conns      ConnectionReader
	Checkpoint CheckpointStore // optional; required for ModeIncremental
	Events     events.Bus      // optional; published lifecycle events drive notifications
	Logger     *slog.Logger

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

// EnqueueRun validates the plan, creates a `status=running` Run row,
// and publishes a RunStarted event. The HTTP layer calls this on POST
// .../run so the UI sees an in-flight row immediately, then hands the
// run ID off to the async worker via the Enqueuer. The actual data
// movement happens in ExecuteRun on the worker side.
func (s *Service) EnqueueRun(ctx context.Context, tenantID, planID string) (*Run, error) {
	if s.Store == nil || s.Warehouse == nil || s.Conns == nil {
		return nil, errors.New("migration: service not fully configured")
	}
	plan, err := s.Store.GetPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}
	if err := preflight(plan, s.Checkpoint); err != nil {
		return nil, err
	}
	run, err := s.Store.StartRun(ctx, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("start run: %w", err)
	}
	s.publish(ctx, events.Event{
		ID:           newEventID(),
		Type:         events.RunStarted,
		Severity:     events.SeverityInfo,
		TenantID:     tenantID,
		ResourceType: "migration_run",
		ResourceID:   run.ID,
		Attributes: map[string]any{
			"plan_id":   plan.ID,
			"plan_name": plan.Name,
			"mode":      string(plan.Mode),
		},
		OccurredAt: time.Now().UTC(),
	})
	return run, nil
}

// ExecuteRun is the worker-side body: load the persisted run + plan,
// dispatch by mode, persist counters + terminal status, publish a
// RunSucceeded or RunFailed event. Errors are returned to the queue
// for telemetry but the run row is always finalised before we return.
func (s *Service) ExecuteRun(ctx context.Context, tenantID, runID string) error {
	if s.Store == nil || s.Warehouse == nil || s.Conns == nil {
		return errors.New("migration: service not fully configured")
	}
	run, err := s.Store.GetRun(ctx, tenantID, runID)
	if err != nil {
		return fmt.Errorf("load run: %w", err)
	}
	plan, err := s.Store.GetPlan(ctx, tenantID, run.PlanID)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	if err := preflight(plan, s.Checkpoint); err != nil {
		_ = s.Store.FinishRun(ctx, tenantID, runID, RunStatusFailed, 0, 0, err.Error())
		s.publishMigrationFinished(ctx, plan, run, 0, 0, err)
		return err
	}

	timeout := s.RunTimeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var rowsRead, rowsWritten int64
	var runErr error
	switch plan.Mode {
	case ModeSnapshot:
		rowsRead, rowsWritten, runErr = s.runSnapshot(runCtx, tenantID, plan)
	case ModeIncremental:
		rowsRead, rowsWritten, runErr = s.runIncremental(runCtx, tenantID, plan)
	}

	status := RunStatusSucceeded
	errStr := ""
	if runErr != nil {
		status = RunStatusFailed
		errStr = runErr.Error()
	}
	if finErr := s.Store.FinishRun(ctx, tenantID, run.ID, status, rowsRead, rowsWritten, errStr); finErr != nil {
		s.logger().WarnContext(ctx, "migration: finish run", "err", finErr)
	}
	s.publishMigrationFinished(ctx, plan, run, rowsRead, rowsWritten, runErr)
	return runErr
}

// RunPlan is the sync convenience that combines EnqueueRun + ExecuteRun.
// Used by tests and the in-memory dev path (where the worker is just
// the same goroutine). Production wires the two halves separately
// through the Asynq queue.
func (s *Service) RunPlan(ctx context.Context, tenantID, planID string) (*Run, error) {
	run, err := s.EnqueueRun(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	execErr := s.ExecuteRun(ctx, tenantID, run.ID)
	// Reload the terminal row so the caller sees final counters + status.
	final, getErr := s.Store.GetRun(ctx, tenantID, run.ID)
	if getErr != nil {
		// Fall back to the in-flight row + error string so the HTTP
		// caller still has something to render.
		if execErr != nil {
			run.Status = RunStatusFailed
			run.Error = execErr.Error()
		}
		return run, execErr
	}
	return final, execErr
}

// preflight validates the plan is runnable in this mode + that the
// dependencies the mode requires are configured.
func preflight(plan *Plan, cp CheckpointStore) error {
	switch plan.Mode {
	case ModeSnapshot:
		return nil
	case ModeIncremental:
		if plan.CursorColumn == "" {
			return errors.New("migration: incremental mode requires cursor_column")
		}
		if cp == nil {
			return errors.New("migration: incremental mode requires a checkpoint store (configure object storage)")
		}
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrModeUnimplemented, plan.Mode)
	}
}

func (s *Service) publish(ctx context.Context, e events.Event) {
	if s.Events == nil {
		return
	}
	s.Events.Publish(ctx, e)
}

func (s *Service) publishMigrationFinished(ctx context.Context, plan *Plan, run *Run, rowsRead, rowsWritten int64, runErr error) {
	if s.Events == nil {
		return
	}
	eventType := events.RunSucceeded
	severity := events.SeverityInfo
	if runErr != nil {
		eventType = events.RunFailed
		severity = events.SeverityError
	}
	attrs := map[string]any{
		"plan_id":      plan.ID,
		"plan_name":    plan.Name,
		"mode":         string(plan.Mode),
		"rows_read":    rowsRead,
		"rows_written": rowsWritten,
	}
	if runErr != nil {
		attrs["error"] = runErr.Error()
	}
	s.Events.Publish(ctx, events.Event{
		ID:           newEventID(),
		Type:         eventType,
		Severity:     severity,
		TenantID:     run.TenantID,
		ResourceType: "migration_run",
		ResourceID:   run.ID,
		Attributes:   attrs,
		OccurredAt:   time.Now().UTC(),
	})
}

func newEventID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "evt-fallback"
	}
	return hex.EncodeToString(b[:])
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

// runIncremental moves rows newer than the persisted checkpoint. On
// first run (no checkpoint) it copies everything; on subsequent runs
// it picks up from where the previous successful flush left off.
//
// Strategy:
//
//  1. Load checkpoint from object storage (nil ⇒ start from beginning).
//  2. Loop:
//     - Read source: SELECT cols FROM src WHERE cursor > $last
//       ORDER BY cursor ASC LIMIT batch
//     - INSERT batch into dest (append-only — TRUNCATE doesn't make
//       sense for incremental).
//     - Persist new checkpoint (= max cursor seen in this batch).
//     - Repeat until LIMIT batch returns fewer rows (end of stream).
//
// Failure recovery: if the run dies after step 2's flush but before
// the step 2 checkpoint, the next run replays the same batch. That's
// a known duplicate-row risk we accept in v0 (idempotent upsert
// semantics are a follow-up). The dest table should treat
// "(primary_key, cursor)" as the dedupe key if it cares.
func (s *Service) runIncremental(ctx context.Context, tenantID string, plan *Plan) (int64, int64, error) {
	srcConn, err := s.Conns.Get(ctx, tenantID, plan.SourceConnectionID)
	if err != nil {
		return 0, 0, fmt.Errorf("load source conn: %w", err)
	}
	destConn, err := s.Conns.Get(ctx, tenantID, plan.DestConnectionID)
	if err != nil {
		return 0, 0, fmt.Errorf("load dest conn: %w", err)
	}
	if !srcConn.Type.IsSQL() || !destConn.Type.IsSQL() {
		return 0, 0, fmt.Errorf("migration: incremental mode requires SQL source + dest")
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

	srcCols, destCols := splitColumnMap(plan.ColumnMap)
	if len(srcCols) == 0 {
		return 0, 0, errors.New("migration: column_map is empty")
	}
	// Make sure the cursor column is one of the columns the source
	// SELECT returns, so we can extract the next checkpoint from each
	// row without a separate query.
	cursorIdx := indexOf(srcCols, plan.CursorColumn)
	if cursorIdx < 0 {
		// Auto-add cursor column to the select if not present.
		srcCols = append(srcCols, plan.CursorColumn)
		cursorIdx = len(srcCols) - 1
	}

	cp, err := s.Checkpoint.Load(ctx, plan.ID)
	if err != nil {
		return 0, 0, fmt.Errorf("load checkpoint: %w", err)
	}
	lastCursor := ""
	if cp != nil {
		lastCursor = cp.LastCursorValue
	}

	batchSize := s.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	var rowsRead, rowsWritten int64
	for {
		if err := ctx.Err(); err != nil {
			return rowsRead, rowsWritten, err
		}
		selectSQL := buildIncrementalSelect(srcDialect, plan.SourceSchema, plan.SourceTable,
			srcCols, plan.CursorColumn, lastCursor, batchSize)
		rows, err := srcExec.Query(ctx, selectSQL)
		if err != nil {
			return rowsRead, rowsWritten, fmt.Errorf("select batch: %w", err)
		}

		batch := make([][]any, 0, batchSize)
		maxCursorInBatch := lastCursor
		for rows.Next() {
			rowsRead++
			scanDest := make([]any, len(srcCols))
			scanPtrs := make([]any, len(srcCols))
			for i := range scanDest {
				scanPtrs[i] = &scanDest[i]
			}
			if err := rows.Scan(scanPtrs...); err != nil {
				rows.Close()
				return rowsRead, rowsWritten, fmt.Errorf("scan: %w", err)
			}
			if c := fmt.Sprint(scanDest[cursorIdx]); c > maxCursorInBatch {
				maxCursorInBatch = c
			}
			batch = append(batch, scanDest)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return rowsRead, rowsWritten, fmt.Errorf("iterate: %w", err)
		}
		rows.Close()

		if len(batch) == 0 {
			// End of stream — checkpoint stays at its current value.
			return rowsRead, rowsWritten, nil
		}

		// Build the dest INSERT using the destCols (without cursor if
		// it was auto-added). If cursor was auto-added, drop the last
		// column from each row before insert so dest schema matches.
		toWrite := batch
		writeCols := destCols
		if cursorIdx == len(srcCols)-1 && len(destCols) == len(srcCols)-1 {
			toWrite = stripLastColumn(batch)
		}
		insertSQL := buildInsert(destDialect, plan.DestSchema, plan.DestTable, writeCols, toWrite)
		if _, err := destExec.Query(ctx, insertSQL); err != nil {
			return rowsRead, rowsWritten, fmt.Errorf("insert batch (written=%d): %w", rowsWritten, err)
		}
		rowsWritten += int64(len(batch))

		// Persist checkpoint AFTER successful dest write. If save
		// itself fails we keep going — the next batch will overwrite
		// it. Failures here only matter on process death between save
		// attempts.
		lastCursor = maxCursorInBatch
		next := &Checkpoint{
			LastCursorValue: lastCursor,
			RowsProcessed:   rowsRead,
			UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.Checkpoint.Save(ctx, plan.ID, next); err != nil {
			s.logger().WarnContext(ctx, "migration: checkpoint save", "err", err)
		}
		if s.MaxRows > 0 && rowsRead >= s.MaxRows {
			return rowsRead, rowsWritten, nil
		}
		if int64(len(batch)) < int64(batchSize) {
			// Less than a full batch ⇒ caught up.
			return rowsRead, rowsWritten, nil
		}
	}
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
	if p.Mode == ModeIncremental {
		if p.CursorColumn == "" {
			return errors.New("plan: incremental mode requires cursor_column")
		}
		if p.WriteMode == WriteModeTruncate {
			return errors.New("plan: incremental mode cannot use truncate_and_replace (incremental implies append)")
		}
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

// indexOf returns the position of needle in cols, case-insensitive,
// or -1 if absent.
func indexOf(cols []string, needle string) int {
	needle = strings.ToLower(needle)
	for i, c := range cols {
		if strings.ToLower(c) == needle {
			return i
		}
	}
	return -1
}

// stripLastColumn returns each row with its last element removed.
// Used when the runner auto-appends the cursor column to source SELECT
// but the dest table doesn't carry it.
func stripLastColumn(rows [][]any) [][]any {
	out := make([][]any, len(rows))
	for i, r := range rows {
		if len(r) == 0 {
			out[i] = r
			continue
		}
		out[i] = r[:len(r)-1]
	}
	return out
}
