package transform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
)

const ConnectorName = "transform"

func init() {
	shared.Default.MustRegister(ConnectorName, func() shared.Connector { return &Connector{} })
}

// Connector reads transformation-tool manifest artifacts from disk and emits
// model/source assets plus LINEAGE edges from depends_on relationships.
//
// Config keys:
//
//	manifest_path       — required; path to manifest.json
//	run_results_path    — optional; path to run_results.json. When set, the
//	                      most recent status/timing is merged into each
//	                      asset's properties.
//	project_name        — optional override for the project segment of QN.
//	                      Defaults to manifest.metadata.project_name, then
//	                      "default".
//	include_tests       — bool; default false. When true, test resources
//	                      become glossary-term assets for documentation.
type Connector struct{}

func (Connector) Info() shared.Info {
	return shared.Info{
		Name:    ConnectorName,
		Version: "0.1.0",
		SupportedAssetTypes: []graph.AssetType{
			graph.AssetTypeTable,
			graph.AssetTypeView,
			graph.AssetTypeGlossaryTerm,
		},
		SupportsLineage: true,
	}
}

func (Connector) Validate(_ context.Context, cfg shared.Config) error {
	if err := cfg.Required("manifest_path"); err != nil {
		return err
	}
	path := cfg.String("manifest_path", "")
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()
	if _, err := ParseManifest(f); err != nil {
		return err
	}
	return nil
}

func (Connector) Crawl(ctx context.Context, cfg shared.Config, sink shared.Sink) error {
	if err := cfg.Required("manifest_path"); err != nil {
		return err
	}

	manifest, err := readManifest(cfg.String("manifest_path", ""))
	if err != nil {
		return err
	}

	project := cfg.String("project_name", manifest.Metadata.ProjectName)
	if project == "" {
		project = "default"
	}
	includeTests := cfg.Bool("include_tests", false)

	// Pass 1: emit one asset per node so qualified names exist before edges.
	for _, node := range manifest.Nodes {
		if node.ResourceType == ResourceTest && !includeTests {
			continue
		}
		if err := sink.UpsertAsset(ctx, nodeToAsset(project, node)); err != nil {
			return err
		}
	}

	// Pass 2: emit LINEAGE edges for depends_on relationships. Edges carry
	// source_qn/target_qn in properties; the BatchedSink resolves them to
	// asset IDs at flush time.
	for _, node := range manifest.Nodes {
		if node.ResourceType == ResourceTest && !includeTests {
			continue
		}
		targetQN := nodeQN(project, node)
		for _, upstreamID := range node.DependsOn.Nodes {
			upstream, ok := manifest.Nodes[upstreamID]
			if !ok {
				continue // dangling reference, ignore
			}
			if upstream.ResourceType == ResourceTest && !includeTests {
				continue
			}
			edge := &graph.Edge{
				Kind: graph.EdgeLineage,
				Properties: map[string]any{
					"source_qn":         nodeQN(project, upstream),
					"target_qn":         targetQN,
					"op":                "transform",
					"upstream_unique":   upstream.UniqueID,
					"downstream_unique": node.UniqueID,
				},
			}
			if err := sink.UpsertEdge(ctx, edge); err != nil {
				return err
			}
		}
	}

	// Pass 3: optional run_results overlay.
	if rrPath := cfg.String("run_results_path", ""); rrPath != "" {
		rr, err := readRunResults(rrPath)
		if err != nil {
			return err
		}
		if err := applyRunResults(ctx, project, manifest, rr, sink); err != nil {
			return err
		}
	}

	return sink.Flush(ctx)
}

// nodeToAsset converts a manifest Node into a graph.Asset. resource_type and
// any extra metadata land on Properties for downstream agents to consume.
func nodeToAsset(project string, n Node) *graph.Asset {
	assetType := graph.AssetTypeTable
	switch n.ResourceType {
	case ResourceTest:
		assetType = graph.AssetTypeGlossaryTerm
	}

	props := map[string]any{
		"resource_type": string(n.ResourceType),
		"unique_id":     n.UniqueID,
		"database":      n.Database,
		"schema":        n.Schema,
	}
	if n.RawSQL != "" {
		props["raw_sql_hash"] = sqlHash(n.RawSQL)
	}

	return &graph.Asset{
		QualifiedName: nodeQN(project, n),
		Type:          assetType,
		Name:          n.Name,
		Description:   n.Description,
		Tags:          n.Tags,
		Properties:    props,
	}
}

// nodeQN returns the canonical qualified name used for a node. Format:
// transform://<project>/<database>/<schema>/<name>. When database is empty
// (some tools omit it for sources), it collapses to transform://<project>/<schema>/<name>.
func nodeQN(project string, n Node) string {
	if n.Database == "" {
		return fmt.Sprintf("transform://%s/%s/%s", project, n.Schema, n.Name)
	}
	return fmt.Sprintf("transform://%s/%s/%s/%s", project, n.Database, n.Schema, n.Name)
}

func readManifest(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return ParseManifest(f)
}

func readRunResults(path string) (*RunResults, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return ParseRunResults(f)
}

// applyRunResults re-upserts each asset that has a recent run result, with
// status/timing folded into properties. Assets without a corresponding
// result are left untouched.
func applyRunResults(ctx context.Context, project string, m *Manifest, rr *RunResults, sink shared.Sink) error {
	for _, r := range rr.Results {
		node, ok := m.Nodes[r.UniqueID]
		if !ok {
			continue
		}
		patch := nodeToAsset(project, node)
		patch.Properties["last_run_status"] = r.Status
		patch.Properties["last_run_completed_at"] = r.CompletedAt
		patch.Properties["last_run_duration_seconds"] = r.ExecutionTime
		if r.Message != "" {
			patch.Properties["last_run_message"] = r.Message
		}
		if err := sink.UpsertAsset(ctx, patch); err != nil {
			return err
		}
	}
	return nil
}

func sqlHash(sql string) string {
	h := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(h[:])[:16]
}
