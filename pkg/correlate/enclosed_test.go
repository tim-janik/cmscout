// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package correlate

import (
	"testing"

	"cmscout/pkg/ir"
)

func mkBlock(id string, kind ir.BlockKind, source string) *ir.SemanticBlock {
	start := uint(0)
	if len(source) > 0 {
		start = 1
	}
	return &ir.SemanticBlock{
		ID:     id,
		Kind:   kind,
		Name:   id,
		Source: source,
		Span:   ir.SourceSpan{StartByte: start, EndByte: uint(len(source)) + 1},
	}
}

func mkComment(id, source string, start, end uint) *ir.SemanticBlock {
	return &ir.SemanticBlock{
		ID:     id,
		Kind:   ir.KindComment,
		Source: source,
		Span:   ir.SourceSpan{StartByte: start, EndByte: end},
	}
}

func pairNames(pairs []ir.CorrelatedPair) map[string]bool {
	names := make(map[string]bool)
	for _, p := range pairs {
		if p.Old != nil {
			names[p.Old.ID] = true
		}
		if p.New != nil {
			names[p.New.ID] = true
		}
	}
	return names
}

func TestSuppressEnclosedComments_MatchedInsideSameFunction(t *testing.T) {
	oldFn := mkBlock("old:fn", ir.KindFunction, "void f() {\n  // c\n}\n")
	newFn := mkBlock("new:fn", ir.KindFunction, "void f() {\n  // c\n}\n")
	oldC := mkComment("old:c", "// c", 11, 16)
	newC := mkComment("new:c", "// c", 11, 16)

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: oldFn, New: newFn, MatchType: ir.MatchExactName},
		{Old: oldC, New: newC, MatchType: ir.MatchExactName},
	}}
	SuppressEnclosedComments(result)

	names := pairNames(result.Pairs)
	if !names["old:fn"] {
		t.Error("function pair must survive")
	}
	if names["old:c"] || names["new:c"] {
		t.Errorf("comment inside the same matched function pair must be suppressed, got: %v", names)
	}
}

func TestSuppressEnclosedComments_MovedBetweenDifferentContainerPairsKept(t *testing.T) {
	// Comment inside an old function (removed) and moved to a new function (added):
	// container pairs differ, so the comment pair must stay visible.
	oldFn := mkBlock("old:fn", ir.KindFunction, "void f() {\n  // c\n}\n")
	newFn := mkBlock("new:fn", ir.KindFunction, "void h() {\n  // c\n}\n")
	oldC := mkComment("old:c", "// c", 11, 16)
	newC := mkComment("new:c", "// c", 11, 16)

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: oldFn, MatchType: ir.MatchNone},                 // removed function
		{New: newFn, MatchType: ir.MatchNone},                 // added function
		{Old: oldC, New: newC, MatchType: ir.MatchSimilarity}, // comment moved between them
	}}
	SuppressEnclosedComments(result)

	names := pairNames(result.Pairs)
	if !names["old:c"] || !names["new:c"] {
		t.Errorf("comment moved between different container pairs must stay visible, got: %v", names)
	}
}

func TestSuppressEnclosedComments_AddedInsideAddedFunction(t *testing.T) {
	newFn := mkBlock("new:fn", ir.KindFunction, "void f() {\n  // c\n}\n")
	newC := mkComment("new:c", "// c", 11, 16)

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{New: newFn, MatchType: ir.MatchNone},
		{New: newC, MatchType: ir.MatchNone},
	}}
	SuppressEnclosedComments(result)

	names := pairNames(result.Pairs)
	if !names["new:fn"] {
		t.Error("function pair must survive")
	}
	if names["new:c"] {
		t.Errorf("added comment inside an added function must be suppressed, got: %v", names)
	}
}

func TestSuppressEnclosedComments_RemovedInsideRemovedFunction(t *testing.T) {
	oldFn := mkBlock("old:fn", ir.KindFunction, "void f() {\n  // c\n}\n")
	oldC := mkComment("old:c", "// c", 11, 16)

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: oldFn, MatchType: ir.MatchNone},
		{Old: oldC, MatchType: ir.MatchNone},
	}}
	SuppressEnclosedComments(result)

	names := pairNames(result.Pairs)
	if !names["old:fn"] {
		t.Error("function pair must survive")
	}
	if names["old:c"] {
		t.Errorf("removed comment inside a removed function must be suppressed, got: %v", names)
	}
}

func TestSuppressEnclosedComments_TopLevelCommentKept(t *testing.T) {
	oldFn := mkBlock("old:fn", ir.KindFunction, "void f() {}\n")
	newFn := mkBlock("new:fn", ir.KindFunction, "void f() {}\n")
	// Comment strictly before the function: EndByte < function StartByte.
	oldC := mkComment("old:c", "// top", 0, 6)
	newC := mkComment("new:c", "// top", 0, 6)

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: oldFn, New: newFn, MatchType: ir.MatchExactName},
		{Old: oldC, New: newC, MatchType: ir.MatchExactName},
	}}
	SuppressEnclosedComments(result)

	names := pairNames(result.Pairs)
	if !names["old:c"] || !names["new:c"] {
		t.Errorf("prefix comment before a function must stay visible, got: %v", names)
	}
}

func TestSuppressEnclosedComments_ContainerNotInResultKept(t *testing.T) {
	// Old comment inside a function that is NOT part of the correlation
	// (e.g. the whole function was folded into a parent pair).
	oldC := mkComment("old:c", "// c", 11, 16)
	newC := mkComment("new:c", "// c", 11, 16)

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: oldC, New: newC, MatchType: ir.MatchExactName},
	}}
	SuppressEnclosedComments(result)

	names := pairNames(result.Pairs)
	if !names["old:c"] || !names["new:c"] {
		t.Errorf("comment with no container pair in the result must stay visible, got: %v", names)
	}
}
