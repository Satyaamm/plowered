// Package mcp wires Plowered's storage layer into MCP tools. Each tool's
// JSON arguments and output schema match the MCP tool list returned to
// clients. Auth and tenant isolation are enforced upstream by the cmd/
// plowered-mcp binary which supplies a tenant-bound context.Context.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/pkg/mcp"
)

// Register installs Plowered's MCP tools on the given registry. Pass the
// shared storage.Store; the handlers use the ctx-bound tenant for isolation.
func Register(reg *mcp.ToolRegistry, store storage.Store) error {
	for _, t := range []mcp.ToolRegistration{
		{
			Tool: mcp.Tool{
				Name:        "search_assets",
				Description: "Search the catalog by keyword across qualified names. Returns up to `limit` matches.",
				InputSchema: schemaSearchAssets,
			},
			Handler: searchAssets(store),
		},
		{
			Tool: mcp.Tool{
				Name:        "get_asset",
				Description: "Fetch a single asset by qualified name. Returns name, type, description, tags, owners.",
				InputSchema: schemaGetAsset,
			},
			Handler: getAsset(store),
		},
		{
			Tool: mcp.Tool{
				Name:        "get_lineage",
				Description: "Return upstream or downstream lineage for an asset. `direction` is 'upstream' or 'downstream'; depth defaults to 1.",
				InputSchema: schemaGetLineage,
			},
			Handler: getLineage(store),
		},
	} {
		if err := reg.Register(t.Tool, t.Handler); err != nil {
			return err
		}
	}
	return nil
}

// ----- tool: search_assets -----

var schemaSearchAssets = json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "minLength": 1},
    "limit": {"type": "integer", "minimum": 1, "maximum": 100, "default": 10}
  },
  "required": ["query"]
}`)

type searchAssetsArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func searchAssets(store storage.Store) mcp.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a searchAssetsArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return mcp.ErrorResult("invalid arguments: %v", err), nil
		}
		if a.Query == "" {
			return mcp.ErrorResult("query is required"), nil
		}
		if a.Limit <= 0 || a.Limit > 100 {
			a.Limit = 10
		}

		// v0: list and filter by qualified-name substring. The dedicated
		// search package replaces this once it lands.
		assets, _, err := store.ListAssets(ctx, storage.ListAssetsOptions{PageSize: 200})
		if err != nil {
			return mcp.ErrorResult("list: %v", err), nil
		}

		var matches []*graph.Asset
		needle := strings.ToLower(a.Query)
		for _, x := range assets {
			if strings.Contains(strings.ToLower(x.QualifiedName), needle) ||
				strings.Contains(strings.ToLower(x.Name), needle) {
				matches = append(matches, x)
				if len(matches) >= a.Limit {
					break
				}
			}
		}
		return mcp.TextResult(formatSearchHits(matches)), nil
	}
}

func formatSearchHits(hits []*graph.Asset) string {
	if len(hits) == 0 {
		return "No matching assets."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d match(es):\n\n", len(hits))
	for _, a := range hits {
		fmt.Fprintf(&b, "- %s (%s)\n", a.QualifiedName, a.Type)
		if a.Description != "" {
			fmt.Fprintf(&b, "  %s\n", a.Description)
		}
	}
	return b.String()
}

// ----- tool: get_asset -----

var schemaGetAsset = json.RawMessage(`{
  "type": "object",
  "properties": {
    "qualified_name": {"type": "string", "minLength": 1}
  },
  "required": ["qualified_name"]
}`)

type getAssetArgs struct {
	QualifiedName string `json:"qualified_name"`
}

func getAsset(store storage.Store) mcp.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a getAssetArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return mcp.ErrorResult("invalid arguments: %v", err), nil
		}
		if a.QualifiedName == "" {
			return mcp.ErrorResult("qualified_name is required"), nil
		}
		asset, err := store.GetAssetByQualifiedName(ctx, a.QualifiedName)
		if err != nil {
			if errors.Is(err, graph.ErrNotFound) {
				return mcp.ErrorResult("asset %q not found", a.QualifiedName), nil
			}
			return mcp.ErrorResult("get: %v", err), nil
		}
		return mcp.TextResult(formatAsset(asset)), nil
	}
}

func formatAsset(a *graph.Asset) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", a.QualifiedName)
	fmt.Fprintf(&b, "Type:  %s\n", a.Type)
	fmt.Fprintf(&b, "Trust: %s\n", a.Trust)
	if a.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", a.Description)
	}
	if len(a.Tags) > 0 {
		fmt.Fprintf(&b, "\nTags: %s\n", strings.Join(a.Tags, ", "))
	}
	if len(a.Owners) > 0 {
		fmt.Fprintf(&b, "Owners: %s\n", strings.Join(a.Owners, ", "))
	}
	if !a.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, "Updated: %s\n", a.UpdatedAt.Format("2006-01-02"))
	}
	return b.String()
}

// ----- tool: get_lineage -----

var schemaGetLineage = json.RawMessage(`{
  "type": "object",
  "properties": {
    "qualified_name": {"type": "string", "minLength": 1},
    "direction": {"type": "string", "enum": ["upstream", "downstream"], "default": "upstream"},
    "depth": {"type": "integer", "minimum": 1, "maximum": 5, "default": 1}
  },
  "required": ["qualified_name"]
}`)

type getLineageArgs struct {
	QualifiedName string `json:"qualified_name"`
	Direction     string `json:"direction"`
	Depth         int    `json:"depth"`
}

func getLineage(store storage.Store) mcp.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a getLineageArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return mcp.ErrorResult("invalid arguments: %v", err), nil
		}
		if a.Direction == "" {
			a.Direction = "upstream"
		}
		if a.Depth <= 0 {
			a.Depth = 1
		}
		root, err := store.GetAssetByQualifiedName(ctx, a.QualifiedName)
		if err != nil {
			return mcp.ErrorResult("get: %v", err), nil
		}
		outgoing := a.Direction == "downstream"
		edges, err := store.Neighbors(ctx, root.ID, storage.NeighborsOptions{
			Kind:     graph.EdgeLineage,
			Outgoing: outgoing,
			Limit:    100,
		})
		if err != nil {
			return mcp.ErrorResult("neighbors: %v", err), nil
		}
		return mcp.TextResult(formatLineage(root, edges, a.Direction)), nil
	}
}

func formatLineage(root *graph.Asset, edges []*graph.Edge, direction string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s lineage of %s (%d edge(s) at depth 1):\n\n",
		strings.Title(direction), root.QualifiedName, len(edges))
	if len(edges) == 0 {
		b.WriteString("  (no immediate neighbors)\n")
		return b.String()
	}
	for _, e := range edges {
		other := e.TargetID
		if direction == "upstream" {
			other = e.SourceID
		}
		fmt.Fprintf(&b, "  - %s\n", other)
	}
	return b.String()
}
