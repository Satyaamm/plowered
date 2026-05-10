package pipeline_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
)

func TestTopologicalSort(t *testing.T) {
	tasks := []pipeline.Task{
		{ID: "extract"},
		{ID: "transform", DependsOn: []string{"extract"}},
		{ID: "load", DependsOn: []string{"transform"}},
		{ID: "quality_check", DependsOn: []string{"load"}},
		{ID: "notify", DependsOn: []string{"quality_check"}},
	}
	got, err := pipeline.TopologicalSort(tasks)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	want := [][]string{
		{"extract"},
		{"transform"},
		{"load"},
		{"quality_check"},
		{"notify"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTopologicalSortDetectsCycle(t *testing.T) {
	tasks := []pipeline.Task{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	if _, err := pipeline.TopologicalSort(tasks); !errors.Is(err, pipeline.ErrCycle) {
		t.Errorf("want ErrCycle, got %v", err)
	}
}

func TestTopologicalSortDetectsDanglingDep(t *testing.T) {
	tasks := []pipeline.Task{{ID: "a", DependsOn: []string{"missing"}}}
	if _, err := pipeline.TopologicalSort(tasks); err == nil {
		t.Error("expected error on dangling dependency")
	}
}

func TestTopologicalSortDetectsDuplicateID(t *testing.T) {
	tasks := []pipeline.Task{{ID: "a"}, {ID: "a"}}
	if _, err := pipeline.TopologicalSort(tasks); err == nil {
		t.Error("expected error on duplicate task id")
	}
}

func TestTopologicalSortGroupsParallelLevels(t *testing.T) {
	tasks := []pipeline.Task{
		{ID: "root"},
		{ID: "left", DependsOn: []string{"root"}},
		{ID: "right", DependsOn: []string{"root"}},
		{ID: "join", DependsOn: []string{"left", "right"}},
	}
	got, err := pipeline.TopologicalSort(tasks)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d levels, want 3", len(got))
	}
	if got[0][0] != "root" {
		t.Errorf("level 0 = %v", got[0])
	}
	if !reflect.DeepEqual(got[1], []string{"left", "right"}) {
		t.Errorf("level 1 = %v, want [left right]", got[1])
	}
	if got[2][0] != "join" {
		t.Errorf("level 2 = %v", got[2])
	}
}

func TestRetryPolicyBackoff(t *testing.T) {
	p := pipeline.RetryPolicy{
		MaxAttempts:    4,
		InitialBackoff: 1,
		Multiplier:     2,
		MaxBackoff:     16,
	}
	cases := []struct {
		attempt int
		want    int64
	}{
		{1, 0},
		{2, 1},
		{3, 2},
		{4, 4},
	}
	for _, c := range cases {
		if got := p.BackoffFor(c.attempt).Nanoseconds(); got != c.want {
			t.Errorf("BackoffFor(%d) = %d, want %d", c.attempt, got, c.want)
		}
	}
}
