// Package athena_source is the Plowered adapter for AWS Athena
// datasources.
//
// Athena exposes both a metadata surface (list databases + table
// metadata under a workgroup's data catalog) and a query surface (start
// + poll a QueryExecution). The crawler uses the metadata API so we
// don't pay per-scan for catalog reads; the warehouse.Executor uses
// StartQueryExecution + GetQueryResults so profile / asker / migration
// can drive it.
//
// Config shape (JSON):
//
//	{
//	  "region":        "us-east-1",
//	  "workgroup":     "primary",
//	  "data_catalog":  "AwsDataCatalog",        // optional, defaults to AwsDataCatalog
//	  "output_location": "s3://my-bucket/athena/", // required for queries
//	  "database":      "analytics"               // optional; restricts crawl
//	}
//
// The secret bytes carry static AWS credentials as
// "ACCESS_KEY_ID:SECRET_ACCESS_KEY" when not relying on the default
// credential chain.
package athena_source

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	atypes "github.com/aws/aws-sdk-go-v2/service/athena/types"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/crawler"
)

const defaultCatalog = "AwsDataCatalog"

// Tester satisfies connection.Tester. ListWorkGroups is the lightest
// authenticated call that proves the credentials work in the region.
type Tester struct{}

func New() *Tester { return &Tester{} }

func (Tester) Test(ctx context.Context, cfg map[string]any, secret []byte) error {
	client, err := newClient(ctx, cfg, secret)
	if err != nil {
		return err
	}
	wg, _ := cfg["workgroup"].(string)
	if wg == "" {
		wg = "primary"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = client.GetWorkGroup(ctx, &athena.GetWorkGroupInput{WorkGroup: awsv2.String(wg)})
	return err
}

// Crawler satisfies crawler.Source. Iterates the catalog → databases →
// tables; each table's column list is pulled via GetTableMetadata.
type Crawler struct{}

func NewCrawler() *Crawler { return &Crawler{} }

func (Crawler) Crawl(ctx context.Context, cfg map[string]any, secret []byte) (*crawler.Tree, error) {
	client, err := newClient(ctx, cfg, secret)
	if err != nil {
		return nil, err
	}
	catalog := stringOr(cfg["data_catalog"], defaultCatalog)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	dbNames := []string{}
	if scoped, _ := cfg["database"].(string); scoped != "" {
		dbNames = append(dbNames, scoped)
	} else {
		var next *string
		for {
			out, err := client.ListDatabases(ctx, &athena.ListDatabasesInput{
				CatalogName: awsv2.String(catalog),
				NextToken:   next,
			})
			if err != nil {
				return nil, fmt.Errorf("list databases: %w", err)
			}
			for _, d := range out.DatabaseList {
				dbNames = append(dbNames, awsv2.ToString(d.Name))
			}
			if out.NextToken == nil {
				break
			}
			next = out.NextToken
		}
	}

	tree := &crawler.Tree{}
	for _, db := range dbNames {
		schema := crawler.SchemaInfo{Name: db}
		var next *string
		for {
			out, err := client.ListTableMetadata(ctx, &athena.ListTableMetadataInput{
				CatalogName:  awsv2.String(catalog),
				DatabaseName: awsv2.String(db),
				NextToken:    next,
			})
			if err != nil {
				return nil, fmt.Errorf("list table metadata in %s: %w", db, err)
			}
			for _, t := range out.TableMetadataList {
				schema.Tables = append(schema.Tables, tableInfoFromMetadata(t))
			}
			if out.NextToken == nil {
				break
			}
			next = out.NextToken
		}
		sort.Slice(schema.Tables, func(i, j int) bool {
			return schema.Tables[i].Name < schema.Tables[j].Name
		})
		tree.Schemas = append(tree.Schemas, schema)
	}
	return tree, nil
}

func tableInfoFromMetadata(t atypes.TableMetadata) crawler.TableInfo {
	kind := "table"
	if strings.EqualFold(awsv2.ToString(t.TableType), "VIRTUAL_VIEW") {
		kind = "view"
	}
	info := crawler.TableInfo{
		Name: awsv2.ToString(t.Name),
		Kind: kind,
	}
	for i, c := range t.Columns {
		info.Columns = append(info.Columns, crawler.ColumnInfo{
			Name:        awsv2.ToString(c.Name),
			DataType:    awsv2.ToString(c.Type),
			Nullable:    true, // Athena/Glue metadata doesn't ship null constraint
			OrdinalPos:  i + 1,
			Description: awsv2.ToString(c.Comment),
		})
	}
	// Partition columns are appended after regular columns with a
	// description hint so the catalog reflects them.
	for i, c := range t.PartitionKeys {
		info.Columns = append(info.Columns, crawler.ColumnInfo{
			Name:        awsv2.ToString(c.Name),
			DataType:    awsv2.ToString(c.Type),
			Nullable:    false,
			OrdinalPos:  len(t.Columns) + i + 1,
			Description: "partition_key",
		})
	}
	return info
}

func newClient(ctx context.Context, cfg map[string]any, secret []byte) (*athena.Client, error) {
	region, _ := cfg["region"].(string)
	if region == "" {
		return nil, errors.New("athena_source: region is required")
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if len(secret) > 0 {
		parts := strings.SplitN(string(secret), ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("athena_source: secret must be ACCESS_KEY_ID:SECRET_ACCESS_KEY")
		}
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(parts[0], parts[1], ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return athena.NewFromConfig(awsCfg), nil
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

var _ connection.Tester = (*Tester)(nil)
var _ crawler.Source = (*Crawler)(nil)
