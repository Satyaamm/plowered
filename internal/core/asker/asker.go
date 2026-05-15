// Package asker is the Text-to-SQL surface. Given a natural-language
// question and a target connection, it:
//
//  1. Uses semantic search to find the K most relevant tables in the
//     catalog (scoped to that connection).
//  2. Assembles their schemas via aictx into a prompt.
//  3. Calls the tenant's primary chat provider to generate a SELECT.
//  4. Validates the SQL is read-only (see safe_sql.go).
//  5. Persists the generation as an execution row (status='generated').
//
// Execution is a SEPARATE step (Run). The user must click "Run" after
// reviewing the SQL — we never auto-execute LLM-produced queries
// against customer warehouses. This is a non-negotiable safety
// boundary: a model that hallucinates a 9-hour join shouldn't crash
// the customer's prod warehouse without human review.
package asker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/aictx"
	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/warehouse"
	"github.com/Satyaamm/plowered/pkg/llm"
)

// ErrNoProvider mirrors aiprovider.ErrNoPrimary so handlers can pattern
// match on a single asker-package symbol.
var ErrNoProvider = aiprovider.ErrNoPrimary

// SemanticSearcher finds the top-K assets matching a free-text query.
// We accept the existing search.Searcher behind this interface so
// asker doesn't take a hard dep on the search package's full surface.
type SemanticSearcher interface {
	TopTables(ctx context.Context, tenantID, connectionID, question string, k int) ([]string, error)
}

// ConnectionReader returns enough about a connection to pick a SQL
// dialect for the prompt and to dispatch the executor on Run.
type ConnectionReader interface {
	Get(ctx context.Context, tenantID, connectionID string) (*connection.Connection, error)
}

// Log persists execution rows for audit + history. RecordGenerated
// receives the full call context (question + connection) explicitly
// rather than via ctx values — keeps the interface honest about what
// it needs.
type Log interface {
	RecordGenerated(ctx context.Context, params RecordGeneratedParams) error
	GetExecution(ctx context.Context, tenantID, executionID string) (*Execution, error)
	RecordExecuted(ctx context.Context, tenantID, executionID string, rowCount int, elapsedMs int64, errStr string) error
}

// RecordGeneratedParams is the explicit input to Log.RecordGenerated.
// All fields are required except GeneratedBy (which may be empty for
// system-initiated calls).
type RecordGeneratedParams struct {
	Generation   *Generation
	TenantID     string
	ConnectionID string
	Question     string
	GeneratedBy  string
}

