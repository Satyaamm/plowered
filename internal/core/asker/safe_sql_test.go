package asker

import (
	"errors"
	"testing"
)

// TestValidateSelectOnly is the security-critical surface. Each case
// is a real-world prompt or injection we want to deny — keep this
// table comprehensive.
func TestValidateSelectOnly(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		// Allowed
		{"simple select", `SELECT 1`, false},
		{"select with where", `SELECT id FROM users WHERE email LIKE '%@example.com'`, false},
		{"select with trailing semicolon", `SELECT 1;`, false},
		{"with cte", `WITH x AS (SELECT 1) SELECT * FROM x`, false},
		{"select with -- inside string", `SELECT 'a -- not a comment' FROM t`, false},
		{"select with delete keyword inside string literal", `SELECT 'DELETE was scheduled' FROM ops`, false},
		{"select preceded by block comment", `/* lookup users */ SELECT id FROM users`, false},
		{"select with line comment after", "SELECT 1 -- trailing comment", false},
		{"quoted identifier with reserved word", `SELECT "delete_at" FROM "tickets"`, false},

		// Denied — verbs
		{"insert", `INSERT INTO users VALUES (1)`, true},
		{"update", `UPDATE users SET email='x' WHERE id=1`, true},
		{"delete", `DELETE FROM users`, true},
		{"drop", `DROP TABLE users`, true},
		{"truncate", `TRUNCATE TABLE users`, true},
		{"alter", `ALTER TABLE users ADD COLUMN x INT`, true},
		{"create", `CREATE TABLE x (id INT)`, true},
		{"grant", `GRANT SELECT ON users TO admin`, true},
		{"revoke", `REVOKE SELECT ON users FROM admin`, true},
		{"merge", `MERGE INTO target USING source ON x WHEN MATCHED THEN UPDATE SET y=1`, true},
		{"call", `CALL refresh_materialized_view('x')`, true},
		{"exec", `EXEC sp_helpdb`, true},
		{"copy", `COPY users FROM '/tmp/u.csv'`, true},
		{"vacuum", `VACUUM ANALYZE users`, true},
		{"commit", `COMMIT`, true},

		// Denied — multi-statement
		{"two selects", `SELECT 1; SELECT 2`, true},
		{"select then drop", `SELECT 1; DROP TABLE users`, true},

		// Denied — sneaky comment-evasion (we strip comments before checking)
		{"comment-hidden drop", `SELECT 1 /* malicious */; DROP TABLE users`, true},
		{"line-comment evasion", "SELECT 1 --; DROP TABLE users\n; INSERT INTO x VALUES (1)", true},

		// Denied — pure comment / empty
		{"empty after stripping", `-- nothing here`, true},
		{"only block comment", `/* anything */`, true},

		// Denied — non-select first token
		{"explain", `EXPLAIN SELECT 1`, true},
		{"set", `SET search_path = public`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSelectOnly(tc.sql)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.sql)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected pass for %q, got: %v", tc.sql, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrUnsafeSQL) {
				t.Errorf("error should wrap ErrUnsafeSQL, got: %v", err)
			}
		})
	}
}
