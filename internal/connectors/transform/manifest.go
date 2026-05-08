// Package transform is a connector for transformation frameworks that emit
// JSON manifest artifacts describing the model DAG and run results.
//
// The package targets a generic, dialect-agnostic manifest shape — fields
// that are common across mainstream tooling. Tool-specific fields are
// preserved on the asset's `properties` map but not interpreted.
package transform

import (
	"encoding/json"
	"fmt"
	"io"
)

// Manifest is the top-level artifact produced by a transformation tool.
type Manifest struct {
	Metadata Metadata        `json:"metadata"`
	Nodes    map[string]Node `json:"nodes"`
}

// Metadata describes the project and the manifest's provenance.
type Metadata struct {
	GeneratedAt string `json:"generated_at"`
	ProjectName string `json:"project_name"`
	AdapterType string `json:"adapter_type,omitempty"`
}

// ResourceType enumerates what a node represents.
type ResourceType string

const (
	ResourceModel    ResourceType = "model"
	ResourceSource   ResourceType = "source"
	ResourceSeed     ResourceType = "seed"
	ResourceSnapshot ResourceType = "snapshot"
	ResourceTest     ResourceType = "test"
)

// Node is one entry in the DAG. Models, sources, seeds, snapshots, and tests
// all share this shape; ResourceType discriminates them.
type Node struct {
	UniqueID     string       `json:"unique_id"`
	Name         string       `json:"name"`
	Database     string       `json:"database"`
	Schema       string       `json:"schema"`
	ResourceType ResourceType `json:"resource_type"`
	Description  string       `json:"description,omitempty"`
	Tags         []string     `json:"tags,omitempty"`
	DependsOn    DependsOn    `json:"depends_on"`
	RawSQL       string       `json:"raw_sql,omitempty"`
	CompiledSQL  string       `json:"compiled_sql,omitempty"`
}

// DependsOn lists the unique_ids of upstream nodes.
type DependsOn struct {
	Nodes []string `json:"nodes"`
}

// RunResults is the run-time companion to a Manifest: status and timing for
// each node in the most recent execution.
type RunResults struct {
	Metadata Metadata    `json:"metadata"`
	Results  []RunResult `json:"results"`
}

type RunResult struct {
	UniqueID      string  `json:"unique_id"`
	Status        string  `json:"status"`
	ExecutionTime float64 `json:"execution_time"`
	CompletedAt   string  `json:"completed_at"`
	Message       string  `json:"message,omitempty"`
}

// ParseManifest decodes a Manifest from r. Empty `nodes` is treated as an
// error — a manifest without a DAG is almost always misconfiguration.
func ParseManifest(r io.Reader) (*Manifest, error) {
	var m Manifest
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if len(m.Nodes) == 0 {
		return nil, fmt.Errorf("manifest contains no nodes")
	}
	return &m, nil
}

// ParseRunResults decodes a RunResults document.
func ParseRunResults(r io.Reader) (*RunResults, error) {
	var rr RunResults
	if err := json.NewDecoder(r).Decode(&rr); err != nil {
		return nil, fmt.Errorf("decode run_results: %w", err)
	}
	return &rr, nil
}
