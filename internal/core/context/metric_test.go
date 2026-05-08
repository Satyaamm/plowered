package context_test

import (
	"context"
	"strings"
	"testing"
	"time"

	pcontext "github.com/Satyaamm/plowered/internal/core/context"
	"github.com/Satyaamm/plowered/pkg/llm/mock"
)

func TestMetricAgentParsesProposals(t *testing.T) {
	mp := mock.New()
	mp.Queue(mock.Reply{
		Content: `{
  "proposals": [
    {
      "name": "Daily Revenue",
      "description": "Total order amount per day.",
      "formula": "SUM(amount)",
      "aggregation": "sum",
      "dimensions": ["date"],
      "confidence": 0.85
    }
  ]
}`,
		InputTokens:  300,
		OutputTokens: 80,
	})

	sink := pcontext.NewMemoryEvalSink()
	a := pcontext.NewMetricAgent(mp, "small-1", sink)

	got, err := a.Propose(context.Background(), []pcontext.QueryPattern{
		{SQL: "SELECT date, SUM(amount) FROM orders GROUP BY date", OccurrenceCount: 12, LastSeen: time.Now()},
		{SQL: "SELECT order_date, SUM(total) FROM mart.orders GROUP BY order_date", OccurrenceCount: 8, LastSeen: time.Now()},
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("proposals = %d, want 1", len(got))
	}
	if got[0].Aggregation != "sum" {
		t.Errorf("aggregation = %q", got[0].Aggregation)
	}
	if got[0].Confidence < 0.8 {
		t.Errorf("confidence = %v", got[0].Confidence)
	}

	// Eval recorded with structured output.
	evals := sink.All()
	if len(evals) != 1 || evals[0].Agent != "metric" {
		t.Errorf("evals = %+v", evals)
	}

	// Prompt safety: untrusted data wrapped in tags.
	calls := mp.Calls()
	if !strings.Contains(calls[0].Messages[0].Content, "<queries>") {
		t.Error("user prompt missing <queries> wrapping")
	}
}

func TestMetricAgentEmptyInput(t *testing.T) {
	mp := mock.New()
	a := pcontext.NewMetricAgent(mp, "x", pcontext.NewMemoryEvalSink())
	got, err := a.Propose(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty proposals, got %v", got)
	}
	if len(mp.Calls()) != 0 {
		t.Error("provider should not be called on empty input")
	}
}

func TestMetricAgentRejectsBadJSON(t *testing.T) {
	mp := mock.New()
	mp.Queue(mock.Reply{Content: "not json"})
	a := pcontext.NewMetricAgent(mp, "x", pcontext.NewMemoryEvalSink())
	_, err := a.Propose(context.Background(), []pcontext.QueryPattern{
		{SQL: "SELECT 1", OccurrenceCount: 1, LastSeen: time.Now()},
	})
	if err == nil {
		t.Error("expected parse error")
	}
}
