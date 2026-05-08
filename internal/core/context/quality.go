package context

import (
	"context"
	"math"
	"time"

	"github.com/Satyaamm/plowered/internal/core/graph"
)

// QualityAgent computes a 0–100 trust score for an asset from observable
// signals. It is deterministic and does not call an LLM — quality should be
// auditable and reproducible.
//
// Components and weights:
//
//	has_description     (0.20) — human-authored description, length > 0
//	has_owner           (0.15) — at least one registered owner
//	freshness           (0.20) — recency of last update relative to age
//	downstream_usage    (0.20) — downstream count, log-scaled, capped
//	tag_coverage        (0.10) — at least one tag
//	classification      (0.15) — at least one classification tag (pii/gdpr/etc)
//
// Weights sum to 1.0.
type QualityAgent struct {
	Now func() time.Time
}

func NewQualityAgent() *QualityAgent { return &QualityAgent{Now: time.Now} }

func (QualityAgent) Name() string    { return "quality" }
func (QualityAgent) Version() string { return "v1" }

// Score returns the score and the per-component contributions for transparency.
func (a *QualityAgent) Score(_ context.Context, summary AssetSummary) (int, map[string]float32) {
	now := a.Now
	if now == nil {
		now = time.Now
	}

	asset := summary.Asset
	components := map[string]float32{
		"has_description":  boolScore(asset != nil && asset.Description != ""),
		"has_owner":        boolScore(summary.OwnerCount > 0),
		"freshness":        freshnessScore(asset, now()),
		"downstream_usage": downstreamScore(summary.DownstreamCount),
		"tag_coverage":     boolScore(asset != nil && len(asset.Tags) > 0),
		"classification":   classificationScore(asset),
	}

	weights := map[string]float32{
		"has_description":  0.20,
		"has_owner":        0.15,
		"freshness":        0.20,
		"downstream_usage": 0.20,
		"tag_coverage":     0.10,
		"classification":   0.15,
	}

	var total float32
	for k, v := range components {
		total += v * weights[k]
	}
	return int(math.Round(float64(total * 100))), components
}

// boolScore turns a bool into 0.0 or 1.0.
func boolScore(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// freshnessScore decays linearly over 90 days. An asset updated today scores
// 1.0; an asset older than 90 days scores 0.0. Returns 0.5 for assets with
// no updated_at (unknown freshness).
func freshnessScore(a *graph.Asset, now time.Time) float32 {
	if a == nil || a.UpdatedAt.IsZero() {
		return 0.5
	}
	days := now.Sub(a.UpdatedAt).Hours() / 24
	if days <= 0 {
		return 1
	}
	if days >= 90 {
		return 0
	}
	return float32(1 - days/90)
}

// downstreamScore log-scales downstream count and caps at 1.0. Anything with
// 50+ downstream assets is treated as "well used".
func downstreamScore(n int) float32 {
	if n <= 0 {
		return 0
	}
	v := math.Log1p(float64(n)) / math.Log1p(50)
	if v > 1 {
		v = 1
	}
	return float32(v)
}

func classificationScore(a *graph.Asset) float32 {
	if a == nil {
		return 0
	}
	for _, t := range a.Tags {
		if len(t) > 6 && t[:6] == "class:" {
			return 1
		}
	}
	return 0
}
