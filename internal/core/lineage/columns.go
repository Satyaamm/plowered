package lineage

import (
	"context"
	"regexp"
	"strings"
)

// ColumnSink is what the transform_run executor calls to persist column
// edges produced from a task's SQL. The Postgres-backed implementation
// lives in internal/storage/postgres; tests can substitute an in-memory
// recorder to assert on extracted edges.
type ColumnSink interface {
	Upsert(ctx context.Context, tenantID, taskRunID string, edges []ColumnEdge) (written, misses int, err error)
}

// ColumnEdge is one source-column → target-column dependency extracted from
// a SQL projection list. Transform names the kind of derivation:
//
//	"identity"   — direct passthrough (e.g. SELECT a, t.b ...)
//	"expression" — computed from one or more upstream columns
//	"wildcard"   — projection used `*` or `t.*`; emit a single edge with
//	               source/target column "*". Callers can expand later.
//
// Expression carries the originating SQL fragment for traceability — the
// column-lineage UI can show it on hover.
type ColumnEdge struct {
	SourceTable  string
	SourceColumn string
	TargetTable  string
	TargetColumn string
	Transform    string
	Expression   string
}

// columnSource is a FROM/JOIN reference, with whatever alias the projection
// will use to disambiguate columns ("t" in `SELECT t.a FROM users t`).
type columnSource struct {
	Alias string
	Name  string
}

// ExtractColumns parses a SELECT and emits one ColumnEdge per (source,
// target) column pair. The function is best-effort — anything outside its
// supported syntax (CTEs, UNION, subquery FROM clauses) degrades to a
// single wildcard edge per detected source so lineage is never silently
// empty for queries the parser can't handle. See the package doc for
// the grammar that's actually supported.
//
// targetTable is stamped onto every edge. When unknown, callers may pass
// "" — downstream stores typically need a real value, so the upstream
// caller (transform_run executor) supplies it from the task config.
func ExtractColumns(sql, targetTable string) []ColumnEdge {
	clean := whitespaceCollapse(stripComments(sql))
	if !strings.Contains(strings.ToUpper(clean), "SELECT") {
		return nil
	}
	sources := extractColumnSources(clean)
	projection := extractProjection(clean)
	if projection == "" {
		return columnWildcardEdges(sources, targetTable)
	}
	items := splitTopLevelCommas(projection)
	if len(items) == 0 {
		return columnWildcardEdges(sources, targetTable)
	}
	out := make([]ColumnEdge, 0, len(items))
	for _, raw := range items {
		expr, alias := splitProjectionItem(raw)
		// Wildcards keep lineage discoverable without expanding columns.
		if expr == "*" || strings.HasSuffix(expr, ".*") {
			for _, s := range sources {
				out = append(out, ColumnEdge{
					SourceTable: s.Name, SourceColumn: "*",
					TargetTable: targetTable, TargetColumn: "*",
					Transform: "wildcard", Expression: expr,
				})
			}
			continue
		}
		// `tab.col` direct passthrough.
		if q := simpleQualifiedColumn(expr); q != nil {
			out = append(out, ColumnEdge{
				SourceTable: resolveColumnAlias(q[0], sources),
				SourceColumn: q[1],
				TargetTable: targetTable,
				TargetColumn: alias,
				Transform: "identity",
			})
			continue
		}
		// Plain `col` — ambiguous unless there's exactly one source.
		if isBareIdent(expr) {
			src := ""
			if len(sources) == 1 {
				src = sources[0].Name
			}
			out = append(out, ColumnEdge{
				SourceTable: src, SourceColumn: expr,
				TargetTable: targetTable, TargetColumn: alias,
				Transform: "identity",
			})
			continue
		}
		// Anything else: expression. Collect every column reference inside.
		refs := extractColumnExprRefs(expr, sources)
		if len(refs) == 0 {
			// SELECT 1 AS one — keep the target column visible.
			out = append(out, ColumnEdge{
				TargetTable: targetTable, TargetColumn: alias,
				Transform: "expression", Expression: expr,
			})
			continue
		}
		for _, ref := range refs {
			out = append(out, ColumnEdge{
				SourceTable: ref.table, SourceColumn: ref.column,
				TargetTable: targetTable, TargetColumn: alias,
				Transform: "expression", Expression: expr,
			})
		}
	}
	return out
}

