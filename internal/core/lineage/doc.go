// Package lineage parses SQL into source→target dependency edges.
//
// The parser is dialect-aware (a single dialect first, additional dialects
// added per milestone) and emits column-level lineage where possible. Output
// is normalized to LineageEdge protos consumed by the graph package.
package lineage
