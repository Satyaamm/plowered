package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/warehouse"
)

// AssetReader is the minimal shape Service needs from the catalog
// store: given a table asset_id, return the connection ID + schema +
// table + ordered column list (with their data types). The Postgres
// store implements this through joins on assets + assets_columns —
// we don't reach into storage from here directly.
type AssetReader interface {
	ReadTable(ctx context.Context, tenantID, tableAssetID string) (*TableInfo, error)
}

// TableInfo is the catalog's view of a table at profiling time. The
// Service uses this to build the SQL and to map result columns back
// to catalog columns.
type TableInfo struct {
	ConnectionID string
	Schema       string
	Table        string
	Type         connection.Type // warehouse kind for dialect selection
	Columns      []ColumnSpec
}

// Cache stores profile reports keyed by table_asset_id. Implementations
// may use Postgres (the production path), in-memory (tests), or S3 +
// metadata row (future). The Service only knows the interface so a
// migration to S3-backed storage is one swap.
type Cache interface {
	Get(ctx context.Context, tenantID, tableAssetID string) (*Report, error)
	// Put persists a report. tenant_id is on the report row server-side
	// keyed by the table_asset_id which is itself tenant-scoped — the
	// store derives tenant from the asset, callers don't pass it.
	Put(ctx context.Context, report *Report) error
}

// ErrNotCached signals a cache miss. Service treats this as "compute
// fresh"; callers can pass it through if they want async semantics.
var ErrNotCached = errors.New("profile: not cached")

// Service runs profile jobs and serves cached results. It's the only
// type in this package the HTTP layer should hold.
//
// The triangle is intentional:
//
//	Service.Get(tenant, assetID)
//	  ├─ cache hit and fresh enough? return cached
//	  ├─ otherwise: Reader → TableInfo, build SQL, run via Executor,
//	  │              decode, persist via Cache, return
//	  └─ never block the API thread on multi-minute scans —
//	     RunStaleness caps how long a sync compute is allowed to take
//	     before falling back to "stale + scheduling a background refresh"
type Service struct {
	Reader    AssetReader
	Cache     Cache
	Warehouse *warehouse.MultiFactory
	Logger    *slog.Logger

	// FreshFor: a report newer than this is served as-is. Default 24h.
	FreshFor time.Duration
	// SampleRows: rows to scan per profile query. Default 100k.
	// 0 = scan whole table (only for small tables / dev).
	SampleRows int
	// SyncTimeout: max wall-clock for a sync profile run. Default 60s.
	// Beyond this the service returns a stale report with a flag and
	// (when wired) enqueues a background refresh.
	SyncTimeout time.Duration
}

// Get returns the freshest available report. Re-runs the profile only
// if the cache is empty or stale.
func (s *Service) Get(ctx context.Context, tenantID, tableAssetID string) (*Report, error) {
	if s.Cache == nil || s.Reader == nil || s.Warehouse == nil {
		return nil, errors.New("profile: service not fully configured")
	}
	logger := s.logger()
	freshFor := s.FreshFor
	if freshFor <= 0 {
		freshFor = 24 * time.Hour
	}
	cached, err := s.Cache.Get(ctx, tenantID, tableAssetID)
	if err == nil && cached != nil && time.Since(cached.GeneratedAt) < freshFor {
		return cached, nil
	}
	report, err := s.compute(ctx, tenantID, tableAssetID)
	if err != nil {
		// Serve stale on failure — broken profile shouldn't blank the
		// whole asset page. Caller can show "last refreshed X ago".
		if cached != nil {
			logger.WarnContext(ctx, "profile: compute failed, serving stale", "err", err)
			return cached, nil
		}
		return nil, err
	}
	if err := s.Cache.Put(ctx, report); err != nil {
		logger.WarnContext(ctx, "profile: cache put", "err", err)
	}
	return report, nil
}

// Refresh forces a recompute regardless of cache freshness. Used by a
// "refresh now" button on the UI.
func (s *Service) Refresh(ctx context.Context, tenantID, tableAssetID string) (*Report, error) {
	if s.Cache == nil || s.Reader == nil || s.Warehouse == nil {
		return nil, errors.New("profile: service not fully configured")
	}
	report, err := s.compute(ctx, tenantID, tableAssetID)
	if err != nil {
		return nil, err
	}
	if err := s.Cache.Put(ctx, report); err != nil {
		s.logger().WarnContext(ctx, "profile: cache put", "err", err)
	}
	return report, nil
}

