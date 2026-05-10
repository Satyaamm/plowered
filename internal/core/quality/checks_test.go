package quality_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Satyaamm/plowered/internal/core/quality"
)

// fakeDS is a programmable DataSource for tests.
type fakeDS struct {
	scalar  any
	tstamp  time.Time
	scalarErr error
	timeErr   error
	gotSQL    string
}

func (f *fakeDS) QueryScalar(_ context.Context, sql string, _ ...any) (any, error) {
	f.gotSQL = sql
	if f.scalarErr != nil {
		return nil, f.scalarErr
	}
	return f.scalar, nil
}
func (f *fakeDS) QueryTime(_ context.Context, sql string, _ ...any) (time.Time, error) {
	f.gotSQL = sql
	if f.timeErr != nil {
		return time.Time{}, f.timeErr
	}
	return f.tstamp, nil
}

func TestRowCountPass(t *testing.T) {
	ds := &fakeDS{scalar: int64(150)}
	cr := quality.NewRunner().Run(context.Background(), quality.Check{
		Type: quality.CheckRowCount,
		Config: map[string]any{"table": "mart.orders", "min": 100.0},
	}, ds)
	if cr.Outcome != quality.OutcomePass {
		t.Errorf("outcome = %s, diag = %q", cr.Outcome, cr.Diagnostic)
	}
}

func TestRowCountFail(t *testing.T) {
	ds := &fakeDS{scalar: int64(0)}
	cr := quality.NewRunner().Run(context.Background(), quality.Check{
		Type: quality.CheckRowCount,
		Config: map[string]any{"table": "mart.orders", "min": 100.0},
	}, ds)
	if cr.Outcome != quality.OutcomeFail {
		t.Errorf("outcome = %s", cr.Outcome)
	}
	if cr.Value != 0 || cr.Threshold != 100 {
		t.Errorf("value/threshold = %v/%v", cr.Value, cr.Threshold)
	}
}

func TestNotNullFail(t *testing.T) {
	ds := &fakeDS{scalar: int64(7)}
	cr := quality.NewRunner().Run(context.Background(), quality.Check{
		Type: quality.CheckNotNull,
		Config: map[string]any{
			"table": "mart.orders", "column": "customer_id", "max_nulls": 0.0,
		},
	}, ds)
	if cr.Outcome != quality.OutcomeFail {
		t.Errorf("outcome = %s", cr.Outcome)
	}
}

func TestFreshnessFailWhenStale(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	r := &quality.Runner{Now: func() time.Time { return now }}
	ds := &fakeDS{tstamp: now.Add(-30 * time.Hour)}
	cr := r.Run(context.Background(), quality.Check{
		Type: quality.CheckFreshness,
		Config: map[string]any{
			"table": "mart.events", "column": "created_at", "max_age_seconds": 86400.0,
		},
	}, ds)
	if cr.Outcome != quality.OutcomeFail {
		t.Errorf("outcome = %s, diag = %q", cr.Outcome, cr.Diagnostic)
	}
}

func TestFreshnessPassWhenRecent(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	r := &quality.Runner{Now: func() time.Time { return now }}
	ds := &fakeDS{tstamp: now.Add(-1 * time.Hour)}
	cr := r.Run(context.Background(), quality.Check{
		Type: quality.CheckFreshness,
		Config: map[string]any{
			"table": "mart.events", "column": "created_at", "max_age_seconds": 86400.0,
		},
	}, ds)
	if cr.Outcome != quality.OutcomePass {
		t.Errorf("outcome = %s", cr.Outcome)
	}
}

func TestUniquenessFail(t *testing.T) {
	ds := &fakeDS{scalar: int64(3)}
	cr := quality.NewRunner().Run(context.Background(), quality.Check{
		Type: quality.CheckUniqueness,
		Config: map[string]any{
			"table": "mart.orders", "columns": []any{"order_id"},
		},
	}, ds)
	if cr.Outcome != quality.OutcomeFail {
		t.Errorf("outcome = %s", cr.Outcome)
	}
}

func TestCustomSQLPass(t *testing.T) {
	ds := &fakeDS{scalar: int64(0)}
	cr := quality.NewRunner().Run(context.Background(), quality.Check{
		Type: quality.CheckCustomSQL,
		Config: map[string]any{
			"sql": "SELECT COUNT(*) FROM mart.orders WHERE total < 0",
		},
	}, ds)
	if cr.Outcome != quality.OutcomePass {
		t.Errorf("outcome = %s, diag = %q", cr.Outcome, cr.Diagnostic)
	}
}

func TestCustomSQLDBError(t *testing.T) {
	ds := &fakeDS{scalarErr: errors.New("syntax error")}
	cr := quality.NewRunner().Run(context.Background(), quality.Check{
		Type: quality.CheckCustomSQL,
		Config: map[string]any{"sql": "broken"},
	}, ds)
	if cr.Outcome != quality.OutcomeError {
		t.Errorf("outcome = %s, want error", cr.Outcome)
	}
}

func TestUnsupportedType(t *testing.T) {
	ds := &fakeDS{}
	cr := quality.NewRunner().Run(context.Background(), quality.Check{Type: "unknown"}, ds)
	if cr.Outcome != quality.OutcomeError {
		t.Errorf("outcome = %s", cr.Outcome)
	}
}
