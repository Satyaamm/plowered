// Command plowered-mcp speaks Model Context Protocol over stdio so any
// MCP-compliant local agent can query a Plowered catalog.
package main

import (
	"log/slog"
	"os"
)

func main() {
	slog.New(slog.NewJSONHandler(os.Stderr, nil)).Info("plowered-mcp starting")
	// TODO(M5): implement MCP stdio transport, register tools (search_assets,
	// get_asset, get_lineage, get_glossary_term, propose_query) backed by the
	// gRPC client to a running plowered server.
}
