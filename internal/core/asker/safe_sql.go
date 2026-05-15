package asker

import (
	"errors"
	"strings"
	"unicode"
)

// ErrUnsafeSQL is returned when ValidateSelectOnly rejects a query.
// Wrapped errors carry the specific reason (multi-statement, DML
// keyword, etc.) — Errors.Is(err, ErrUnsafeSQL) holds in all cases.
var ErrUnsafeSQL = errors.New("asker: generated SQL is not safe to execute")

// ValidateSelectOnly is the gatekeeper between generated SQL and the
// warehouse. The rules are intentionally strict — false positives are
// preferable to false negatives. The LLM is asked for read-only
// SELECTs; anything else is either prompt drift or an attempted
// injection.
//
// Rules:
//
//  1. Strip --line and /* block */ comments.
//  2. Reject if more than one statement remains (one trailing semicolon
//     is fine; we strip it before counting).
//  3. The first significant token must be SELECT or WITH (CTE).
//  4. No write or DDL verbs anywhere outside string literals:
//     INSERT, UPDATE, DELETE, MERGE, DROP, TRUNCATE, ALTER, CREATE,
//     GRANT, REVOKE, CALL, EXECUTE, COPY, LOAD, ATTACH, VACUUM,
//     ANALYZE-only-with-side-effects, COMMIT, ROLLBACK.
//
// The tokenizer is hand-rolled — small and auditable beats pulling in
// a SQL parser. Strings are skipped so a literal "DELETE" inside a
// WHERE clause won't trigger.
func ValidateSelectOnly(sql string) error {
	stripped := stripSQLComments(sql)
	stripped = strings.TrimSpace(stripped)
	stripped = strings.TrimRight(stripped, ";")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return errSafe("empty SQL after stripping comments")
	}

	// Tokenise; skip strings so blocked keywords inside text literals
	// don't false-positive.
	tokens, hasMultipleStatements := tokenize(stripped)
	if hasMultipleStatements {
		return errSafe("multiple statements not allowed")
	}
	if len(tokens) == 0 {
		return errSafe("no tokens")
	}
	first := strings.ToUpper(tokens[0])
	if first != "SELECT" && first != "WITH" {
		return errSafe("first statement must be SELECT or WITH (got " + first + ")")
	}
	for _, t := range tokens {
		if blockedKeyword(t) {
			return errSafe("forbidden keyword: " + strings.ToUpper(t))
		}
	}
	return nil
}

// stripSQLComments removes --line and /* block */ comments. Quoting
// rules: a -- inside a string literal is not a comment.
func stripSQLComments(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))
	i := 0
	for i < len(sql) {
		c := sql[i]
		// String literals — single quote. Doubled '' inside is an
		// escaped quote per SQL standard.
		if c == '\'' {
			out.WriteByte(c)
			i++
			for i < len(sql) {
				out.WriteByte(sql[i])
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						out.WriteByte(sql[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		// Identifier quotes — "..." (Postgres/Snowflake) and `...`
		// (MySQL). Pass through unchanged.
		if c == '"' || c == '`' {
			delim := c
			out.WriteByte(c)
			i++
			for i < len(sql) && sql[i] != delim {
				out.WriteByte(sql[i])
				i++
			}
			if i < len(sql) {
				out.WriteByte(sql[i])
				i++
			}
			continue
		}
		// --line comment
		if c == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
		// /* block */ comment
		if c == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 < len(sql) {
				i += 2
			}
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

// tokenize returns the list of identifier-shaped tokens AND whether
// more than one statement exists. Identifier-shape means a maximal run
// of [a-zA-Z0-9_]; punctuation and string literals are skipped (so a
// blocked keyword inside 'a string with DELETE' is invisible to the
// caller).
func tokenize(sql string) ([]string, bool) {
	var tokens []string
	statements := 1
	i := 0
	for i < len(sql) {
		c := sql[i]
		switch {
		case c == '\'':
			// Skip string literal
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '"' || c == '`':
			// Skip quoted identifier (keep AS-IS in source; doesn't
			// contribute a token because the contents could be
			// arbitrary user-named columns).
			delim := c
			i++
			for i < len(sql) && sql[i] != delim {
				i++
			}
			if i < len(sql) {
				i++
			}
		case c == ';':
			i++
			// Skip whitespace
			for i < len(sql) && unicode.IsSpace(rune(sql[i])) {
				i++
			}
			if i < len(sql) {
				statements++
			}
		case isIdentChar(c):
			start := i
			for i < len(sql) && isIdentChar(sql[i]) {
				i++
			}
			tokens = append(tokens, sql[start:i])
		default:
			i++
		}
	}
	return tokens, statements > 1
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}

// blockedKeyword: the kill-list. Compared case-insensitively. EXEC,
// CALL, COPY, ATTACH, VACUUM, ANALYZE are read-ish in some dialects
// but can still mutate state (ANALYZE writes stats; COPY can write
// files); blocking them is conservative-but-safe.
func blockedKeyword(t string) bool {
	switch strings.ToUpper(t) {
	case "INSERT", "UPDATE", "DELETE", "MERGE",
		"DROP", "TRUNCATE", "ALTER", "CREATE",
		"GRANT", "REVOKE",
		"CALL", "EXEC", "EXECUTE",
		"COPY", "LOAD", "ATTACH", "DETACH",
		"VACUUM", "REINDEX", "CLUSTER",
		"COMMIT", "ROLLBACK", "BEGIN", "START", "SAVEPOINT":
		return true
	}
	return false
}

func errSafe(reason string) error {
	// errors.Join keeps Errors.Is(err, ErrUnsafeSQL) true while letting
	// the API surface the reason.
	return errors.Join(ErrUnsafeSQL, errors.New(reason))
}
