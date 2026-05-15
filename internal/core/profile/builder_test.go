package profile

import (
	"strings"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/connection"
)

// TestBuildAggregateQuery exercises the per-dialect quirks that are
// most likely to bite: identifier quoting, sample clause placement,
// approx-distinct selection, and per-column type-aware projection.
func TestBuildAggregateQuery(t *testing.T) {
	cols := []ColumnSpec{
		{Name: "id", DataType: "integer"},
		{Name: "email", DataType: "varchar"},
		{Name: "payload", DataType: "jsonb"}, // non-comparable
	}

	t.Run("postgres uses double quotes and outer LIMIT", func(t *testing.T) {
		d, _ := PickDialect(connection.TypePostgres)
		q := BuildAggregateQuery(d, "public", "users", cols, 1000)
		if !strings.Contains(q, `"public"."users"`) {
			t.Errorf("expected double-quoted ref, got %s", q)
		}
		if !strings.Contains(q, "LIMIT 1000") {
			t.Errorf("expected LIMIT fallback for Postgres, got %s", q)
		}
		if strings.Contains(q, "APPROX_COUNT_DISTINCT") {
			t.Errorf("Postgres should fall back to COUNT(DISTINCT), got %s", q)
		}
		// JSON column must NOT get MIN/MAX projections.
		if strings.Contains(q, `MIN("payload")`) {
			t.Errorf("JSON column should not be MIN'd, got %s", q)
		}
	})

	t.Run("snowflake uses SAMPLE clause + APPROX_COUNT_DISTINCT", func(t *testing.T) {
		d, _ := PickDialect(connection.TypeSnowflake)
		q := BuildAggregateQuery(d, "ANALYTICS", "USERS", cols, 5000)
		if !strings.Contains(q, "SAMPLE (5000 ROWS)") {
			t.Errorf("expected Snowflake SAMPLE clause, got %s", q)
		}
		if !strings.Contains(q, "APPROX_COUNT_DISTINCT") {
			t.Errorf("Snowflake should use APPROX_COUNT_DISTINCT, got %s", q)
		}
	})

	t.Run("mysql uses backticks", func(t *testing.T) {
		d, _ := PickDialect(connection.TypeMySQL)
		q := BuildAggregateQuery(d, "app", "users", cols, 0)
		if !strings.Contains(q, "`app`.`users`") {
			t.Errorf("expected backtick-quoted ref, got %s", q)
		}
	})

	t.Run("redshift re-uses postgres dialect", func(t *testing.T) {
		d, _ := PickDialect(connection.TypeRedshift)
		q := BuildAggregateQuery(d, "public", "events", cols, 100)
		if !strings.Contains(q, `"public"."events"`) {
			t.Errorf("redshift should quote like postgres, got %s", q)
		}
	})

	t.Run("document source rejected", func(t *testing.T) {
		if _, err := PickDialect(connection.TypeDynamoDB); err == nil {
			t.Error("expected document type to be rejected, got nil error")
		}
	})

	t.Run("numeric column gets mean", func(t *testing.T) {
		d, _ := PickDialect(connection.TypePostgres)
		q := BuildAggregateQuery(d, "", "t", []ColumnSpec{
			{Name: "price", DataType: "numeric"},
		}, 100)
		if !strings.Contains(q, `AVG(CAST("price"`) {
			t.Errorf("numeric column should get AVG, got %s", q)
		}
	})

	t.Run("empty cols returns empty query", func(t *testing.T) {
		d, _ := PickDialect(connection.TypePostgres)
		q := BuildAggregateQuery(d, "public", "users", nil, 100)
		if q != "" {
			t.Errorf("expected empty query for no cols, got %s", q)
		}
	})
}

func TestBuildTopValuesQuery(t *testing.T) {
	d, _ := PickDialect(connection.TypePostgres)
	q := BuildTopValuesQuery(d, "public", "users", "country", 5)
	for _, want := range []string{
		`"public"."users"`,
		`"country"`,
		"WHERE",
		"IS NOT NULL",
		"GROUP BY",
		"ORDER BY cnt DESC",
		"LIMIT 5",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("expected query to contain %q, got: %s", want, q)
		}
	}
}
