package context_test

import (
	"context"
	"strings"
	"testing"

	pcontext "github.com/Satyaamm/plowered/internal/core/context"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/pkg/llm/mock"
)

func TestGlossaryAgentParsesProposals(t *testing.T) {
	mp := mock.New()
	mp.Queue(mock.Reply{
		Content: `{
  "proposals": [
    {
      "term": "Active User",
      "definition": "A user with at least one event in the past 30 days.",
      "domain": "growth",
      "linked_asset_qns": ["wh://prod/mart/active_users", "wh://prod/raw/events"],
      "confidence": 0.9
    }
  ]
}`,
		InputTokens:  250,
		OutputTokens: 90,
	})

	sink := pcontext.NewMemoryEvalSink()
	a := pcontext.NewGlossaryAgent(mp, "small-1", sink)

	got, err := a.Propose(context.Background(), []*graph.Asset{
		{QualifiedName: "wh://prod/mart/active_users", Type: graph.AssetTypeTable, Name: "active_users",
			Description: "Users with events in the last 30 days."},
		{QualifiedName: "wh://prod/raw/events", Type: graph.AssetTypeTable, Name: "events",
			Description: "User-level event stream."},
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(got) != 1 || got[0].Term != "Active User" {
		t.Fatalf("got = %+v", got)
	}
	if len(got[0].LinkedAssetQNs) != 2 {
		t.Errorf("linked qns = %v", got[0].LinkedAssetQNs)
	}

	calls := mp.Calls()
	if !strings.Contains(calls[0].Messages[0].Content, "<assets>") {
		t.Error("user prompt missing <assets> wrapping")
	}

	if len(sink.All()) != 1 {
		t.Errorf("eval not recorded")
	}
}

func TestGlossaryAgentSkipsEmptyDescriptions(t *testing.T) {
	mp := mock.New()
	mp.Queue(mock.Reply{Content: `{"proposals": []}`})
	a := pcontext.NewGlossaryAgent(mp, "x", pcontext.NewMemoryEvalSink())

	_, err := a.Propose(context.Background(), []*graph.Asset{
		{QualifiedName: "x", Type: graph.AssetTypeTable, Name: "x"}, // no description
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := mp.Calls()
	if !strings.Contains(calls[0].Messages[0].Content, "<assets>") {
		t.Error("expected <assets> tag")
	}
	// Asset without description should be skipped from the prompt body.
	if strings.Contains(calls[0].Messages[0].Content, "qualified_name: x") {
		t.Error("asset without description should be skipped")
	}
}

func TestGlossaryAgentEmptyInput(t *testing.T) {
	mp := mock.New()
	a := pcontext.NewGlossaryAgent(mp, "x", pcontext.NewMemoryEvalSink())
	got, err := a.Propose(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v", got)
	}
	if len(mp.Calls()) != 0 {
		t.Error("provider should not be called on empty input")
	}
}
