// Package lineage parses SQL into source→target dependency edges.
//
// The parser is dialect-agnostic for the cases it handles and uses a
// token-aware scanner — not just regex — so identifiers, comments, and
// string literals are respected. Coverage in v0:
//
//	INSERT INTO <t> ...               (with subqueries)
//	CREATE [OR REPLACE] [TEMP] TABLE <t> AS SELECT ... FROM <s>
//	CREATE [OR REPLACE] [MATERIALIZED] VIEW <t> AS SELECT ... FROM <s>
//	UPDATE <t> SET ... [FROM <s>]
//	MERGE INTO <t> USING <s> ...
//	WITH ... INSERT/CREATE-AS  (CTEs are scanned for source names)
//
// Out of scope (v0): correlated subqueries that resolve to dynamic targets,
// dialect-specific features, table-valued function calls, dynamic SQL.
package lineage

import (
	"strings"
	"unicode"
)

// Op identifies the kind of SQL statement that produced an edge.
type Op string

const (
	OpInsert        Op = "insert"
	OpCreateTableAs Op = "create_table_as"
	OpCreateView    Op = "create_view"
	OpUpdate        Op = "update"
	OpMerge         Op = "merge"
)

// Statement is one parsed SQL statement with a single target and zero-or-more
// sources. Identifiers are returned lowercased and schema-qualified where the
// SQL provided a qualifier; otherwise just the table name.
type Statement struct {
	Op      Op
	Target  string
	Sources []string
	Raw     string // original SQL, trimmed
}

// Parse splits sql on top-level semicolons and emits one Statement per
// identifiable transformation. Statements that do not match a known target
// pattern are skipped silently.
func Parse(sql string) []Statement {
	var out []Statement
	for _, raw := range splitStatements(sql) {
		stripped := stripComments(raw)
		if !looksLikeTransformation(stripped) {
			continue
		}
		s := parseOne(stripped)
		if s == nil {
			continue
		}
		s.Raw = strings.TrimSpace(raw)
		out = append(out, *s)
	}
	return out
}

// parseOne extracts the first transformation statement from already-stripped
// SQL. Returns nil if no target can be identified.
func parseOne(sql string) *Statement {
	tokens := tokenize(sql)
	if len(tokens) == 0 {
		return nil
	}
	switch first := strings.ToLower(tokens[0]); first {
	case "with":
		return parseWith(tokens)
	case "insert":
		return parseInsert(tokens)
	case "create":
		return parseCreate(tokens)
	case "update":
		return parseUpdate(tokens)
	case "merge":
		return parseMerge(tokens)
	}
	return nil
}

// parseWith handles `WITH cte_name AS (...), ... INSERT/CREATE-AS ...`.
// It scans through the CTE definitions, collecting their source names, and
// then parses the trailing INSERT/CREATE statement, merging the CTE-derived
// sources in.
func parseWith(tokens []string) *Statement {
	cteSources := collectCTESources(tokens)

	for i := 0; i < len(tokens); i++ {
		w := strings.ToLower(tokens[i])
		switch w {
		case "insert", "create", "update", "merge":
			tail := tokens[i:]
			s := parseOne(strings.Join(tail, " "))
			if s == nil {
				return nil
			}
			s.Sources = mergeUnique(s.Sources, cteSources)
			return s
		}
	}
	return nil
}

func parseInsert(tokens []string) *Statement {
	if len(tokens) < 3 || strings.ToLower(tokens[1]) != "into" {
		return nil
	}
	target, after := readQualifiedName(tokens[2:])
	if target == "" {
		return nil
	}
	return &Statement{
		Op:      OpInsert,
		Target:  target,
		Sources: collectSourceTables(after),
	}
}

func parseCreate(tokens []string) *Statement {
	// skip optional OR REPLACE, TEMP, GLOBAL TEMPORARY, MATERIALIZED, IF NOT EXISTS
	idx := 1
	skipKeywords := map[string]bool{
		"or": true, "replace": true,
		"temp": true, "temporary": true, "global": true,
		"local": true, "unlogged": true, "materialized": true,
		"if": true, "not": true, "exists": true,
	}
	for idx < len(tokens) {
		w := strings.ToLower(tokens[idx])
		if skipKeywords[w] {
			idx++
			continue
		}
		break
	}
	if idx >= len(tokens) {
		return nil
	}
	kind := strings.ToLower(tokens[idx])
	idx++
	if idx >= len(tokens) {
		return nil
	}
	target, after := readQualifiedName(tokens[idx:])
	if target == "" {
		return nil
	}
	switch kind {
	case "table":
		// must be CREATE TABLE ... AS SELECT to count as transformation
		if !containsKeyword(after, "as") {
			return nil
		}
		return &Statement{
			Op:      OpCreateTableAs,
			Target:  target,
			Sources: collectSourceTables(after),
		}
	case "view":
		return &Statement{
			Op:      OpCreateView,
			Target:  target,
			Sources: collectSourceTables(after),
		}
	}
	return nil
}

func parseUpdate(tokens []string) *Statement {
	if len(tokens) < 2 {
		return nil
	}
	target, after := readQualifiedName(tokens[1:])
	if target == "" {
		return nil
	}
	return &Statement{
		Op:      OpUpdate,
		Target:  target,
		Sources: collectSourceTables(after),
	}
}

