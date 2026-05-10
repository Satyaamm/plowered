package pipeline

import (
	"errors"
	"fmt"
)

// ErrCycle indicates the task graph contains a cycle and cannot be ordered.
var ErrCycle = errors.New("pipeline: dependency cycle detected")

// TopologicalSort returns task IDs grouped by depth level. Tasks at the same
// depth share no dependency relationship and may run in parallel.
//
// Returns ErrCycle if any cycle is detected; returns an error naming any
// dangling dependency (task references an ID not in the pipeline).
func TopologicalSort(tasks []Task) ([][]string, error) {
	idSet := make(map[string]Task, len(tasks))
	for _, t := range tasks {
		if _, dup := idSet[t.ID]; dup {
			return nil, fmt.Errorf("pipeline: duplicate task id %q", t.ID)
		}
		idSet[t.ID] = t
	}
	for _, t := range tasks {
		for _, d := range t.DependsOn {
			if _, ok := idSet[d]; !ok {
				return nil, fmt.Errorf("pipeline: task %q depends on unknown id %q", t.ID, d)
			}
		}
	}

	indeg := make(map[string]int, len(tasks))
	for _, t := range tasks {
		indeg[t.ID] = len(t.DependsOn)
	}
	successors := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		for _, d := range t.DependsOn {
			successors[d] = append(successors[d], t.ID)
		}
	}

	var levels [][]string
	remaining := len(tasks)
	for remaining > 0 {
		var level []string
		for id, deg := range indeg {
			if deg == 0 {
				level = append(level, id)
			}
		}
		if len(level) == 0 {
			return nil, ErrCycle
		}
		// stable ordering for determinism
		sortStrings(level)
		levels = append(levels, level)
		for _, id := range level {
			delete(indeg, id)
			for _, s := range successors[id] {
				indeg[s]--
			}
		}
		remaining -= len(level)
	}
	return levels, nil
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
