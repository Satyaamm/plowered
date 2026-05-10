package quality

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DataSource is the slim view of the customer's data warehouse the check
// runner needs. Concrete implementations wrap a pgx Conn; the test suite
// uses a fake.
type DataSource interface {
	QueryScalar(ctx context.Context, sql string, args ...any) (any, error)
	QueryTime(ctx context.Context, sql string, args ...any) (time.Time, error)
}

// Runner executes a Check against a DataSource and produces a CheckRun.
// Concrete check types are dispatched here rather than via a separate
// registry — five built-ins is small enough that a switch is clearer than
// indirection.
type Runner struct {
	Now func() time.Time
}

func NewRunner() *Runner {
	return &Runner{Now: time.Now}
}

// Run evaluates check against ds and returns a CheckRun with the verdict
// captured. Errors that prevent evaluation produce an OutcomeError CheckRun
// rather than a Go error — the caller wants the result persisted either way.
func (r *Runner) Run(ctx context.Context, check Check, ds DataSource) *CheckRun {
	now := r.Now
	if now == nil {
		now = time.Now
	}
	cr := &CheckRun{
		ID:        newID(),
		TenantID:  check.TenantID,
		CheckID:   check.ID,
		AssetID:   check.AssetID,
		StartedAt: now().UTC(),
		Severity:  check.Severity,
	}
	defer func() {
		cr.FinishedAt = now().UTC()
		cr.Duration = cr.FinishedAt.Sub(cr.StartedAt)
	}()

	switch check.Type {
	case CheckRowCount:
		runRowCount(ctx, check, ds, cr)
	case CheckNotNull:
		runNotNull(ctx, check, ds, cr)
	case CheckFreshness:
		runFreshness(ctx, check, ds, cr, now())
	case CheckUniqueness:
		runUniqueness(ctx, check, ds, cr)
	case CheckCustomSQL:
		runCustomSQL(ctx, check, ds, cr)
	default:
		cr.Outcome = OutcomeError
		cr.Diagnostic = fmt.Sprintf("unsupported check type %q", check.Type)
	}
	return cr
}

// runRowCount asserts the asset has rows. Config: {"min": int, "max": int (optional)}.
func runRowCount(ctx context.Context, c Check, ds DataSource, cr *CheckRun) {
	tableExpr, err := configString(c.Config, "table")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	min := configFloat(c.Config, "min", 1)
	max := configFloat(c.Config, "max", -1)

	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableExpr)
	v, err := queryFloat(ctx, ds, q)
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	cr.Value = v
	cr.Threshold = min
	switch {
	case v < min:
		cr.Outcome = OutcomeFail
		cr.Diagnostic = fmt.Sprintf("row count %d below minimum %d", int64(v), int64(min))
	case max > 0 && v > max:
		cr.Outcome = OutcomeFail
		cr.Diagnostic = fmt.Sprintf("row count %d above maximum %d", int64(v), int64(max))
	default:
		cr.Outcome = OutcomePass
		cr.Diagnostic = fmt.Sprintf("row count %d", int64(v))
	}
}

// runNotNull counts NULLs in a column. Config: {"table": str, "column": str, "max_nulls": int}.
func runNotNull(ctx context.Context, c Check, ds DataSource, cr *CheckRun) {
	table, err := configString(c.Config, "table")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	col, err := configString(c.Config, "column")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	maxNulls := configFloat(c.Config, "max_nulls", 0)
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NULL", table, col)
	v, err := queryFloat(ctx, ds, q)
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	cr.Value = v
	cr.Threshold = maxNulls
	if v > maxNulls {
		cr.Outcome = OutcomeFail
		cr.Diagnostic = fmt.Sprintf("%d nulls in %s.%s exceeds %d", int64(v), table, col, int64(maxNulls))
	} else {
		cr.Outcome = OutcomePass
		cr.Diagnostic = fmt.Sprintf("%d nulls in %s.%s", int64(v), table, col)
	}
}