func parseMerge(tokens []string) *Statement {
	if len(tokens) < 3 || strings.ToLower(tokens[1]) != "into" {
		return nil
	}
	target, after := readQualifiedName(tokens[2:])
	if target == "" {
		return nil
	}
	return &Statement{
		Op:      OpMerge,
		Target:  target,
		Sources: collectSourceTables(after),
	}
}

// collectSourceTables walks tokens and returns the set of source table names
// that appear after FROM, JOIN, or USING keywords.
func collectSourceTables(tokens []string) []string {
	seen := make(map[string]bool)
	var out []string
	i := 0
	for i < len(tokens) {
		w := strings.ToLower(tokens[i])
		switch w {
		case "from", "join", "using":
			name, advance := readQualifiedName(tokens[i+1:])
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			i = i + 1 + advance
		default:
			i++
		}
	}
	return out
}

// collectCTESources walks `WITH name AS (subquery), ...` and returns source
// tables referenced inside CTE bodies.
func collectCTESources(tokens []string) []string {
	depth := 0
	var inside []string
	started := false
	for _, t := range tokens {
		switch t {
		case "(":
			depth++
			started = true
			continue
		case ")":
			depth--
			continue
		}
		if started && depth > 0 {
			inside = append(inside, t)
		}
	}
	return collectSourceTables(inside)
}

// readQualifiedName reads tokens[0] (and possibly more) as a possibly-quoted,
// possibly schema-qualified table name. Returns the canonical lowercase name
// and the number of tokens consumed.
func readQualifiedName(tokens []string) (name string, advance int) {
	if len(tokens) == 0 {
		return "", 0
	}
	first := stripQuotes(tokens[0])
	if !isIdentifier(first) {
		return "", 0
	}
	name = strings.ToLower(first)
	advance = 1
	// schema.table or db.schema.table
	for advance+1 < len(tokens) && tokens[advance] == "." {
		next := stripQuotes(tokens[advance+1])
		if !isIdentifier(next) {
			break
		}
		name = name + "." + strings.ToLower(next)
		advance += 2
	}
	return name, advance
}

// ----- lex helpers -----

// tokenize produces a flat list of identifier / keyword / punctuation tokens.
// Whitespace is dropped. Quoted identifiers ("foo") and string literals
// ('text') are preserved as single tokens.
func tokenize(sql string) []string {
	var tokens []string
	runes := []rune(sql)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '\'':
			// string literal — skip to closing quote, dropping content
			j := i + 1
			for j < len(runes) && runes[j] != '\'' {
				j++
			}
			if j < len(runes) {
				j++
			}
			i = j
		case r == '"':
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				j++
			}
			if j < len(runes) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		case r == '(' || r == ')' || r == ',' || r == ';' || r == '.':
			tokens = append(tokens, string(r))
			i++
		case isIdentRune(r):
			j := i
			for j < len(runes) && isIdentRune(runes[j]) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		default:
			i++ // skip unrecognized punctuation
		}
	}
	return tokens
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && !(unicode.IsLetter(r) || r == '_') {
			return false
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return !isReservedKeyword(s)
}

func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// stripComments removes -- line comments and /* block comments */.
func stripComments(sql string) string {
	var out strings.Builder
	runes := []rune(sql)
	i := 0
	for i < len(runes) {
		switch {
		case i+1 < len(runes) && runes[i] == '-' && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case i+1 < len(runes) && runes[i] == '/' && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i += 2
		default:
			out.WriteRune(runes[i])
			i++
		}
	}
	return out.String()
}

// splitStatements splits on top-level `;`, ignoring those inside string
// literals or quoted identifiers.
func splitStatements(sql string) []string {
	var out []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	for _, r := range sql {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		}
		if r == ';' && !inSingle && !inDouble {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				out = append(out, s)
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func looksLikeTransformation(sql string) bool {
	low := strings.ToLower(sql)
	for _, kw := range []string{"insert ", "create ", "update ", "merge ", "with "} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func containsKeyword(tokens []string, kw string) bool {
	kw = strings.ToLower(kw)
	for _, t := range tokens {
		if strings.ToLower(t) == kw {
			return true
		}
	}
	return false
}

func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, x := range append(a, b...) {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// isReservedKeyword filters out SQL reserved words that should never be
// treated as table names by readQualifiedName.
func isReservedKeyword(s string) bool {
	_, ok := reservedKeywords[strings.ToLower(s)]
	return ok
}

var reservedKeywords = map[string]struct{}{
	"select": {}, "from": {}, "where": {}, "group": {}, "by": {}, "having": {},
	"order": {}, "limit": {}, "offset": {}, "join": {}, "inner": {}, "outer": {},
	"left": {}, "right": {}, "full": {}, "cross": {}, "on": {}, "using": {},
	"and": {}, "or": {}, "not": {}, "in": {}, "exists": {}, "between": {},
	"like": {}, "is": {}, "null": {}, "true": {}, "false": {},
	"insert": {}, "into": {}, "values": {}, "update": {}, "set": {},
	"delete": {}, "merge": {}, "create": {}, "table": {}, "view": {}, "as": {},
	"with": {}, "case": {}, "when": {}, "then": {}, "else": {}, "end": {},
	"union": {}, "intersect": {}, "except": {}, "all": {}, "distinct": {},
}
