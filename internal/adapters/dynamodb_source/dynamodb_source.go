// Package dynamodb_source is the Plowered adapter for AWS DynamoDB
// datasources.
//
// DynamoDB is schemaless beyond the primary key; we derive a per-table
// "schema" by scanning N items and unioning the top-level attribute
// names + their first-seen type. The key schema (HASH + optional RANGE)
// is reported with role hints in the column description.
//
// Config shape (JSON):
//
//	{
//	  "region":   "us-east-1",   // required
//	  "endpoint": "",             // optional, for DynamoDB Local / VPC endpoint
//	  "table":    "orders"        // optional; restricts crawl to one table
//	}
//
// The secret bytes carry static AWS credentials as
// "ACCESS_KEY_ID:SECRET_ACCESS_KEY" or are ignored entirely when the
// deployment uses the default credential chain (IRSA, instance role).
package dynamodb_source

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
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/crawler"
)

// SampleSize is the number of items per table we scan to derive an
// attribute/type schema. 200 matches the SQL classifier defaults.
const SampleSize int32 = 200

// Tester satisfies connection.Tester. ListTables is the lightest
// permissioned call that proves we have a working DynamoDB session.
type Tester struct{}

func New() *Tester { return &Tester{} }

func (Tester) Test(ctx context.Context, cfg map[string]any, secret []byte) error {
	client, err := newClient(ctx, cfg, secret)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = client.ListTables(ctx, &dynamodb.ListTablesInput{Limit: awsv2.Int32(1)})
	return err
}

// Crawler satisfies crawler.Source. DynamoDB has no "database/schema"
// concept; the Tree carries one synthetic schema named "dynamodb" so
// the catalog projection (connection.schema.table.column) stays
// uniform with SQL sources.
type Crawler struct{}

func NewCrawler() *Crawler { return &Crawler{} }

func (Crawler) Crawl(ctx context.Context, cfg map[string]any, secret []byte) (*crawler.Tree, error) {
	client, err := newClient(ctx, cfg, secret)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	tables := []string{}
	if t, _ := cfg["table"].(string); t != "" {
		tables = []string{t}
	} else {
		var marker *string
		for {
			out, err := client.ListTables(ctx, &dynamodb.ListTablesInput{
				ExclusiveStartTableName: marker,
				Limit:                   awsv2.Int32(100),
			})
			if err != nil {
				return nil, fmt.Errorf("list tables: %w", err)
			}
			tables = append(tables, out.TableNames...)
			if out.LastEvaluatedTableName == nil {
				break
			}
			marker = out.LastEvaluatedTableName
		}
	}
	sort.Strings(tables)

	schema := crawler.SchemaInfo{Name: "dynamodb"}
	for _, name := range tables {
		desc, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: awsv2.String(name)})
		if err != nil {
			return nil, fmt.Errorf("describe %s: %w", name, err)
		}
		cols, err := sampleTable(ctx, client, name, desc.Table)
		if err != nil {
			return nil, fmt.Errorf("sample %s: %w", name, err)
		}
		schema.Tables = append(schema.Tables, crawler.TableInfo{
			Name:    name,
			Kind:    "table",
			Columns: cols,
		})
	}
	return &crawler.Tree{Schemas: []crawler.SchemaInfo{schema}}, nil
}

func sampleTable(ctx context.Context, client *dynamodb.Client, name string, table *dtypes.TableDescription) ([]crawler.ColumnInfo, error) {
	keyRole := map[string]string{}
	if table != nil {
		for _, k := range table.KeySchema {
			role := "partition_key"
			if k.KeyType == dtypes.KeyTypeRange {
				role = "sort_key"
			}
			keyRole[awsv2.ToString(k.AttributeName)] = role
		}
	}

	out, err := client.Scan(ctx, &dynamodb.ScanInput{
		TableName: awsv2.String(name),
		Limit:     awsv2.Int32(SampleSize),
	})
	if err != nil {
		return nil, err
	}

	types := map[string]string{}
	seen := map[string]int{}
	for _, item := range out.Items {
		for k, v := range item {
			seen[k]++
			if _, ok := types[k]; !ok {
				types[k] = attrTypeName(v)
			}
		}
	}
	// Always surface declared key attributes even if they weren't in
	// the sampled items (defensive — a fresh table may be empty).
	for k := range keyRole {
		if _, ok := types[k]; !ok {
			types[k] = "scalar"
		}
	}

	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cols := make([]crawler.ColumnInfo, 0, len(keys))
	for i, k := range keys {
		desc := ""
		if role := keyRole[k]; role != "" {
			desc = role
		}
		cols = append(cols, crawler.ColumnInfo{
			Name:        k,
			DataType:    types[k],
			Nullable:    seen[k] < int(out.Count),
			OrdinalPos:  i + 1,
			Description: desc,
		})
	}
	return cols, nil
}

func attrTypeName(v dtypes.AttributeValue) string {
	switch v.(type) {
	case *dtypes.AttributeValueMemberS:
		return "string"
	case *dtypes.AttributeValueMemberN:
		return "number"
	case *dtypes.AttributeValueMemberB:
		return "binary"
	case *dtypes.AttributeValueMemberBOOL:
		return "boolean"
	case *dtypes.AttributeValueMemberM:
		return "map"
	case *dtypes.AttributeValueMemberL:
		return "list"
	case *dtypes.AttributeValueMemberSS:
		return "string_set"
	case *dtypes.AttributeValueMemberNS:
		return "number_set"
	case *dtypes.AttributeValueMemberBS:
		return "binary_set"
	case *dtypes.AttributeValueMemberNULL:
		return "null"
	}
	return "unknown"
}

func newClient(ctx context.Context, cfg map[string]any, secret []byte) (*dynamodb.Client, error) {
	region, _ := cfg["region"].(string)
	if region == "" {
		return nil, errors.New("dynamodb_source: region is required")
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if len(secret) > 0 {
		parts := strings.SplitN(string(secret), ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("dynamodb_source: secret must be ACCESS_KEY_ID:SECRET_ACCESS_KEY")
		}
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(parts[0], parts[1], ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	dopts := []func(*dynamodb.Options){}
	if endpoint, _ := cfg["endpoint"].(string); endpoint != "" {
		dopts = append(dopts, func(o *dynamodb.Options) { o.BaseEndpoint = awsv2.String(endpoint) })
	}
	return dynamodb.NewFromConfig(awsCfg, dopts...), nil
}

var _ connection.Tester = (*Tester)(nil)
var _ crawler.Source = (*Crawler)(nil)
