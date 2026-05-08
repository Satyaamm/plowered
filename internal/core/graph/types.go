package graph

import (
	"time"
)

type AssetType string

const (
	AssetTypeUnspecified  AssetType = ""
	AssetTypeDatabase     AssetType = "database"
	AssetTypeSchema       AssetType = "schema"
	AssetTypeTable        AssetType = "table"
	AssetTypeView         AssetType = "view"
	AssetTypeColumn       AssetType = "column"
	AssetTypeDashboard    AssetType = "dashboard"
	AssetTypeReport       AssetType = "report"
	AssetTypeDBTModel     AssetType = "dbt_model"
	AssetTypeDAG          AssetType = "dag"
	AssetTypeMLModel      AssetType = "ml_model"
	AssetTypeGlossaryTerm AssetType = "glossary_term"
)

type TrustLevel string

const (
	TrustUnverified TrustLevel = "unverified"
	TrustDraft      TrustLevel = "draft"
	TrustReviewed   TrustLevel = "reviewed"
	TrustCertified  TrustLevel = "certified"
	TrustDeprecated TrustLevel = "deprecated"
)

// EdgeKind enumerates the relationships we track.
type EdgeKind string

const (
	EdgeLineage   EdgeKind = "lineage"
	EdgeOwnedBy   EdgeKind = "owned_by"
	EdgeTaggedAs  EdgeKind = "tagged_as"
	EdgeDefines   EdgeKind = "defines"
	EdgeDependsOn EdgeKind = "depends_on"
)

// Asset is the unit of metadata. The Properties map is intentionally narrow
// (string keys, JSON-encodable values); type-specific structured fields live
// on dedicated tables in storage.
type Asset struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	QualifiedName string         `json:"qualified_name"`
	Type          AssetType      `json:"type"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	DescriptionAI string         `json:"description_ai,omitempty"`
	Trust         TrustLevel     `json:"trust"`
	Tags          []string       `json:"tags,omitempty"`     // tag IDs
	Owners        []string       `json:"owners,omitempty"`   // user/group IDs
	Properties    map[string]any `json:"properties,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	CreatedBy     string         `json:"created_by,omitempty"`
	UpdatedBy     string         `json:"updated_by,omitempty"`
}

// Edge is a directed relationship in the graph.
type Edge struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Kind         EdgeKind  `json:"kind"`
	SourceID     string    `json:"source_id"`
	TargetID     string    `json:"target_id"`
	Properties   map[string]any `json:"properties,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Tag is a label that can be attached to assets. Classifications (PII, GDPR,
// HIPAA) are tags with a reserved namespace ("class:pii").
type Tag struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Color    string `json:"color,omitempty"`
}
