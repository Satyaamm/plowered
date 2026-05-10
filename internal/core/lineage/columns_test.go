package lineage

import (
	"testing"
)

func TestExtractColumns_Identity(t *testing.T) {
	edges := ExtractColumns("SELECT id, email FROM users", "stage.users_stage")
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d (%v)", len(edges), edges)
	}
	for _, e := range edges {
		if e.Transform != "identity" {
			t.Errorf("want identity, got %q for %s", e.Transform, e.TargetColumn)
		}
		if e.SourceTable != "users" {
			t.Errorf("want source users, got %q", e.SourceTable)
		}
		if e.TargetTable != "stage.users_stage" {
			t.Errorf("target wrong: %q", e.TargetTable)
		}
	}
}

func TestExtractColumns_Aliased(t *testing.T) {
	edges := ExtractColumns("SELECT id AS user_id, lower(email) AS email_lc FROM users u", "out")
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d (%v)", len(edges), edges)
	}
	if edges[0].TargetColumn != "user_id" || edges[0].Transform != "identity" {
		t.Errorf("expected identity user_id, got %+v", edges[0])
	}
	if edges[1].TargetColumn != "email_lc" || edges[1].Transform != "expression" {
		t.Errorf("expected expression email_lc, got %+v", edges[1])
	}
	if edges[1].SourceColumn != "email" {
		t.Errorf("expected SourceColumn=email, got %q", edges[1].SourceColumn)
	}
}

func TestExtractColumns_Join(t *testing.T) {
	edges := ExtractColumns(
		"SELECT u.id, p.title FROM users u JOIN posts p ON p.user_id = u.id",
		"out",
	)
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d (%v)", len(edges), edges)
	}
	want := map[string]string{"id": "users", "title": "posts"}
	for _, e := range edges {
		if want[e.TargetColumn] != e.SourceTable {
			t.Errorf("target %s expected source %s, got %s",
				e.TargetColumn, want[e.TargetColumn], e.SourceTable)
		}
	}
}

func TestExtractColumns_Wildcard(t *testing.T) {
	edges := ExtractColumns("SELECT * FROM users", "out")
	if len(edges) != 1 || edges[0].Transform != "wildcard" {
		t.Fatalf("expected one wildcard edge, got %+v", edges)
	}
}

func TestExtractColumns_ExpressionMultiCol(t *testing.T) {
	edges := ExtractColumns(
		"SELECT u.first_name || ' ' || u.last_name AS full_name FROM users u",
		"out",
	)
	if len(edges) != 2 {
		t.Fatalf("want 2 expression edges, got %d (%v)", len(edges), edges)
	}
	for _, e := range edges {
		if e.TargetColumn != "full_name" || e.Transform != "expression" {
			t.Errorf("unexpected edge: %+v", e)
		}
		if e.SourceTable != "users" {
			t.Errorf("source table wrong: %+v", e)
		}
	}
}

func TestExtractColumns_NoFromConstant(t *testing.T) {
	edges := ExtractColumns("SELECT 1 AS one", "out")
	if len(edges) != 1 || edges[0].TargetColumn != "one" || edges[0].SourceColumn != "" {
		t.Fatalf("unexpected edges: %+v", edges)
	}
}

func TestExtractColumns_FunctionCall(t *testing.T) {
	edges := ExtractColumns("SELECT count(*) AS n FROM events", "out")
	if len(edges) == 0 {
		t.Fatalf("expected at least one edge")
	}
	if edges[0].TargetColumn != "n" {
		t.Errorf("alias wrong: %+v", edges[0])
	}
}