// runFreshness asserts MAX(timestamp_column) is recent.
// Config: {"table": str, "column": str, "max_age_seconds": int}.
func runFreshness(ctx context.Context, c Check, ds DataSource, cr *CheckRun, now time.Time) {
	table, err := configString(c.Config, "table")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	col, err := configString(c.Config, "column")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	maxAge := configFloat(c.Config, "max_age_seconds", 24*3600)
	q := fmt.Sprintf("SELECT MAX(%s) FROM %s", col, table)
	t, err := ds.QueryTime(ctx, q)
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	age := now.Sub(t).Seconds()
	cr.Value = age
	cr.Threshold = maxAge
	cr.Properties = map[string]any{"last_modified": t.UTC().Format(time.RFC3339)}
	if age > maxAge {
		cr.Outcome = OutcomeFail
		cr.Diagnostic = fmt.Sprintf("last update %s ago, threshold %s",
			time.Duration(age*float64(time.Second)).Round(time.Second),
			time.Duration(maxAge*float64(time.Second)).Round(time.Second))
	} else {
		cr.Outcome = OutcomePass
		cr.Diagnostic = fmt.Sprintf("last update %s ago", time.Duration(age*float64(time.Second)).Round(time.Second))
	}
}

// runUniqueness counts duplicate values in column(s).
// Config: {"table": str, "columns": ["col1","col2",...]}
func runUniqueness(ctx context.Context, c Check, ds DataSource, cr *CheckRun) {
	table, err := configString(c.Config, "table")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	cols, err := configStringSlice(c.Config, "columns")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	if len(cols) == 0 {
		cr.Outcome = OutcomeError
		cr.Diagnostic = "uniqueness check requires non-empty columns"
		return
	}
	keyExpr := strings.Join(cols, ", ")
	q := fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT %s, COUNT(*) AS dup
			FROM %s
			GROUP BY %s
			HAVING COUNT(*) > 1
		) d`, keyExpr, table, keyExpr)
	v, err := queryFloat(ctx, ds, q)
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	cr.Value = v
	cr.Threshold = 0
	if v > 0 {
		cr.Outcome = OutcomeFail
		cr.Diagnostic = fmt.Sprintf("%d duplicate (%s) groups in %s", int64(v), keyExpr, table)
	} else {
		cr.Outcome = OutcomePass
		cr.Diagnostic = fmt.Sprintf("(%s) is unique in %s", keyExpr, table)
	}
}

// runCustomSQL runs the user's SQL. Convention: query must return exactly
// one row, one column. The cell is interpreted as a count of *failing* rows;
// 0 means pass. Allows users to express anything they can write in SQL.
// Config: {"sql": str}
func runCustomSQL(ctx context.Context, c Check, ds DataSource, cr *CheckRun) {
	sql, err := configString(c.Config, "sql")
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	v, err := queryFloat(ctx, ds, sql)
	if err != nil {
		cr.Outcome = OutcomeError
		cr.Diagnostic = err.Error()
		return
	}
	cr.Value = v
	cr.Threshold = 0
	if v > 0 {
		cr.Outcome = OutcomeFail
		cr.Diagnostic = fmt.Sprintf("custom check returned %d failing rows", int64(v))
	} else {
		cr.Outcome = OutcomePass
		cr.Diagnostic = "custom check passed"
	}
}

// ----- helpers -----

func queryFloat(ctx context.Context, ds DataSource, sql string, args ...any) (float64, error) {
	v, err := ds.QueryScalar(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	switch x := v.(type) {
	case int64:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	}
	return 0, fmt.Errorf("unexpected scalar type %T", v)
}

func configString(m map[string]any, key string) (string, error) {
	v, ok := m[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("check config missing %q", key)
	}
	return v, nil
}

func configStringSlice(m map[string]any, key string) ([]string, error) {
	v, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("check config missing %q", key)
	}
	switch x := v.(type) {
	case []string:
		return x, nil
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, errors.New("expected []string")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("expected []string for %q", key)
}

func configFloat(m map[string]any, key string, def float64) float64 {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return def
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "id-fallback"
	}
	return hex.EncodeToString(b[:])
}
