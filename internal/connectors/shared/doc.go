// Package shared defines the Connector interface and helpers used by every
// data-source connector in internal/connectors/*.
//
// A connector implements:
//
//	type Connector interface {
//	    Info() ConnectorInfo
//	    Validate(ctx context.Context, cfg Config) error
//	    Crawl(ctx context.Context, cfg Config, sink Sink) error
//	    Lineage(ctx context.Context, cfg Config, sink Sink) error
//	}
//
// Sink batches Asset and Edge writes and forwards them to the graph engine.
package shared
