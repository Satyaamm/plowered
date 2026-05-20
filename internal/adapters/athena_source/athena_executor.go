package athena_source

import (
	"context"
	"errors"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	atypes "github.com/aws/aws-sdk-go-v2/service/athena/types"

	"github.com/Satyaamm/plowered/internal/core/warehouse"
)

// NewExecutor returns a warehouse.Executor backed by Athena. The
// constructor takes the same cfg + secret pair as Tester / Crawler so
// the warehouse factory can dispatch by connection type.
func NewExecutor(ctx context.Context, cfg map[string]any, secret []byte) (warehouse.Executor, error) {
	client, err := newClient(ctx, cfg, secret)
	if err != nil {
		return nil, err
	}
	workgroup := stringOr(cfg["workgroup"], "primary")
	output, _ := cfg["output_location"].(string)
	if output == "" {
		return nil, errors.New("athena_source: output_location is required for queries")
	}
	database, _ := cfg["database"].(string)
	catalog := stringOr(cfg["data_catalog"], defaultCatalog)
	return &athenaExecutor{
		client:    client,
		workgroup: workgroup,
		output:    output,
		database:  database,
		catalog:   catalog,
	}, nil
}

type athenaExecutor struct {
	client    *athena.Client
	workgroup string
	output    string
	database  string
	catalog   string
}

func (e *athenaExecutor) Query(ctx context.Context, sqlText string) (warehouse.Rows, error) {
	startOut, err := e.client.StartQueryExecution(ctx, &athena.StartQueryExecutionInput{
		QueryString: awsv2.String(sqlText),
		WorkGroup:   awsv2.String(e.workgroup),
		QueryExecutionContext: &atypes.QueryExecutionContext{
			Database: awsv2.String(e.database),
			Catalog:  awsv2.String(e.catalog),
		},
		ResultConfiguration: &atypes.ResultConfiguration{
			OutputLocation: awsv2.String(e.output),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("athena: start query: %w", err)
	}
	id := awsv2.ToString(startOut.QueryExecutionId)
	if err := e.waitFor(ctx, id); err != nil {
		return nil, err
	}

	first, err := e.client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
		QueryExecutionId: awsv2.String(id),
	})
	if err != nil {
		return nil, fmt.Errorf("athena: get results: %w", err)
	}
	return newAthenaRows(ctx, e.client, id, first), nil
}

// waitFor polls the query state until terminal or ctx expires.
// Backoff doubles up to 1s — Athena queries that take <100ms exist
// (cached) but most run multiple seconds. A linear poll would burn
// API calls on slow queries.
func (e *athenaExecutor) waitFor(ctx context.Context, id string) error {
	delay := 50 * time.Millisecond
	for {
		out, err := e.client.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{
			QueryExecutionId: awsv2.String(id),
		})
		if err != nil {
			return fmt.Errorf("athena: get execution: %w", err)
		}
		state := out.QueryExecution.Status.State
		switch state {
		case atypes.QueryExecutionStateSucceeded:
			return nil
		case atypes.QueryExecutionStateFailed, atypes.QueryExecutionStateCancelled:
			reason := awsv2.ToString(out.QueryExecution.Status.StateChangeReason)
			return fmt.Errorf("athena: query %s ended in %s: %s", id, state, reason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < time.Second {
			delay *= 2
		}
	}
}

// athenaRows paginates GetQueryResults. The first page carries a header
// row Athena includes verbatim; we drop it once on construction and
// then surface only data rows.
type athenaRows struct {
	ctx      context.Context
	client   *athena.Client
	id       string
	cols     []string
	page     *athena.GetQueryResultsOutput
	pos      int
	consumed bool
	err      error
	current  []atypes.Datum
}

func newAthenaRows(ctx context.Context, client *athena.Client, id string, first *athena.GetQueryResultsOutput) *athenaRows {
	r := &athenaRows{ctx: ctx, client: client, id: id, page: first}
	if first.ResultSet != nil && first.ResultSet.ResultSetMetadata != nil {
		for _, c := range first.ResultSet.ResultSetMetadata.ColumnInfo {
			r.cols = append(r.cols, awsv2.ToString(c.Name))
		}
	}
	// Drop the header row Athena prepends on the first page.
	if len(first.ResultSet.Rows) > 0 {
		r.pos = 1
	}
	return r
}

func (r *athenaRows) Columns() []string { return r.cols }

func (r *athenaRows) Next() bool {
	if r.err != nil {
		return false
	}
	if r.page == nil {
		return false
	}
	if r.pos < len(r.page.ResultSet.Rows) {
		r.current = r.page.ResultSet.Rows[r.pos].Data
		r.pos++
		return true
	}
	// Next page?
	if r.page.NextToken == nil {
		return false
	}
	next, err := r.client.GetQueryResults(r.ctx, &athena.GetQueryResultsInput{
		QueryExecutionId: awsv2.String(r.id),
		NextToken:        r.page.NextToken,
	})
	if err != nil {
		r.err = err
		return false
	}
	r.page = next
	r.pos = 0
	return r.Next()
}

func (r *athenaRows) Scan(dest ...any) error {
	if len(dest) != len(r.cols) {
		return fmt.Errorf("athena: scan: got %d cols, dest has %d", len(r.cols), len(dest))
	}
	for i, d := range r.current {
		ptr, ok := dest[i].(*any)
		if !ok {
			return fmt.Errorf("athena: scan: dest[%d] must be *any", i)
		}
		if d.VarCharValue == nil {
			*ptr = nil
		} else {
			*ptr = awsv2.ToString(d.VarCharValue)
		}
	}
	return nil
}

func (r *athenaRows) Err() error   { return r.err }
func (r *athenaRows) Close() error { return nil }
