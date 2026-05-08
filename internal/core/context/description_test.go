package context_test

import (
	"context"
	"strings"
	"testing"
	"time"

	pcontext "github.com/Satyaamm/plowered/internal/core/context"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/pkg/llm/mock"
)

func TestDescriptionAgentHappyPath(t *testing.T) {
	mp := mock.New()
	mp.Queue(mock.Reply{
		Content:      "Customer orders aggregated daily for revenue analytics.",
		InputTokens:  120,
		OutputTokens: 18,
	})

	sink := pcontext.NewMemoryEvalSink()
	a := pcontext.NewDescriptionAgent(mp, "small-1", sink)

	got, err := a.Propose(context.Background(), pcontext.AssetSummary{
		Asset: &graph.Asset{
			ID: "asset-1", QualifiedName: "warehouse://mart.orders",
			Type: graph.AssetTypeTable, Name: "orders",
			Tags: []string{"daily", "class:pii"},
		},
		DownstreamCount: 5,
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if !strings.Contains(got, "orders") {
		t.Errorf("output missing context: %q", got)
	}

	// One call captured.
	calls := mp.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	if !strings.Contains(calls[0].System, "asset_metadata") {
		t.Errorf("system prompt missing safety wrapper")
	}
	if !strings.Contains(calls[0].Messages[0].Content, "<asset_metadata>") {
		t.Errorf("user prompt missing <asset_metadata> wrapping")
	}

	// Eval recorded.
	evals := sink.All()
	if len(evals) != 1 || evals[0].Disposition != pcontext.DispositionPending {
		t.Errorf("evals = %+v", evals)
	}
	if evals[0].TokensIn != 120 || evals[0].TokensOut != 18 {
		t.Errorf("token counts not propagated: %+v", evals[0])
	}
}

func TestDescriptionAgentRejectsMissingAsset(t *testing.T) {
	a := pcontext.NewDescriptionAgent(mock.New(), "any", nil)
	_, err := a.Propose(context.Background(), pcontext.AssetSummary{})
	if err == nil {
		t.Error("want error when asset is nil")
	}
}

func TestDescriptionAgentStripsFencing(t *testing.T) {
	mp := mock.New()
	mp.Queue(mock.Reply{Content: "```\nDaily revenue table.\n```"})
	a := pcontext.NewDescriptionAgent(mp, "small-1", pcontext.NewMemoryEvalSink())
	got, err := a.Propose(context.Background(), pcontext.AssetSummary{
		Asset: &graph.Asset{ID: "x", QualifiedName: "x", Type: graph.AssetTypeTable, Name: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Daily revenue table." {
		t.Errorf("fencing not stripped: %q", got)
	}
}

func TestQualityAgentScoring(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a := &pcontext.QualityAgent{Now: func() time.Time { return now }}

	// Fresh, well-described, owned, used asset.
	good := pcontext.AssetSummary{
		Asset: &graph.Asset{
			QualifiedName: "warehouse://orders",
			Type:          graph.AssetTypeTable,
			Description:   "Orders table",
			Tags:          []string{"daily", "class:pii"},
			UpdatedAt:     now.Add(-24 * time.Hour),
		},
		OwnerCount:      2,
		DownstreamCount: 30,
	}
	score, components := a.Score(context.Background(), good)
	if score < 80 {
		t.Errorf("good asset score = %d, want ≥ 80; components=%+v", score, components)
	}

	// Bare-minimum asset: no desc, no owner, stale, unused.
	bad := pcontext.AssetSummary{
		Asset: &graph.Asset{
			QualifiedName: "warehouse://stale",
			Type:          graph.AssetTypeTable,
			UpdatedAt:     now.Add(-365 * 24 * time.Hour),
		},
	}
	score2, _ := a.Score(context.Background(), bad)
	if score2 > 20 {
		t.Errorf("bad asset score = %d, want ≤ 20", score2)
	}
}