// Generation is the result of Ask — generated SQL plus its audit
// metadata. It's the wire payload the UI uses to render the SQL
// preview before the user opts in to Run.
type Generation struct {
	ExecutionID  string   `json:"execution_id"`
	GeneratedSQL string   `json:"generated_sql"`
	Tables       []string `json:"tables_used"` // qualified names that fed the prompt
	Model        string   `json:"model"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
}

// Execution is the persisted row, returned for history views.
type Execution struct {
	ID            string
	TenantID      string
	ConnectionID  string
	Question      string
	GeneratedSQL  string
	Model         string
	Status        string
	GeneratedAt   time.Time
	ExecutedAt    *time.Time
	RowCount      *int
	ElapsedMs     *int64
	Error         *string
}

// RunResult is the output of Run — the table the warehouse returned.
// Rows are capped at MaxRows; oversized result sets get truncated
// with Truncated=true so the UI can warn.
type RunResult struct {
	Columns   []string  `json:"columns"`
	Rows      [][]any   `json:"rows"`
	RowCount  int       `json:"row_count"`
	Truncated bool      `json:"truncated"`
	ElapsedMs int64     `json:"elapsed_ms"`
}

// Service is the only exported type the HTTP layer should hold.
type Service struct {
	Context   *aictx.Builder
	Resolver  *aiprovider.Resolver
	Search    SemanticSearcher
	Conns     ConnectionReader
	Warehouse *warehouse.MultiFactory
	Log       Log
	Logger    *slog.Logger

	// TopK is how many tables we feed into the prompt as schema
	// context. Default 5 — enough breadth for most questions without
	// blowing the context window.
	TopK int
	// MaxOutputTokens caps generated SQL length. 600 tokens ≈ a
	// reasonable JOIN-heavy SELECT.
	MaxOutputTokens int
	// MaxRows caps rows returned from Run. 1000 is the wire ceiling;
	// the UI paginates above that.
	MaxRows int
}

// Ask is the read-only generation step. It produces SQL + persists a
// Generation row but does NOT execute against the warehouse.
func (s *Service) Ask(ctx context.Context, tenantID, connectionID, question, generatedBy string) (*Generation, error) {
	if s.Context == nil || s.Resolver == nil || s.Search == nil || s.Conns == nil || s.Log == nil {
		return nil, errors.New("asker: service not fully configured")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, errors.New("asker: question is empty")
	}

	conn, err := s.Conns.Get(ctx, tenantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("load connection: %w", err)
	}
	if !conn.Type.IsSQL() {
		return nil, fmt.Errorf("asker: connection type %q does not support SQL", conn.Type)
	}

	topK := s.TopK
	if topK <= 0 {
		topK = 5
	}
	tableIDs, err := s.Search.TopTables(ctx, tenantID, connectionID, question, topK)
	if err != nil {
		return nil, fmt.Errorf("top tables: %w", err)
	}
	if len(tableIDs) == 0 {
		return nil, errors.New("asker: no relevant tables found in this connection")
	}

	tableNames := make([]string, 0, len(tableIDs))
	var schemas strings.Builder
	for _, id := range tableIDs {
		tctx, cerr := s.Context.BuildForTable(ctx, tenantID, id)
		if cerr != nil {
			s.logger().WarnContext(ctx, "asker: build context", "asset", id, "err", cerr)
			continue
		}
		tableNames = append(tableNames, qualify(tctx.Schema, tctx.Table))
		schemas.WriteString(tctx.Render())
		schemas.WriteString("\n")
	}
	if schemas.Len() == 0 {
		return nil, errors.New("asker: could not build any schema context")
	}

	provider, err := s.Resolver.Primary(ctx, tenantID, aiprovider.CapChat)
	if err != nil {
		return nil, err
	}
	dialect := dialectNameFor(conn.Type)
	maxTok := s.MaxOutputTokens
	if maxTok <= 0 {
		maxTok = 600
	}
	resp, err := provider.Generate(ctx, llm.GenerateRequest{
		System:      buildSystemPrompt(dialect),
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: buildUserPrompt(schemas.String(), question)}},
		MaxTokens:   maxTok,
		Temperature: 0.0,
	})
	if err != nil {
		return nil, fmt.Errorf("llm generate: %w", err)
	}
	sql := extractSQL(resp.Content)
	if err := ValidateSelectOnly(sql); err != nil {
		// Persist a failed generation so the user sees what the model
		// produced and why it was rejected — UI shows the SQL + the
		// rejection reason.
		failed := &Generation{
			GeneratedSQL: sql,
			Tables:       tableNames,
			Model:        resp.Model,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
		}
		s.logger().WarnContext(ctx, "asker: unsafe SQL rejected", "sql", sql, "err", err)
		_ = s.Log.RecordGenerated(ctx, RecordGeneratedParams{
			Generation: failed, TenantID: tenantID,
			ConnectionID: connectionID, Question: question, GeneratedBy: generatedBy,
		})
		return nil, fmt.Errorf("%w: %v", ErrUnsafeSQL, err)
	}

	gen := &Generation{
		GeneratedSQL: sql,
		Tables:       tableNames,
		Model:        resp.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
	}
	if err := s.Log.RecordGenerated(ctx, RecordGeneratedParams{
		Generation: gen, TenantID: tenantID,
		ConnectionID: connectionID, Question: question, GeneratedBy: generatedBy,
	}); err != nil {
		return nil, fmt.Errorf("record generation: %w", err)
	}
	// The Log impl sets gen.ExecutionID on the in-place struct as it
	// inserts the row. Callers get it back on the response.
	return gen, nil
}

// Run executes a previously-generated SQL via the warehouse. Re-runs
// the safety validator before each execution (defence in depth — the
// generation row could be tampered with between Ask and Run).
func (s *Service) Run(ctx context.Context, tenantID, executionID string) (*RunResult, error) {
	if s.Log == nil || s.Warehouse == nil {
		return nil, errors.New("asker: service not fully configured")
	}
	exec, err := s.Log.GetExecution(ctx, tenantID, executionID)
	if err != nil {
		return nil, fmt.Errorf("load execution: %w", err)
	}
	if err := ValidateSelectOnly(exec.GeneratedSQL); err != nil {
		return nil, err
	}
	maxRows := s.MaxRows
	if maxRows <= 0 {
		maxRows = 1000
	}
	executor, err := s.Warehouse.Open(ctx, tenantID, exec.ConnectionID)
	if err != nil {
		return nil, fmt.Errorf("open warehouse: %w", err)
	}
	start := time.Now()
	rows, err := executor.Query(ctx, exec.GeneratedSQL)
	if err != nil {
		elapsed := time.Since(start).Milliseconds()
		_ = s.Log.RecordExecuted(ctx, tenantID, executionID, 0, elapsed, err.Error())
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	cols := rows.Columns()
	out := &RunResult{Columns: cols, Rows: make([][]any, 0, 100)}
	scanDest := make([]any, len(cols))
	scanPtrs := make([]any, len(cols))
	for i := range scanDest {
		scanPtrs[i] = &scanDest[i]
	}
	for rows.Next() {
		if len(out.Rows) >= maxRows {
			out.Truncated = true
			break
		}
		if err := rows.Scan(scanPtrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		// Copy into a fresh slice — scanDest is reused per row.
		row := make([]any, len(scanDest))
		copy(row, scanDest)
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	out.RowCount = len(out.Rows)
	out.ElapsedMs = time.Since(start).Milliseconds()
	if err := s.Log.RecordExecuted(ctx, tenantID, executionID, out.RowCount, out.ElapsedMs, ""); err != nil {
		s.logger().WarnContext(ctx, "asker: record executed", "err", err)
	}
	return out, nil
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// dialectNameFor maps connection.Type to a human-readable SQL dialect
// name the prompt instructs the model to target. The model already
// knows these dialects; we just need to say which one.
func dialectNameFor(t connection.Type) string {
	switch t {
	case connection.TypePostgres:
		return "PostgreSQL"
	case connection.TypeSnowflake:
		return "Snowflake SQL"
	case connection.TypeMySQL:
		return "MySQL"
	case connection.TypeRedshift:
		return "Amazon Redshift SQL"
	case connection.TypeBigQuery:
		return "BigQuery Standard SQL"
	case connection.TypeAthena:
		return "Amazon Athena (Presto) SQL"
	case connection.TypeDatabricks:
		return "Databricks Spark SQL"
	default:
		return "Standard SQL"
	}
}

func buildSystemPrompt(dialect string) string {
	return "You are a careful data analyst who writes precise " + dialect + ` SELECT statements.

Rules:
- ALWAYS produce a single SELECT statement (or WITH ... SELECT). Never INSERT, UPDATE, DELETE, DROP, ALTER, CREATE, TRUNCATE, GRANT, REVOKE, MERGE.
- Use only the columns + tables shown to you. Never invent column or table names.
- Quote identifiers per ` + dialect + ` rules.
- Always include a LIMIT 100 unless the question explicitly requests aggregation.
- If the supplied schemas cannot answer the question, return exactly: SELECT 'INSUFFICIENT_CONTEXT' AS error;
- Output the SQL only — no explanation, no markdown fences.`
}

func buildUserPrompt(schemas, question string) string {
	return "Schemas:\n" + schemas + "\nQuestion: " + question + "\n\nSQL:"
}

// extractSQL strips common LLM cruft: markdown code fences, leading
// "sql" language tags, surrounding quotes. The model is told not to
// emit fences, but defence is cheap.
func extractSQL(s string) string {
	s = strings.TrimSpace(s)
	// Markdown code fence
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "sql")
		s = strings.TrimPrefix(s, "SQL")
		s = strings.TrimSpace(s)
		if end := strings.LastIndex(s, "```"); end != -1 {
			s = s[:end]
		}
	}
	return strings.TrimSpace(s)
}

func qualify(schema, table string) string {
	if schema == "" {
		return table
	}
	return schema + "." + table
}
