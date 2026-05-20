// Package bigquery_driver is the real implementation of the BigQuery
// adapter, registered into bigquery_source via SetActive at process
// start. Kept in its own package so deployments that don't ship the
// cloud.google.com/go/bigquery dependency still compile cleanly —
// the cmd binary blank-imports this package to wire it in.
//
// Auth modes:
//   - "service_account": secret bytes are the service-account JSON key.
//   - "workload_identity": secret ignored; the runtime credentials chain
//     (GKE workload identity, gcloud user, GOOGLE_APPLICATION_CREDENTIALS)
//     is used.
package bigquery_driver

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/Satyaamm/plowered/internal/adapters/bigquery_source"
	"github.com/Satyaamm/plowered/internal/core/crawler"
	"github.com/Satyaamm/plowered/internal/core/warehouse"
)

func init() {
	bigquery_source.SetActive(&driver{})
}

type driver struct{}

// Test validates auth + project access by listing datasets (free, fast).
func (driver) Test(ctx context.Context, projectID, location, authMethod string, secret []byte) error {
	client, err := newClient(ctx, projectID, location, authMethod, secret)
	if err != nil {
		return err
	}
	defer client.Close()
	it := client.Datasets(ctx)
	if _, err := it.Next(); err != nil && err != iterator.Done {
		return fmt.Errorf("bigquery: list datasets: %w", err)
	}
	return nil
}

// Crawl walks every dataset → table → column. When scopedDataset is
// supplied, only that dataset is walked.
func (driver) Crawl(ctx context.Context, projectID, scopedDataset, location, authMethod string, secret []byte) (*crawler.Tree, error) {
	client, err := newClient(ctx, projectID, location, authMethod, secret)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	var datasets []string
	if scopedDataset != "" {
		datasets = []string{scopedDataset}
	} else {
		it := client.Datasets(ctx)
		for {
			d, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("list datasets: %w", err)
			}
			datasets = append(datasets, d.DatasetID)
		}
		sort.Strings(datasets)
	}

	tree := &crawler.Tree{}
	for _, dsID := range datasets {
		ds := client.Dataset(dsID)
		schema := crawler.SchemaInfo{Name: dsID}
		tIt := ds.Tables(ctx)
		for {
			t, err := tIt.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("list tables in %s: %w", dsID, err)
			}
			meta, err := t.Metadata(ctx)
			if err != nil {
				return nil, fmt.Errorf("metadata %s.%s: %w", dsID, t.TableID, err)
			}
			info := crawler.TableInfo{
				Name:        t.TableID,
				Kind:        tableKind(meta.Type),
				Description: meta.Description,
			}
			for i, f := range meta.Schema {
				info.Columns = append(info.Columns, crawler.ColumnInfo{
					Name:        f.Name,
					DataType:    string(f.Type),
					Nullable:    !f.Required,
					OrdinalPos:  i + 1,
					Description: f.Description,
				})
			}
			schema.Tables = append(schema.Tables, info)
		}
		sort.Slice(schema.Tables, func(i, j int) bool { return schema.Tables[i].Name < schema.Tables[j].Name })
		tree.Schemas = append(tree.Schemas, schema)
	}
	return tree, nil
}

// NewExecutor builds a warehouse.Executor for BigQuery queries. Used
// by the warehouse factory so profile / asker / migration work against
// BigQuery without per-feature changes.
func NewExecutor(ctx context.Context, cfg map[string]any, secret []byte) (warehouse.Executor, error) {
	projectID, _ := cfg["project_id"].(string)
	if projectID == "" {
		return nil, errors.New("bigquery: project_id required")
	}
	location, _ := cfg["location"].(string)
	authMethod, _ := cfg["auth_method"].(string)
	if authMethod == "" {
		authMethod = "service_account"
	}
	client, err := newClient(ctx, projectID, location, authMethod, secret)
	if err != nil {
		return nil, err
	}
	return &bqExecutor{client: client}, nil
}

func newClient(ctx context.Context, projectID, location, authMethod string, secret []byte) (*bigquery.Client, error) {
	var opts []option.ClientOption
	switch authMethod {
	case "service_account":
		if len(secret) == 0 {
			return nil, errors.New("bigquery: service_account auth requires a JSON key in the secret")
		}
		opts = append(opts, option.WithCredentialsJSON(secret))
	case "workload_identity", "":
		// default credentials chain
	default:
		return nil, fmt.Errorf("bigquery: unknown auth_method %q", authMethod)
	}
	client, err := bigquery.NewClient(ctx, projectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("bigquery: new client: %w", err)
	}
	if location != "" {
		client.Location = location
	}
	return client, nil
}

func tableKind(t bigquery.TableType) string {
	switch t {
	case bigquery.ViewTable, bigquery.MaterializedView:
		return "view"
	}
	return "table"
}

// bqExecutor adapts a BigQuery client to warehouse.Executor. The
// client is closed on Rows.Close().
type bqExecutor struct {
	client *bigquery.Client
}

func (e *bqExecutor) Query(ctx context.Context, sqlText string) (warehouse.Rows, error) {
	q := e.client.Query(sqlText)
	it, err := q.Read(ctx)
	if err != nil {
		_ = e.client.Close()
		return nil, fmt.Errorf("bigquery query: %w", err)
	}
	return &bqRows{it: it, client: e.client}, nil
}

type bqRows struct {
	it      *bigquery.RowIterator
	client  *bigquery.Client
	cols    []string
	current []bigquery.Value
	err     error
	exhausted bool
}

func (r *bqRows) Columns() []string {
	if r.cols != nil {
		return r.cols
	}
	// Schema is populated after the first Next(); pre-emptively try to
	// pull it. If still nil (no rows ever), return an empty slice so
	// callers get a deterministic shape.
	if r.it != nil && r.it.Schema != nil {
		for _, f := range r.it.Schema {
			r.cols = append(r.cols, f.Name)
		}
	}
	return r.cols
}

func (r *bqRows) Next() bool {
	if r.err != nil || r.exhausted {
		return false
	}
	var row []bigquery.Value
	err := r.it.Next(&row)
	if err == iterator.Done {
		r.exhausted = true
		return false
	}
	if err != nil {
		r.err = err
		return false
	}
	r.current = row
	if r.cols == nil && r.it.Schema != nil {
		for _, f := range r.it.Schema {
			r.cols = append(r.cols, f.Name)
		}
	}
	return true
}

func (r *bqRows) Scan(dest ...any) error {
	if len(dest) != len(r.current) {
		return fmt.Errorf("bigquery scan: got %d cols, dest has %d", len(r.current), len(dest))
	}
	for i, v := range r.current {
		ptr, ok := dest[i].(*any)
		if !ok {
			return fmt.Errorf("bigquery scan: dest[%d] must be *any", i)
		}
		*ptr = v
	}
	return nil
}

func (r *bqRows) Err() error { return r.err }

func (r *bqRows) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}
