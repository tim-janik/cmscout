package metrics

import (
	"context"
	"testing"

	"cmscout/pkg/analysis"
)

func TestCompare_missing_file(t *testing.T) {
	snapshot := measurement(t, "a.js", "// doc\nfunction f(x) { const inner = () => x ? 1 : 0; }\n")
	for _, added := range []bool{true, false} {
		before, after := snapshot, snapshot
		if added {
			before = nil
		} else {
			after = nil
		}
		result, err := Compare(before, after)
		if err != nil || result.Status != "complete" || len(result.Changes) != len(snapshot.Components) {
			t.Fatalf("missing file comparison: %+v %v", result, err)
		}
		if result.Before != before || result.After != after {
			t.Fatal("invented an absent snapshot")
		}
		for _, change := range result.Changes {
			if change.Added != added || change.Removed == added || !change.DirectChanged || change.Delta.Cyclomatic != nil {
				t.Fatalf("wrong whole-file change: %+v", change)
			}
			if added && (change.BeforeName != nil || change.AfterName == nil || len(change.AfterRanges) == 0) ||
				!added && (change.BeforeName == nil || change.AfterName != nil || len(change.BeforeRanges) == 0) {
				t.Fatalf("wrong absent reference or range: %+v", change)
			}
		}
	}
}

func TestCompare_missing_file_unknown_and_empty(t *testing.T) {
	ast, err := analysis.Parse(context.Background(), []byte("function f() {"), "a.js")
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()
	snapshot, err := Measure(ast, Options{Namespace: "test", Path: "a.js"})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := Compare(nil, snapshot)
	if err != nil || partial.Status != "partial" {
		t.Fatalf("partial addition: %+v %v", partial, err)
	}
	for _, change := range partial.Changes {
		if change.Added || change.Removed || change.Match.Status != "unavailable" || change.Delta.Status != "unavailable" {
			t.Fatalf("partial inventory gave trusted changes: %+v", change)
		}
	}
	empty, err := Compare(nil, measurement(t, "a.js", ""))
	if err != nil || empty.Status != "complete" || len(empty.Changes) != 1 || !empty.Changes[0].Added {
		t.Fatalf("empty source file lost: %+v %v", empty, err)
	}
	if _, err := Compare(nil, nil); err == nil {
		t.Fatal("accepted two absent files")
	}
}
