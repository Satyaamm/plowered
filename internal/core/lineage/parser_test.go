package lineage_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/lineage"
)

func sources(s lineage.Statement) []string {
	out := append([]string(nil), s.Sources...)
	sort.Strings(out)
	return out
}

func TestInsertSelect(t *testing.T) {
	stmts := lineage.Parse(`INSERT INTO mart.orders SELECT * FROM raw.orders r JOIN raw.customers c ON c.id = r.customer_id`)
	if len(stmts) != 1 {
		t.Fatalf("got %d statements", len(stmts))
	}
	s := stmts[0]
	if s.Op != lineage.OpInsert {
		t.Errorf("op = %s", s.Op)
	}
	if s.Target != "mart.orders" {
		t.Errorf("target = %q", s.Target)
	}
	want := []string{"raw.customers", "raw.orders"}
	if got := sources(s); !reflect.DeepEqual(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
}

func TestCreateTableAs(t *testing.T) {
	stmts := lineage.Parse(`CREATE TABLE analytics.daily_revenue AS SELECT date, SUM(amount) FROM raw.payments GROUP BY date`)
	if len(stmts) != 1 || stmts[0].Op != lineage.OpCreateTableAs {
		t.Fatalf("got %+v", stmts)
	}
	if stmts[0].Target != "analytics.daily_revenue" {
		t.Errorf("target = %q", stmts[0].Target)
	}
	if !reflect.DeepEqual(stmts[0].Sources, []string{"raw.payments"}) {
		t.Errorf("sources = %v", stmts[0].Sources)
	}
}

func TestCreateOrReplaceView(t *testing.T) {
	stmts := lineage.Parse(`CREATE OR REPLACE VIEW analytics.active_users AS SELECT id FROM raw.users WHERE active = true`)
	if len(stmts) != 1 || stmts[0].Op != lineage.OpCreateView {
		t.Fatalf("got %+v", stmts)
	}
	if stmts[0].Target != "analytics.active_users" {
		t.Errorf("target = %q", stmts[0].Target)
	}
}

func TestMerge(t *testing.T) {
	stmts := lineage.Parse(`MERGE INTO mart.dim_customers t USING raw.customer_updates s ON t.id = s.id WHEN MATCHED THEN UPDATE SET t.name = s.name`)
	if len(stmts) != 1 || stmts[0].Op != lineage.OpMerge {
		t.Fatalf("got %+v", stmts)
	}
	if stmts[0].Target != "mart.dim_customers" {
		t.Errorf("target = %q", stmts[0].Target)
	}
	if !contains(stmts[0].Sources, "raw.customer_updates") {
		t.Errorf("missing source: %v", stmts[0].Sources)
	}
}

func TestWithCTE(t *testing.T) {
	sql := `WITH a AS (SELECT * FROM raw.x), b AS (SELECT * FROM raw.y)
		INSERT INTO mart.combined SELECT * FROM a JOIN b ON a.id = b.id`
	stmts := lineage.Parse(sql)
	if len(stmts) != 1 {
		t.Fatalf("got %d statements", len(stmts))
	}
	if stmts[0].Target != "mart.combined" {
		t.Errorf("target = %q", stmts[0].Target)
	}
	// CTE bodies should contribute raw.x and raw.y as sources.
	if !contains(stmts[0].Sources, "raw.x") || !contains(stmts[0].Sources, "raw.y") {
		t.Errorf("sources missing CTE refs: %v", stmts[0].Sources)
	}
}

func TestMultipleStatements(t *testing.T) {
	sql := `
		CREATE TABLE a AS SELECT * FROM x;
		INSERT INTO b SELECT * FROM a;
	`
	stmts := lineage.Parse(sql)
	if len(stmts) != 2 {
		t.Fatalf("got %d statements", len(stmts))
	}
}

func TestCommentsStripped(t *testing.T) {
	sql := `
		-- this is a comment about the table
		/* and a block comment */
		INSERT INTO mart.t SELECT * FROM raw.s;
	`
	stmts := lineage.Parse(sql)
	if len(stmts) != 1 {
		t.Fatalf("got %d", len(stmts))
	}
	if !contains(stmts[0].Sources, "raw.s") {
		t.Errorf("sources = %v", stmts[0].Sources)
	}
}

func TestStringLiteralsIgnored(t *testing.T) {
	sql := `INSERT INTO logs.events SELECT 'fake_table_in_string', col FROM raw.real`
	stmts := lineage.Parse(sql)
	if len(stmts) != 1 {
		t.Fatalf("got %d", len(stmts))
	}
	if contains(stmts[0].Sources, "fake_table_in_string") {
		t.Errorf("string literal leaked into sources: %v", stmts[0].Sources)
	}
	if !contains(stmts[0].Sources, "raw.real") {
		t.Errorf("missing real source: %v", stmts[0].Sources)
	}
}

func TestQuotedIdentifiers(t *testing.T) {
	sql := `INSERT INTO "Mart"."Orders" SELECT * FROM "Raw"."Orders"`
	stmts := lineage.Parse(sql)
	if len(stmts) != 1 {
		t.Fatalf("got %d", len(stmts))
	}
	if stmts[0].Target != "mart.orders" {
		t.Errorf("target = %q", stmts[0].Target)
	}
}

func TestExtractAndResolve(t *testing.T) {
	stmts := lineage.Parse(`INSERT INTO mart.t SELECT * FROM raw.a JOIN raw.b ON a.id = b.id`)
	props := lineage.Extract(stmts)
	if len(props) != 2 {
		t.Fatalf("want 2 proposals, got %d", len(props))
	}

	r := lineage.Resolver{
		LookupQN: func(qn string) string {
			if qn == "raw.a" || qn == "raw.b" || qn == "mart.t" {
				return "id-of-" + qn
			}
			return ""
		},
	}
	edges, misses := r.Resolve(props)
	if len(edges) != 2 || len(misses) != 0 {
		t.Errorf("edges=%d misses=%d", len(edges), len(misses))
	}
}

func TestResolveMissingRecorded(t *testing.T) {
	stmts := lineage.Parse(`INSERT INTO mart.t SELECT * FROM raw.unknown`)
	props := lineage.Extract(stmts)
	r := lineage.Resolver{LookupQN: func(qn string) string {
		if qn == "mart.t" {
			return "id-mart.t"
		}
		return ""
	}}
	edges, misses := r.Resolve(props)
	if len(edges) != 0 || len(misses) != 1 {
		t.Errorf("edges=%d misses=%v", len(edges), misses)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
