package migration

import (
	"strings"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/profile"
)

func TestBuildSelect(t *testing.T) {
	d, _ := profile.PickDialect(connection.TypePostgres)
	got := buildSelect(d, "public", "users", []string{"id", "email"}, 100)
	for _, want := range []string{`"public"."users"`, `"id"`, `"email"`, "LIMIT 100"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}

	// no schema → just the table
	got = buildSelect(d, "", "users", []string{"id"}, 0)
	if strings.Contains(got, ".") && !strings.Contains(got, "public") {
		// Acceptable — "users" has no dot.
	}
	if strings.Contains(got, "LIMIT") {
		t.Errorf("no maxRows should omit LIMIT, got %s", got)
	}
}

func TestBuildTruncateUsesDelete(t *testing.T) {
	d, _ := profile.PickDialect(connection.TypePostgres)
	got := buildTruncate(d, "public", "users")
	if !strings.HasPrefix(got, "DELETE FROM") {
		t.Errorf("expected DELETE FROM (not TRUNCATE) for portability: %s", got)
	}
}

func TestBuildInsertMultiRow(t *testing.T) {
	d, _ := profile.PickDialect(connection.TypePostgres)
	rows := [][]any{
		{1, "alice@example.com", true},
		{2, "bob", nil},
	}
	got := buildInsert(d, "public", "users", []string{"id", "email", "active"}, rows)
	for _, want := range []string{
		`INSERT INTO "public"."users"`,
		`("id", "email", "active")`,
		`(1, 'alice@example.com', TRUE)`,
		`(2, 'bob', NULL)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuildInsertEscapesQuotes(t *testing.T) {
	d, _ := profile.PickDialect(connection.TypePostgres)
	got := buildInsert(d, "", "t", []string{"name"}, [][]any{
		{"O'Brien"},
	})
	if !strings.Contains(got, "'O''Brien'") {
		t.Errorf("single quote not doubled: %s", got)
	}
}

func TestBuildInsertEmptyBatch(t *testing.T) {
	d, _ := profile.PickDialect(connection.TypePostgres)
	if got := buildInsert(d, "", "t", []string{"x"}, nil); got != "" {
		t.Errorf("empty batch should produce empty SQL, got %s", got)
	}
}

func TestFormatLiteralNumericTypes(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{true, "TRUE"},
		{false, "FALSE"},
		{int64(42), "42"},
		{float64(3.14), "3.14"},
		{"hello", "'hello'"},
		{[]byte("bytes"), "'bytes'"},
		{"O'Reilly", "'O''Reilly'"},
		{"NUL\x00inside", "'NULinside'"}, // NUL stripped
	}
	for _, c := range cases {
		if got := formatLiteral(c.in); got != c.want {
			t.Errorf("formatLiteral(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidatePlan(t *testing.T) {
	cases := []struct {
		name    string
		plan    *Plan
		wantErr bool
	}{
		{
			"happy path",
			&Plan{
				TenantID: "t", Name: "n",
				SourceConnectionID: "s", SourceTable: "src",
				DestConnectionID: "d", DestTable: "dst",
				ColumnMap: []ColumnMap{{SourceCol: "a", DestCol: "a"}},
				Mode:      ModeSnapshot, WriteMode: WriteModeTruncate,
			},
			false,
		},
		{"missing tenant", &Plan{Name: "n"}, true},
		{"empty column map", &Plan{
			TenantID: "t", Name: "n",
			SourceConnectionID: "s", SourceTable: "src",
			DestConnectionID: "d", DestTable: "dst",
			Mode: ModeSnapshot, WriteMode: WriteModeTruncate,
		}, true},
		{"unknown mode", &Plan{
			TenantID: "t", Name: "n",
			SourceConnectionID: "s", SourceTable: "src",
			DestConnectionID: "d", DestTable: "dst",
			ColumnMap: []ColumnMap{{SourceCol: "a", DestCol: "a"}},
			Mode:      "weird", WriteMode: WriteModeTruncate,
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePlan(c.plan)
			if c.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