func columnWildcardEdges(sources []columnSource, target string) []ColumnEdge {
	if len(sources) == 0 {
		return nil
	}
	out := make([]ColumnEdge, 0, len(sources))
	for _, s := range sources {
		out = append(out, ColumnEdge{
			SourceTable: s.Name, SourceColumn: "*",
			TargetTable: target, TargetColumn: "*",
			Transform: "wildcard",
		})
	}
	return out
}

// --- helpers (file-local; the existing parser.go owns the higher-level
// statement parser) ---

var (
	colWhitespaceRE = regexp.MustCompile(`\s+`)
	colIdentRE      = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	colQualifiedRE  = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)$`)
	colRefRE        = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)`)
	colBareRefRE    = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\b`)
)

func whitespaceCollapse(s string) string { return colWhitespaceRE.ReplaceAllString(s, " ") }
func isBareIdent(s string) bool          { return colIdentRE.MatchString(s) }

func simpleQualifiedColumn(s string) []string {
	m := colQualifiedRE.FindStringSubmatch(s)
	if len(m) == 3 {
		return []string{m[1], m[2]}
	}
	return nil
}

// extractProjection returns the substring between the first SELECT and the
// next top-level FROM. Parentheses are tracked so subqueries don't trick
// the matcher into stopping early.
func extractProjection(s string) string {
	upper := strings.ToUpper(s)
	selectIdx := strings.Index(upper, "SELECT")
	if selectIdx < 0 {
		return ""
	}
	rest := strings.TrimSpace(s[selectIdx+len("SELECT"):])
	upperRest := strings.ToUpper(rest)
	for _, kw := range []string{"DISTINCT ", "ALL "} {
		if strings.HasPrefix(upperRest, kw) {
			rest = strings.TrimSpace(rest[len(kw):])
			break
		}
	}
	depth := 0
	for i := 0; i+5 <= len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.EqualFold(rest[i:i+5], " FROM") {
			return strings.TrimSpace(rest[:i])
		}
	}
	return strings.TrimSpace(rest)
}

// splitTopLevelCommas splits on commas while respecting parentheses, so
// "coalesce(a, b)" doesn't get split.
func splitTopLevelCommas(p string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(p[start:i]))
				start = i + 1
			}
		}
	}
	if start < len(p) {
		if tail := strings.TrimSpace(p[start:]); tail != "" {
			out = append(out, tail)
		}
	}
	return out
}

// splitProjectionItem separates `expr AS alias` (or a trailing alias with no
// AS) into its parts. When no alias is present, the alias is the trailing
// identifier of expr.
func splitProjectionItem(item string) (expr, alias string) {
	upper := strings.ToUpper(item)
	if idx := strings.LastIndex(upper, " AS "); idx >= 0 {
		return strings.TrimSpace(item[:idx]), strings.TrimSpace(item[idx+4:])
	}
	parts := strings.Fields(item)
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		if isBareIdent(last) && !columnIsKeyword(last) && depthZeroAt(item, last) {
			return strings.TrimSpace(item[:strings.LastIndex(item, last)]), last
		}
	}
	expr = strings.TrimSpace(item)
	if q := simpleQualifiedColumn(expr); q != nil {
		return expr, q[1]
	}
	if isBareIdent(expr) {
		return expr, expr
	}
	return expr, slugify(expr)
}

func depthZeroAt(item, last string) bool {
	idx := strings.LastIndex(item, last)
	if idx < 0 {
		return false
	}
	depth := 0
	for i := 0; i < idx; i++ {
		switch item[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth == 0
}

func slugify(expr string) string {
	out := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, expr)
	out = strings.Trim(out, "_")
	if len(out) > 24 {
		out = out[:24]
	}
	if out == "" {
		return "expr"
	}
	return out
}

func columnIsKeyword(s string) bool {
	switch strings.ToUpper(s) {
	case "FROM", "WHERE", "GROUP", "ORDER", "LIMIT", "JOIN", "ON", "AND", "OR",
		"IS", "NULL", "NOT", "IN", "AS", "DISTINCT", "ALL", "BY", "HAVING",
		"UNION", "INTERSECT", "EXCEPT", "INNER", "OUTER", "LEFT", "RIGHT", "FULL", "CROSS":
		return true
	}
	return false
}

func extractColumnSources(s string) []columnSource {
	upper := strings.ToUpper(s)
	fromIdx := strings.Index(upper, " FROM ")
	if fromIdx < 0 {
		return nil
	}
	rest := s[fromIdx+len(" FROM "):]
	stopUpper := strings.ToUpper(rest)
	end := len(rest)
	for _, st := range []string{" WHERE ", " GROUP ", " ORDER ", " LIMIT ", " HAVING "} {
		if i := strings.Index(stopUpper, st); i >= 0 && i < end {
			end = i
		}
	}
	clause := rest[:end]
	upperClause := strings.ToUpper(clause)
	for _, jk := range []string{
		" LEFT OUTER JOIN ", " RIGHT OUTER JOIN ", " FULL OUTER JOIN ",
		" LEFT JOIN ", " RIGHT JOIN ", " FULL JOIN ",
		" INNER JOIN ", " CROSS JOIN ", " JOIN ",
	} {
		for {
			i := strings.Index(upperClause, jk)
			if i < 0 {
				break
			}
			clause = clause[:i] + " , " + clause[i+len(jk):]
			upperClause = strings.ToUpper(clause)
		}
	}
	for {
		i := strings.Index(strings.ToUpper(clause), " ON ")
		if i < 0 {
			break
		}
		next := strings.Index(clause[i+4:], ",")
		if next < 0 {
			clause = clause[:i]
			break
		}
		clause = clause[:i] + clause[i+4+next:]
	}
	parts := strings.Split(clause, ",")
	var out []columnSource
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "(") {
			continue
		}
		fields := strings.Fields(p)
		switch len(fields) {
		case 1:
			out = append(out, columnSource{Alias: fields[0], Name: fields[0]})
		case 2:
			out = append(out, columnSource{Alias: fields[1], Name: fields[0]})
		case 3:
			if strings.EqualFold(fields[1], "AS") {
				out = append(out, columnSource{Alias: fields[2], Name: fields[0]})
			} else {
				out = append(out, columnSource{Alias: fields[1], Name: fields[0]})
			}
		default:
			out = append(out, columnSource{Alias: fields[len(fields)-1], Name: fields[0]})
		}
	}
	return out
}

func resolveColumnAlias(alias string, sources []columnSource) string {
	for _, s := range sources {
		if s.Alias == alias || s.Name == alias {
			return s.Name
		}
	}
	return alias
}

type colExprRef struct{ table, column string }

func extractColumnExprRefs(expr string, sources []columnSource) []colExprRef {
	seen := map[string]struct{}{}
	out := []colExprRef{}
	for _, m := range colRefRE.FindAllStringSubmatch(expr, -1) {
		key := m[1] + "." + m[2]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, colExprRef{table: resolveColumnAlias(m[1], sources), column: m[2]})
	}
	if len(out) > 0 {
		return out
	}
	for _, m := range colBareRefRE.FindAllStringSubmatchIndex(expr, -1) {
		token := expr[m[0]:m[1]]
		if columnIsKeyword(token) {
			continue
		}
		next := m[1]
		for next < len(expr) && expr[next] == ' ' {
			next++
		}
		if next < len(expr) && expr[next] == '(' {
			continue
		}
		if token[0] >= '0' && token[0] <= '9' {
			continue
		}
		key := ":" + token
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		src := ""
		if len(sources) == 1 {
			src = sources[0].Name
		}
		out = append(out, colExprRef{table: src, column: token})
	}
	return out
}
