// Package mongodb_source is the Plowered adapter for MongoDB
// datasources.
//
// MongoDB is schemaless from the server's perspective; we derive a
// "schema" by sampling N documents per collection and unioning their
// top-level keys. Type inference is per-key first-seen: integer, double,
// string, boolean, array, object, date, objectid, null.
//
// Config shape (JSON):
//
//	{
//	  "uri":      "mongodb://host:27017",   // optional if user/host given
//	  "user":     "plowered",                // optional
//	  "host":     "cluster.example.net",     // alternative to uri
//	  "port":     27017,
//	  "database": "analytics",               // optional; restricts crawl
//	  "auth_db":  "admin",                   // optional default db for auth
//	  "tls":      true                        // optional
//	}
//
// The secret bytes are the password. When `uri` is set we use it
// verbatim (allowing srv records, replica-set hosts, query params);
// otherwise we synthesise a DSN from the discrete fields.
package mongodb_source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/crawler"
)

// SampleSize is the number of documents per collection we sample to
// derive a key/type schema. Larger samples catch rare fields at the
// cost of a longer crawl. 200 is the same default as the SQL
// classifiers — keeps the crawl bounded for wide collections.
const SampleSize = 200

// Tester satisfies connection.Tester.
type Tester struct{}

func New() *Tester { return &Tester{} }

func (Tester) Test(ctx context.Context, cfg map[string]any, secret []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, err := dial(ctx, cfg, secret)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)
	return client.Ping(ctx, nil)
}

// Crawler satisfies crawler.Source.
type Crawler struct{}

func NewCrawler() *Crawler { return &Crawler{} }

func (Crawler) Crawl(ctx context.Context, cfg map[string]any, secret []byte) (*crawler.Tree, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client, err := dial(ctx, cfg, secret)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	dbs := []string{}
	if scoped, _ := cfg["database"].(string); scoped != "" {
		dbs = append(dbs, scoped)
	} else {
		names, err := client.ListDatabaseNames(ctx, bson.M{})
		if err != nil {
			return nil, fmt.Errorf("list databases: %w", err)
		}
		for _, n := range names {
			if n == "admin" || n == "local" || n == "config" {
				continue
			}
			dbs = append(dbs, n)
		}
	}

	tree := &crawler.Tree{}
	for _, dbName := range dbs {
		db := client.Database(dbName)
		colls, err := db.ListCollectionNames(ctx, bson.M{})
		if err != nil {
			return nil, fmt.Errorf("list collections in %s: %w", dbName, err)
		}
		sort.Strings(colls)
		schema := crawler.SchemaInfo{Name: dbName}
		for _, name := range colls {
			cols, err := sampleCollection(ctx, db.Collection(name))
			if err != nil {
				return nil, fmt.Errorf("sample %s.%s: %w", dbName, name, err)
			}
			schema.Tables = append(schema.Tables, crawler.TableInfo{
				Name:    name,
				Kind:    "collection",
				Columns: cols,
			})
		}
		tree.Schemas = append(tree.Schemas, schema)
	}
	return tree, nil
}

// sampleCollection reads up to SampleSize docs and projects top-level
// keys into column-like descriptors. Type is the first non-null bson
// type observed for each key.
func sampleCollection(ctx context.Context, coll *mongo.Collection) ([]crawler.ColumnInfo, error) {
	opts := options.Find().SetLimit(SampleSize)
	cur, err := coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	types := map[string]string{}
	seen := map[string]int{}
	nullable := map[string]bool{}
	count := 0
	for cur.Next(ctx) {
		count++
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		for k, v := range doc {
			seen[k]++
			if v == nil {
				nullable[k] = true
				continue
			}
			if _, ok := types[k]; !ok {
				types[k] = bsonTypeName(v)
			}
		}
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]crawler.ColumnInfo, 0, len(keys))
	for i, k := range keys {
		// A key counts as nullable if it appeared with null OR was
		// missing from at least one sampled doc.
		isNullable := nullable[k] || seen[k] < count
		out = append(out, crawler.ColumnInfo{
			Name:       k,
			DataType:   types[k],
			Nullable:   isNullable,
			OrdinalPos: i + 1,
		})
	}
	return out, nil
}

func bsonTypeName(v any) string {
	switch v.(type) {
	case bool:
		return "boolean"
	case int32:
		return "int"
	case int64:
		return "long"
	case float64:
		return "double"
	case string:
		return "string"
	case bson.ObjectID:
		return "objectid"
	case bson.DateTime, time.Time:
		return "date"
	case bson.M, bson.D:
		return "object"
	case bson.A, []any:
		return "array"
	}
	return fmt.Sprintf("%T", v)
}

// dial builds a mongo.Client. URI takes precedence; the discrete
// fields are convenience for wizard-style configuration.
func dial(ctx context.Context, cfg map[string]any, secret []byte) (*mongo.Client, error) {
	uri, _ := cfg["uri"].(string)
	if uri == "" {
		built, err := buildURI(cfg, secret)
		if err != nil {
			return nil, err
		}
		uri = built
	}
	clientOpts := options.Client().ApplyURI(uri)
	// When uri didn't carry credentials but a separate user/secret did,
	// layer them onto SetAuth.
	if user, ok := cfg["user"].(string); ok && user != "" && len(secret) > 0 {
		clientOpts.SetAuth(options.Credential{
			Username:   user,
			Password:   string(secret),
			AuthSource: stringOr(cfg["auth_db"], "admin"),
		})
	}
	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	return client, nil
}

func buildURI(cfg map[string]any, secret []byte) (string, error) {
	host, _ := cfg["host"].(string)
	if host == "" {
		return "", errors.New("mongodb_source: either uri or host is required")
	}
	port := intOr(cfg["port"], 27017)
	u := &url.URL{
		Scheme: "mongodb",
		Host:   fmt.Sprintf("%s:%d", host, port),
	}
	if user, _ := cfg["user"].(string); user != "" {
		u.User = url.UserPassword(user, string(secret))
	}
	if db, _ := cfg["database"].(string); db != "" {
		u.Path = "/" + db
	}
	q := u.Query()
	if tls, _ := cfg["tls"].(bool); tls {
		q.Set("tls", "true")
	}
	if auth, _ := cfg["auth_db"].(string); auth != "" {
		q.Set("authSource", auth)
	}
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
	return u.String(), nil
}

func intOr(v any, fallback int) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return fallback
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

var _ connection.Tester = (*Tester)(nil)
var _ crawler.Source = (*Crawler)(nil)