// compute is the actual work: open executor, build + run the
// aggregate SQL, decode, run top-values queries per column, assemble
// Report.
func (s *Service) compute(ctx context.Context, tenantID, tableAssetID string) (*Report, error) {
	info, err := s.Reader.ReadTable(ctx, tenantID, tableAssetID)
	if err != nil {
		return nil, fmt.Errorf("read table info: %w", err)
	}
	if len(info.Columns) == 0 {
		return nil, fmt.Errorf("profile: table has no columns recorded in catalog")
	}
	dialect, err := PickDialect(info.Type)
	if err != nil {
		return nil, err
	}
	timeout := s.SyncTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execr, err := s.Warehouse.Open(runCtx, tenantID, info.ConnectionID)
	if err != nil {
		return nil, fmt.Errorf("open warehouse: %w", err)
	}
	sampleRows := s.SampleRows
	if sampleRows <= 0 {
		sampleRows = 100_000
	}
	q := BuildAggregateQuery(dialect, info.Schema, info.Table, info.Columns, sampleRows)
	rows, err := execr.Query(runCtx, q)
	if err != nil {
		return nil, fmt.Errorf("aggregate query: %w", err)
	}
	report, err := s.decodeAggregate(rows, info)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}

	// Top values per column. Skip columns whose distinct_count is the
	// same as row count (every value is unique — useless top-N) or
	// columns we couldn't aggregate (no comparable type).
	for i, col := range info.Columns {
		if i >= len(report.Columns) {
			break
		}
		rc := &report.Columns[i]
		if rc.DistinctCount == 0 || rc.DistinctCount == rc.RowsSampled-rc.NullCount {
			continue
		}
		if rc.RowsSampled == 0 {
			continue
		}
		if err := s.fillTopValues(runCtx, execr, dialect, info, col.Name, rc); err != nil {
			s.logger().WarnContext(ctx, "profile: top values", "column", col.Name, "err", err)
			// Non-fatal: the column-level stats still ship.
		}
	}
	report.TableAssetID = tableAssetID
	report.Schema = info.Schema
	report.Table = info.Table
	report.GeneratedAt = time.Now().UTC()
	return report, nil
}

func (s *Service) fillTopValues(
	ctx context.Context,
	execr warehouse.Executor,
	dialect Dialect,
	info *TableInfo,
	column string,
	rc *Column,
) error {
	q := BuildTopValuesQuery(dialect, info.Schema, info.Table, column, 5)
	rows, err := execr.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := rows.Columns()
	if len(cols) != 2 {
		return fmt.Errorf("top values: expected 2 cols, got %d", len(cols))
	}
	for rows.Next() {
		var val, cnt any
		if err := rows.Scan(&val, &cnt); err != nil {
			return err
		}
		rc.TopValues = append(rc.TopValues, TopValue{
			Value: stringify(val),
			Count: toInt64(cnt),
		})
	}
	return rows.Err()
}

// decodeAggregate scans the one-row aggregate result. Column layout
// matches BuildAggregateQuery exactly: __rows, then per-input-column
// nulls/distinct/(min,max)/(mean) in declaration order.
func (s *Service) decodeAggregate(rows warehouse.Rows, info *TableInfo) (*Report, error) {
	cols := rows.Columns()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("aggregate query returned no rows")
	}
	scanDest := make([]any, len(cols))
	scanPtrs := make([]any, len(cols))
	for i := range scanDest {
		scanPtrs[i] = &scanDest[i]
	}
	if err := rows.Scan(scanPtrs...); err != nil {
		return nil, err
	}

	report := &Report{Columns: make([]Column, 0, len(info.Columns))}
	report.RowsScanned = toInt64(scanDest[0])
	idx := 1
	for _, spec := range info.Columns {
		rc := Column{
			Name:        spec.Name,
			DataType:    spec.DataType,
			RowsSampled: report.RowsScanned,
		}
		if idx < len(scanDest) {
			rc.NullCount = toInt64(scanDest[idx])
			idx++
		}
		if idx < len(scanDest) {
			rc.DistinctCount = toInt64(scanDest[idx])
			idx++
		}
		if isComparable(spec.DataType) {
			if idx < len(scanDest) {
				rc.Min = optString(scanDest[idx])
				idx++
			}
			if idx < len(scanDest) {
				rc.Max = optString(scanDest[idx])
				idx++
			}
		}
		if isNumeric(spec.DataType) {
			if idx < len(scanDest) {
				rc.Mean = optFloat(scanDest[idx])
				idx++
			}
		}
		report.Columns = append(report.Columns, rc)
	}
	return report, nil
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// --- coercion helpers: driver decoders return wildly different types ---

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int32:
		return int64(x)
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case []byte:
		n, _ := strconv.ParseInt(string(x), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	default:
		return 0
	}
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprint(v)
	}
}

func optString(v any) *string {
	if v == nil {
		return nil
	}
	s := stringify(v)
	if s == "" {
		return nil
	}
	return &s
}

func optFloat(v any) *float64 {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case float64:
		return &x
	case float32:
		f := float64(x)
		return &f
	case int64:
		f := float64(x)
		return &f
	case []byte:
		if f, err := strconv.ParseFloat(string(x), 64); err == nil {
			return &f
		}
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return &f
		}
	}
	return nil
}
