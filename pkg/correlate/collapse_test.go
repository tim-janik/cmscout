// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package correlate

import (
	"fmt"
	"strings"
	"testing"

	"cmscout/pkg/ir"
	"cmscout/pkg/matching"
)

func TestNumberAnonymousBlocks_PerSide(t *testing.T) {
	// Anonymous arrows use matching per-side ordinals.
	mkArrow := func(id string, pos uint) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID:   id,
			Kind: ir.KindArrowFunc,
			Name: "",
			Span: ir.SourceSpan{StartByte: pos},
		}
	}
	old1, old2 := mkArrow("old:a1:10", 10), mkArrow("old:a2:50", 50)
	new1, new2 := mkArrow("new:a1:12", 12), mkArrow("new:a2:52", 52)

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: old1, New: new1, Confidence: 0.9},
		{Old: old2, New: new2, Confidence: 0.9},
	}}

	numberAnonymousBlocks(result)

	if old1.Name != new1.Name {
		t.Errorf("first arrow: old=%q new=%q (expected equal)", old1.Name, new1.Name)
	}
	if old2.Name != new2.Name {
		t.Errorf("second arrow: old=%q new=%q (expected equal)", old2.Name, new2.Name)
	}
	if old1.Name == old2.Name {
		t.Errorf("distinct arrows must have distinct names, both %q", old1.Name)
	}
}

func TestIsCollapsibleChild(t *testing.T) {
	collapsible := []ir.BlockKind{
		ir.KindMethod, ir.KindLifecycle, ir.KindFunction,
		ir.KindArrowFunc, ir.KindObjectMethod, ir.KindJSX, ir.KindLambda,
	}
	notCollapsible := []ir.BlockKind{
		ir.KindConstant, ir.KindVariable, ir.KindImport,
		ir.KindExport, ir.KindComment, ir.KindClass,
	}
	for _, k := range collapsible {
		if !isCollapsibleChild(k) {
			t.Errorf("expected %s to be collapsible", k)
		}
	}
	for _, k := range notCollapsible {
		if isCollapsibleChild(k) {
			t.Errorf("expected %s to NOT be collapsible", k)
		}
	}
}

func TestCollapseMatchedSubBlocks(t *testing.T) {
	src := `class BKnob {
  other() {}
  pointerdown(event) {}
  more() {}
}`
	matchedMethod := ir.SemanticBlock{
		ID:     "method:pointerdown:29",
		Kind:   ir.KindMethod,
		Name:   "pointerdown",
		Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 29, EndByte: 50}, // "pointerdown(event) {}"
		Source: "pointerdown(event) {}",
	}
	unmatchedClass := ir.SemanticBlock{
		ID:     "class:BKnob:0",
		Kind:   ir.KindClass,
		Name:   "BKnob",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(src))},
		Source: src,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &matchedMethod, New: &matchedMethod, Confidence: 0.9},
			{Old: &unmatchedClass, New: nil, Confidence: 0.0},
		},
	}

	CollapseMatchedSubBlocks(result)

	got := result.Pairs[1].Old.Source
	if !strings.Contains(got, "// [matched: method pointerdown]") {
		t.Errorf("class body should have reference, got:\n%s", got)
	}
	if strings.Contains(got, "pointerdown(event)") {
		t.Error("matched method source should have been replaced")
	}
}

func TestCollapseMatchedSubBlocks_AddedAndRemoved(t *testing.T) {
	// Added and removed parents use the new side's canonical child reference.
	oldSrc := "class BKnob {\nrelabel() {}\nother()\n}"
	newSrc := "function Knob() {\nfunction relabel() {}\nnewThing()\n}"

	oldMethod := ir.SemanticBlock{
		ID:     "method:relabel:14",
		Kind:   ir.KindMethod,
		Name:   "relabel",
		Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 14, EndByte: 26},
		Source: "relabel() {}",
	}
	oldClass := ir.SemanticBlock{
		ID:     "class:BKnob:0",
		Kind:   ir.KindClass,
		Name:   "BKnob",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}

	newMethod := ir.SemanticBlock{
		ID:     "function:relabel:18",
		Kind:   ir.KindFunction,
		Name:   "relabel",
		Parent: "function:Knob:0",
		Span:   ir.SourceSpan{StartByte: 18, EndByte: 39},
		Source: "function relabel() {}",
	}
	newClass := ir.SemanticBlock{
		ID:     "function:Knob:0",
		Kind:   ir.KindFunction,
		Name:   "Knob",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &oldMethod, New: &newMethod, Confidence: 0.5},
			{Old: &oldClass, New: nil, Confidence: 0.0},
			{Old: nil, New: &newClass, Confidence: 0.0},
		},
	}

	CollapseMatchedSubBlocks(result)

	if !strings.Contains(result.Pairs[1].Old.Source, "// [matched: function relabel]") {
		t.Errorf("old class should have the canonical reference, got:\n%s", result.Pairs[1].Old.Source)
	}
	if !strings.Contains(result.Pairs[2].New.Source, "// [matched: function relabel]") {
		t.Errorf("new class should have the canonical reference, got:\n%s", result.Pairs[2].New.Source)
	}
}

func TestCollapseMatchedSubBlocks_MatchedParent(t *testing.T) {
	// Matched converted parents still collapse their matched children.
	oldSrc := "class BKnob {\n  reposition() {}\n  relabel() {}\n}"
	newSrc := "function Knob() {\n  function reposition() {}\n  function relabel() {}\n}"

	oldReposition := ir.SemanticBlock{
		ID:     "method:reposition:16",
		Kind:   ir.KindMethod,
		Name:   "reposition",
		Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 16, EndByte: 31},
		Source: "reposition() {}",
	}
	oldRelabel := ir.SemanticBlock{
		ID:     "method:relabel:34",
		Kind:   ir.KindMethod,
		Name:   "relabel",
		Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 34, EndByte: 46},
		Source: "relabel() {}",
	}
	oldClass := ir.SemanticBlock{
		ID:     "class:BKnob:0",
		Kind:   ir.KindClass,
		Name:   "BKnob",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}

	newReposition := ir.SemanticBlock{
		ID:     "function:reposition:20",
		Kind:   ir.KindFunction,
		Name:   "reposition",
		Parent: "function:Knob:0",
		Span:   ir.SourceSpan{StartByte: 20, EndByte: 44},
		Source: "function reposition() {}",
	}
	newRelabel := ir.SemanticBlock{
		ID:     "function:relabel:47",
		Kind:   ir.KindFunction,
		Name:   "relabel",
		Parent: "function:Knob:0",
		Span:   ir.SourceSpan{StartByte: 47, EndByte: 68},
		Source: "function relabel() {}",
	}
	newClass := ir.SemanticBlock{
		ID:     "function:Knob:0",
		Kind:   ir.KindFunction,
		Name:   "Knob",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &oldClass, New: &newClass, Confidence: 0.6},
			{Old: &oldReposition, New: &newReposition, Confidence: 0.9},
			{Old: &oldRelabel, New: &newRelabel, Confidence: 0.9},
		},
	}

	CollapseMatchedSubBlocks(result)

	if got := result.Pairs[0].Old.Source; !strings.Contains(got, "// [matched: function relabel]") ||
		!strings.Contains(got, "// [matched: function reposition]") ||
		strings.Contains(got, "relabel() {}") {
		t.Errorf("matched old parent should have canonical reference comments, got:\n%s", got)
	}
	if got := result.Pairs[0].New.Source; !strings.Contains(got, "// [matched: function relabel]") ||
		!strings.Contains(got, "// [matched: function reposition]") ||
		strings.Contains(got, "function relabel() {}") {
		t.Errorf("matched new parent should have canonical reference comments, got:\n%s", got)
	}
}

func TestCollapse_MatchedParentCanonicalRefs(t *testing.T) {
	// Collapsed parents use the new side's anonymous identity on both sides.
	arrowText := "() => {\n    pending = null;\n  }"
	oldSrc := "function spin() {\n  x = requestAnimationFrame (\n    " + arrowText + "\n  );\n}\n"
	newSrc := oldSrc
	arrowStart := strings.Index(oldSrc, "() => {")
	if arrowStart < 0 {
		t.Fatal("arrow text not found in parent source")
	}

	oldArrow := ir.SemanticBlock{
		ID:     "arrow:old:03",
		Kind:   ir.KindArrowFunc,
		Name:   "arrow_function.03", // old-side numbering
		Parent: "fn:spin:0",
		Span:   ir.SourceSpan{StartByte: uint(arrowStart), EndByte: uint(arrowStart + len(arrowText))},
		Source: arrowText,
	}
	newArrow := ir.SemanticBlock{
		ID:     "arrow:new:01",
		Kind:   ir.KindArrowFunc,
		Name:   "arrow_function.01", // new-side numbering
		Parent: "fn:spin:0",
		Span:   ir.SourceSpan{StartByte: uint(arrowStart), EndByte: uint(arrowStart + len(arrowText))},
		Source: arrowText,
	}
	oldParent := ir.SemanticBlock{
		ID:     "fn:spin:0",
		Kind:   ir.KindFunction,
		Name:   "spin",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}
	newParent := ir.SemanticBlock{
		ID:     "fn:spin:0",
		Kind:   ir.KindFunction,
		Name:   "spin",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &oldParent, New: &newParent, Confidence: 1.0},
			{Old: &oldArrow, New: &newArrow, Confidence: 1.0},
		},
	}

	CollapseMatchedSubBlocks(result)

	oldCollapsed, newCollapsed := result.Pairs[0].Old.Source, result.Pairs[0].New.Source
	if oldCollapsed != newCollapsed {
		t.Errorf("collapsed parents must be identical:\nold:\n%s\nnew:\n%s", oldCollapsed, newCollapsed)
	}
	if !strings.Contains(oldCollapsed, "// [matched: arrow_function arrow_function.01]") {
		t.Errorf("reference must use the new side's identity, got:\n%s", oldCollapsed)
	}
	if strings.Contains(oldCollapsed, "arrow_function.03") {
		t.Errorf("old side's per-side number must not leak into the reference:\n%s", oldCollapsed)
	}
	if matching.PairSimilarity(&result.Pairs[0]) != 1.0 {
		t.Errorf("identical collapsed parents must report 100%% similarity, got %v", matching.PairSimilarity(&result.Pairs[0]))
	}
}

// TestCollapse_FoldRequiresOwnLine rejects mid-expression reference folding.
func TestCollapse_FoldRequiresOwnLine(t *testing.T) {
	// An arrow nested inside a function call (mid-line) must stay inline.
	midSrc := "function spin() {\n  x = requestAnimationFrame (() => {\n    pending = null;\n  });\n}\n"
	arrowText := "() => {\n    pending = null;\n  }"
	midStart := strings.Index(midSrc, arrowText)
	arrow := ir.SemanticBlock{
		ID:     "arrow:a1:" + fmt.Sprint(midStart),
		Kind:   ir.KindArrowFunc,
		Name:   "arrow_function.01",
		Parent: "fn:spin:0",
		Span:   ir.SourceSpan{StartByte: uint(midStart), EndByte: uint(midStart + len(arrowText))},
		Source: arrowText,
	}
	parent := ir.SemanticBlock{
		ID:     "fn:spin:0",
		Kind:   ir.KindFunction,
		Name:   "spin",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(midSrc))},
		Source: midSrc,
	}
	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &parent, New: &parent, Confidence: 1.0},
			{Old: &arrow, New: &arrow, Confidence: 1.0},
		},
	}

	CollapseMatchedSubBlocks(result)

	collapsed := result.Pairs[0].Old.Source
	if strings.Contains(collapsed, "// [matched: arrow_function") {
		t.Errorf("mid-line arrow must not be folded into an expression:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "requestAnimationFrame (() => {") {
		t.Errorf("mid-line arrow must stay inline in the expression:\n%s", collapsed)
	}
}

func TestCollapseMatchedSubBlocks_NoMatchedChildren(t *testing.T) {
	soloClass := ir.SemanticBlock{
		ID:     "class:Foo:0",
		Kind:   ir.KindClass,
		Name:   "Foo",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: 21},
		Source: "class Foo { bar() {} }",
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &soloClass, New: nil, Confidence: 0.0},
		},
	}

	CollapseMatchedSubBlocks(result)

	if result.Pairs[0].Old.Source != "class Foo { bar() {} }" {
		t.Error("source should be unchanged when no matched children")
	}
}

func TestCollapse_AnonymousBlockNumbering(t *testing.T) {
	// Two anonymous arrow functions should get numbered sequentially.
	arrow1 := ir.SemanticBlock{
		ID:     "arrow:a1:10",
		Kind:   ir.KindArrowFunc,
		Name:   "", // anonymous
		Parent: "fn:parent:0",
		Span:   ir.SourceSpan{StartByte: 10, EndByte: 30},
		Source: "() => doSomething()",
	}
	arrow2 := ir.SemanticBlock{
		ID:     "arrow:a2:40",
		Kind:   ir.KindArrowFunc,
		Name:   "", // anonymous
		Parent: "fn:parent:0",
		Span:   ir.SourceSpan{StartByte: 40, EndByte: 60},
		Source: "() => doOtherThing()",
	}
	parent := ir.SemanticBlock{
		ID:     "fn:parent:0",
		Kind:   ir.KindFunction,
		Name:   "parent",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: 70},
		Source: "function parent() { () => doSomething(); () => doOtherThing(); }",
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &arrow1, New: &arrow1, Confidence: 0.9},
			{Old: &arrow2, New: &arrow2, Confidence: 0.9},
			{Old: &parent, New: nil, Confidence: 0.0},
		},
	}

	CollapseMatchedSubBlocks(result)

	// Anonymous blocks should be numbered.
	if arrow1.Name == "" {
		t.Error("first anonymous arrow should have been numbered")
	}
	if arrow2.Name == "" {
		t.Error("second anonymous arrow should have been numbered")
	}
	if !strings.HasPrefix(arrow1.Name, "arrow_function.") {
		t.Errorf("expected arrow_function prefix, got %q", arrow1.Name)
	}
	if !strings.HasPrefix(arrow2.Name, "arrow_function.") {
		t.Errorf("expected arrow_function prefix, got %q", arrow2.Name)
	}
	// Numbers should differ.
	if arrow1.Name == arrow2.Name {
		t.Errorf("anonymous blocks should have distinct names, both are %q", arrow1.Name)
	}
}

func TestCollapse_PrefixCommentAbsorption(t *testing.T) {
	src := `class BKnob {
  // This is a prefix comment for pointerdown
  pointerdown(event) {}
  other() {}
}`
	commentText := "// This is a prefix comment for pointerdown"
	methodText := "pointerdown(event) {}"
	commentStart := strings.Index(src, commentText)
	methodStart := strings.Index(src, "pointerdown(event)")

	mkComment := func(id, parent string, start uint) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindComment, Name: "", Parent: parent,
			Span:   ir.SourceSpan{StartByte: start, EndByte: start + uint(len(commentText))},
			Source: commentText,
		}
	}
	mkMethod := func(id, parent string, start uint) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindMethod, Name: "pointerdown", Parent: parent,
			Span:   ir.SourceSpan{StartByte: start, EndByte: start + uint(len(methodText))},
			Source: methodText,
		}
	}
	mkClass := func(id string) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindClass, Name: "BKnob",
			Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(src))},
			Source: src,
		}
	}

	oldClass := mkClass("class:BKnob:0")
	newClass := mkClass("class:BKnob:1")
	oldComment := mkComment("comment:c1:16", "class:BKnob:0", uint(commentStart))
	newComment := mkComment("comment:c2:16", "class:BKnob:1", uint(commentStart))
	oldMethod := mkMethod("method:pointerdown:62", "class:BKnob:0", uint(methodStart))
	newMethod := mkMethod("method:pointerdown:66", "class:BKnob:1", uint(methodStart))

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldClass, New: newClass, Confidence: 1.0},
			{Old: oldMethod, New: newMethod, Confidence: 0.9},
			{Old: oldComment, New: newComment, Confidence: 0.9},
		},
	}

	CollapseMatchedSubBlocks(result)

	// The class pair collapsed identically on both sides: the reference
	// replaces the method AND its prefix comment.
	var oldCollapsed, newCollapsed string
	for _, p := range result.Pairs {
		if p.Old != nil && p.Old.Kind == ir.KindClass {
			oldCollapsed = p.Old.Source
		}
		if p.New != nil && p.New.Kind == ir.KindClass {
			newCollapsed = p.New.Source
		}
	}
	if oldCollapsed == "" || newCollapsed == "" {
		t.Fatal("class pair not found in result")
	}
	if oldCollapsed != newCollapsed {
		t.Errorf("collapsed class sources must be identical:\nold:\n%s\nnew:\n%s", oldCollapsed, newCollapsed)
	}
	if !strings.Contains(oldCollapsed, "// [matched: method pointerdown]") {
		t.Errorf("class body should have the matched reference, got:\n%s", oldCollapsed)
	}
	if strings.Contains(oldCollapsed, "prefix comment for pointerdown") {
		t.Error("prefix comment should have been absorbed into the matched reference")
	}

	// The comment was absorbed on BOTH sides, so the pair is suppressed.
	for _, p := range result.Pairs {
		if (p.Old != nil && p.Old.Kind == ir.KindComment) || (p.New != nil && p.New.Kind == ir.KindComment) {
			t.Errorf("absorbed comment should have been filtered from result pairs, got %+v", p)
		}
	}
}

func TestCollapse_StandaloneDedup(t *testing.T) {
	// Distinct IDs with the same name keep their own source.
	matchedFn := ir.SemanticBlock{
		ID:     "fn:relabel:10",
		Kind:   ir.KindFunction,
		Name:   "relabel",
		Parent: "fn:Knob:0",
		Span:   ir.SourceSpan{StartByte: 10, EndByte: 30},
		Source: "function relabel() { /* impl */ }",
	}
	soloFn := ir.SemanticBlock{
		ID:     "fn:relabel:40",
		Kind:   ir.KindFunction,
		Name:   "relabel",
		Parent: "",
		Span:   ir.SourceSpan{StartByte: 40, EndByte: 60},
		Source: "function relabel() { /* other impl */ }",
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: &matchedFn, New: &matchedFn, Confidence: 0.9},
			{Old: &soloFn, New: nil, Confidence: 0.0},
		},
	}

	CollapseMatchedSubBlocks(result)

	// The standalone unmatched block is a DIFFERENT block: it must keep its
	// real source instead of being deduped into a matched reference.
	if soloFn.Source != "function relabel() { /* other impl */ }" {
		t.Errorf("standalone unmatched block must keep its real source, got: %q", soloFn.Source)
	}
}

// TestCollapse_AddedMethodNotRewritten keeps an added sibling method's source.
func TestCollapse_AddedMethodNotRewritten(t *testing.T) {
	mkMethod := func(id, name, src, parent string, start uint) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID:     id,
			Kind:   ir.KindMethod,
			Name:   name,
			Parent: parent,
			Span:   ir.SourceSpan{StartByte: start, EndByte: start + uint(len(src))},
			Source: src,
		}
	}

	oldAFoo := mkMethod("method:foo:oldA", "foo", "  foo() { return 1; }\n", "class:A:old", 8)
	newAFoo := mkMethod("method:foo:newA", "foo", "  foo() { return 1; }\n", "class:A:new", 8)
	newBFoo := mkMethod("method:foo:newB", "foo", "  foo() { return 2; }\n", "class:B:new", 8)

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldAFoo, New: newAFoo, Confidence: 1.0, MatchType: ir.MatchExactName},
			{Old: nil, New: newBFoo, Confidence: 0.0, MatchType: ir.MatchNone},
		},
	}

	CollapseMatchedSubBlocks(result)

	if newBFoo.Source != "  foo() { return 2; }\n" {
		t.Errorf("added B.foo must keep its real source, got: %q", newBFoo.Source)
	}
}

// TestCollapse_RemovedSiblingKeepsSource keeps a removed sibling's source.
func TestCollapse_RemovedSiblingKeepsSource(t *testing.T) {
	oldAFoo := &ir.SemanticBlock{
		ID: "method:foo:oldA", Kind: ir.KindMethod, Name: "foo", Parent: "class:A:old",
		Span:   ir.SourceSpan{StartByte: 8, EndByte: 8 + uint(len("  foo() { return 1; }\n"))},
		Source: "  foo() { return 1; }\n",
	}
	newAFoo := &ir.SemanticBlock{
		ID: "method:foo:newA", Kind: ir.KindMethod, Name: "foo", Parent: "class:A:new",
		Span:   ir.SourceSpan{StartByte: 8, EndByte: 8 + uint(len("  foo() { return 1; }\n"))},
		Source: "  foo() { return 1; }\n",
	}
	oldBFoo := &ir.SemanticBlock{
		ID: "method:foo:oldB", Kind: ir.KindMethod, Name: "foo", Parent: "class:B:old",
		Span:   ir.SourceSpan{StartByte: 8, EndByte: 8 + uint(len("  foo() { return 2; }\n"))},
		Source: "  foo() { return 2; }\n",
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldAFoo, New: newAFoo, Confidence: 1.0, MatchType: ir.MatchExactName},
			{Old: oldBFoo, New: nil, Confidence: 0.0, MatchType: ir.MatchNone},
		},
	}

	CollapseMatchedSubBlocks(result)

	if oldBFoo.Source != "  foo() { return 2; }\n" {
		t.Errorf("removed B.foo must keep its real source, got: %q", oldBFoo.Source)
	}
}

// TestCollapse_SameNameDifferentParentNotCollapsed keeps parent-specific IDs.
func TestCollapse_SameNameDifferentParentNotCollapsed(t *testing.T) {
	// A method and top-level function can share a name without sharing an ID.
	newSrc := "function render() { return 1; }"

	oldMethod := &ir.SemanticBlock{
		ID: "method:render:old", Kind: ir.KindMethod, Name: "render", Parent: "class:Foo:old",
		Span:   ir.SourceSpan{StartByte: 12, EndByte: 12 + uint(len("  render() { return 1; }\n"))},
		Source: "  render() { return 1; }\n",
	}
	newFn := &ir.SemanticBlock{
		ID: "function:render:new", Kind: ir.KindFunction, Name: "render", Parent: "",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldMethod, New: newFn, Confidence: 0.9, MatchType: ir.MatchSimilarity},
		},
	}

	CollapseMatchedSubBlocks(result)

	if newFn.Source != newSrc {
		t.Errorf("matched top-level function must keep its source, got: %q", newFn.Source)
	}
}

// TestCollapse_PrefixCommentAbsorptionSimilarityMatched handles reworded prefixes.
func TestCollapse_PrefixCommentAbsorptionSimilarityMatched(t *testing.T) {
	oldSrc := "class BKnob {\n  // Handles the pointer down\n  pointerdown(event) {}\n}"
	newSrc := "class BKnob {\n  // Handles the pointerdown\n  pointerdown(event) {}\n}"

	oldComment := &ir.SemanticBlock{
		ID: "comment:c1:16", Kind: ir.KindComment, Name: "", Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 16, EndByte: 43},
		Source: "// Handles the pointer down",
	}
	newComment := &ir.SemanticBlock{
		ID: "comment:c1:16", Kind: ir.KindComment, Name: "", Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 16, EndByte: 42},
		Source: "// Handles the pointerdown",
	}
	oldMethod := &ir.SemanticBlock{
		ID: "method:pointerdown:46", Kind: ir.KindMethod, Name: "pointerdown", Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 46, EndByte: 67},
		Source: "pointerdown(event) {}",
	}
	newMethod := &ir.SemanticBlock{
		ID: "method:pointerdown:45", Kind: ir.KindMethod, Name: "pointerdown", Parent: "class:BKnob:0",
		Span:   ir.SourceSpan{StartByte: 45, EndByte: 66},
		Source: "pointerdown(event) {}",
	}
	oldClass := &ir.SemanticBlock{
		ID: "class:BKnob:0", Kind: ir.KindClass, Name: "BKnob",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}
	newClass := &ir.SemanticBlock{
		ID: "class:BKnob:0", Kind: ir.KindClass, Name: "BKnob",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldClass, New: newClass, Confidence: 0.9, MatchType: ir.MatchExactName},
			{Old: oldMethod, New: newMethod, Confidence: 0.9, MatchType: ir.MatchExactName},
			{Old: oldComment, New: newComment, Confidence: 0.8, MatchType: ir.MatchSimilarity},
		},
	}

	CollapseMatchedSubBlocks(result)

	// Reworded absorbed comments remain visible beside the collapsed method.
	var commentPair *ir.CorrelatedPair
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if (p.Old != nil && p.Old.Kind == ir.KindComment) || (p.New != nil && p.New.Kind == ir.KindComment) {
			commentPair = p
		}
	}
	if commentPair == nil {
		t.Fatal("reworded prefix comment absorbed on both sides must stay in the semantic report")
	}
	if commentPair.Old == nil || commentPair.New == nil ||
		commentPair.Old.Source != "// Handles the pointer down" ||
		commentPair.New.Source != "// Handles the pointerdown" {
		t.Errorf("the surviving pair must carry both original comment texts, got %+v", commentPair)
	}
	// The class pair collapsed identically on both sides: the reference
	// replaces the method AND its prefix comment.
	if result.Pairs[0].Old.Source != result.Pairs[0].New.Source {
		t.Errorf("collapsed class sources must be identical:\nold:\n%s\nnew:\n%s",
			result.Pairs[0].Old.Source, result.Pairs[0].New.Source)
	}
	if !strings.Contains(result.Pairs[0].Old.Source, "// [matched: method pointerdown]") {
		t.Errorf("class body must contain the matched reference, got:\n%s", result.Pairs[0].Old.Source)
	}
	for _, gone := range []string{"Handles the pointer down", "Handles the pointerdown"} {
		if strings.Contains(result.Pairs[0].Old.Source, gone) {
			t.Errorf("prefix comment text must be absorbed, found %q in:\n%s", gone, result.Pairs[0].Old.Source)
		}
	}
}

func TestAbsorbPrefixComments(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantRef string // substring that must appear in output
		unwant  string // substring that must NOT appear in output
	}{
		{
			name:    "single_comment_before_ref",
			input:   "code\n// prefix comment\n// [matched: method foo]",
			wantRef: "// [matched: method foo]",
			unwant:  "prefix comment",
		},
		{
			name:    "multiple_comments_before_ref",
			input:   "code\n// comment one\n// comment two\n// [matched: function bar]",
			wantRef: "// [matched: function bar]",
			unwant:  "comment one",
		},
		{
			name:    "no_comments_before_ref",
			input:   "code\n// [matched: method baz]",
			wantRef: "// [matched: method baz]",
		},
		{
			name:    "block_comment_before_ref",
			input:   "code\n/* block comment */\n// [matched: method qux]",
			wantRef: "// [matched: method qux]",
			unwant:  "block comment",
		},
		{
			name:    "marker_inside_trailing_comment",
			input:   "code\n// keep this comment\ncall() // [matched: not-a-reference]",
			wantRef: "// keep this comment",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := absorbPrefixComments(tc.input)
			if !strings.Contains(got, tc.wantRef) {
				t.Errorf("output should contain %q, got:\n%s", tc.wantRef, got)
			}
			if tc.unwant != "" && strings.Contains(got, tc.unwant) {
				t.Errorf("output should NOT contain %q, got:\n%s", tc.unwant, got)
			}
		})
	}
}

func TestAbsorbPrefixComments_ReturnsAbsorbedSources(t *testing.T) {
	input := "code\n// first comment\n// second comment\n// [matched: method foo]"
	_, absorbed := absorbPrefixComments(input)

	if len(absorbed) != 2 {
		t.Fatalf("expected 2 absorbed comments, got %d", len(absorbed))
	}
	// Absorbed in reverse order (scanned backwards from reference line)
	if !strings.Contains(absorbed[0], "second comment") {
		t.Error("first absorbed should contain 'second comment' (closest to ref)")
	}
	if !strings.Contains(absorbed[1], "first comment") {
		t.Error("second absorbed should contain 'first comment'")
	}
}

func TestCollapse_AdjacentChildrenKeepAllRefs(t *testing.T) {
	// Adjacent references must survive prefix-comment absorption.
	oldSrc := "class Foo {\n  a() {}\n  b() {}\n}"
	newSrc := oldSrc

	mkChild := func(id string, name string, src string, parent string) *ir.SemanticBlock {
		start := strings.Index(src, name+"() {}")
		if start < 0 {
			t.Fatalf("%s not found in parent source", name)
		}
		return &ir.SemanticBlock{
			ID:     id,
			Kind:   ir.KindMethod,
			Name:   name,
			Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start + len(name+"() {}"))},
			Source: name + "() {}",
		}
	}

	oldA := mkChild("method:a:old", "a", oldSrc, "class:Foo:old")
	oldB := mkChild("method:b:old", "b", oldSrc, "class:Foo:old")
	newA := mkChild("method:a:new", "a", newSrc, "class:Foo:new")
	newB := mkChild("method:b:new", "b", newSrc, "class:Foo:new")
	oldFoo := &ir.SemanticBlock{
		ID:     "class:Foo:old",
		Kind:   ir.KindClass,
		Name:   "Foo",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}
	newFoo := &ir.SemanticBlock{
		ID:     "class:Foo:new",
		Kind:   ir.KindClass,
		Name:   "Foo",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldFoo, New: newFoo, Confidence: 1.0},
			{Old: oldA, New: newA, Confidence: 0.9},
			{Old: oldB, New: newB, Confidence: 0.9},
		},
	}

	CollapseMatchedSubBlocks(result)

	oldCollapsed, newCollapsed := result.Pairs[0].Old.Source, result.Pairs[0].New.Source
	for _, ref := range []string{"// [matched: method a]", "// [matched: method b]"} {
		if !strings.Contains(oldCollapsed, ref) {
			t.Errorf("old parent must keep %q (adjacent references are not prefix comments), got:\n%s", ref, oldCollapsed)
		}
		if !strings.Contains(newCollapsed, ref) {
			t.Errorf("new parent must keep %q (adjacent references are not prefix comments), got:\n%s", ref, newCollapsed)
		}
	}
	if oldCollapsed != newCollapsed {
		t.Errorf("collapsed parents must be identical:\nold:\n%s\nnew:\n%s", oldCollapsed, newCollapsed)
	}
}

func TestCollapse_TrailingChildNoPhantomRefs(t *testing.T) {
	// Old class Foo: a, b. New class Foo: a, b, c (c is matched, its old
	// twin lives outside class Foo). Both classes collapse their matched
	// children, so the new parent ends with an extra trailing reference.
	// absorbPrefixComments must not eat ref(a) when processing ref(b): with
	// the bug, the old parent lost ref(a) and the new parent kept only
	// ref(c), so the matched parent pair rendered phantom -/+ reference
	// lines for the unchanged children a and b.
	oldSrc := "class Foo {\n  a() {}\n  b() {}\n}"
	newSrc := "class Foo {\n  a() {}\n  b() {}\n  c() {}\n}"

	mkChild := func(id, name, src, parent string) *ir.SemanticBlock {
		start := strings.Index(src, name+"() {}")
		if start < 0 {
			t.Fatalf("%s not found in parent source", name)
		}
		return &ir.SemanticBlock{
			ID:     id,
			Kind:   ir.KindMethod,
			Name:   name,
			Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start + len(name+"() {}"))},
			Source: name + "() {}",
		}
	}

	oldA := mkChild("method:a:old", "a", oldSrc, "class:Foo:old")
	oldB := mkChild("method:b:old", "b", oldSrc, "class:Foo:old")
	oldC := mkChild("method:c:old", "c", "function c() {}", "") // twin lives outside the class
	newA := mkChild("method:a:new", "a", newSrc, "class:Foo:new")
	newB := mkChild("method:b:new", "b", newSrc, "class:Foo:new")
	newC := mkChild("method:c:new", "c", newSrc, "class:Foo:new")
	oldFoo := &ir.SemanticBlock{
		ID:     "class:Foo:old",
		Kind:   ir.KindClass,
		Name:   "Foo",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}
	newFoo := &ir.SemanticBlock{
		ID:     "class:Foo:new",
		Kind:   ir.KindClass,
		Name:   "Foo",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldFoo, New: newFoo, Confidence: 1.0},
			{Old: oldA, New: newA, Confidence: 0.9},
			{Old: oldB, New: newB, Confidence: 0.9},
			{Old: oldC, New: newC, Confidence: 0.9},
		},
	}

	CollapseMatchedSubBlocks(result)

	oldCollapsed, newCollapsed := result.Pairs[0].Old.Source, result.Pairs[0].New.Source
	// Both parents keep every reference for the children they contain.
	for _, ref := range []string{"// [matched: method a]", "// [matched: method b]"} {
		if !strings.Contains(oldCollapsed, ref) {
			t.Errorf("old parent must keep %q, got:\n%s", ref, oldCollapsed)
		}
		if !strings.Contains(newCollapsed, ref) {
			t.Errorf("new parent must keep %q, got:\n%s", ref, newCollapsed)
		}
	}
	if strings.Contains(oldCollapsed, "// [matched: method c]") {
		t.Errorf("old parent must not reference the new-only child c:\n%s", oldCollapsed)
	}
	if !strings.Contains(newCollapsed, "// [matched: method c]") {
		t.Errorf("new parent must reference the trailing child c, got:\n%s", newCollapsed)
	}
}

func TestAbsorbPrefixComments_AdjacentRefsNotAbsorbed(t *testing.T) {
	// Two adjacent reference lines (back-to-back collapsed children): the
	// first is a reference, not a prefix comment, and must survive.
	input := "code\n// [matched: method a]\n// [matched: method b]"
	got, absorbed := absorbPrefixComments(input)
	for _, ref := range []string{"// [matched: method a]", "// [matched: method b]"} {
		if !strings.Contains(got, ref) {
			t.Errorf("adjacent references must both survive, got:\n%s", got)
		}
	}
	if len(absorbed) != 0 {
		t.Errorf("references are not prefix comments; nothing should be absorbed, got %v", absorbed)
	}
}

// TestCollapse_OneSidedAbsorptionKeepsStandaloneSide preserves one-sided comments.
func TestCollapse_OneSidedAbsorptionKeepsStandaloneSide(t *testing.T) {
	oldSrc := "class BKnob {\n  // prefix note\n  pointerdown() {}\n}\n"
	newSrc := "// prefix note\nclass BKnob {\n  pointerdown() {}\n}\n"
	commentText := "// prefix note"
	methodText := "pointerdown() {}"

	mkComment := func(id, parent string) *ir.SemanticBlock {
		start := strings.Index(oldSrc, commentText)
		if parent == "" {
			start = strings.Index(newSrc, commentText)
		}
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindComment, Name: "", Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start + len(commentText))},
			Source: commentText,
		}
	}
	mkMethod := func(id, parent string, src string) *ir.SemanticBlock {
		start := strings.Index(src, methodText)
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindMethod, Name: "pointerdown", Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start + len(methodText))},
			Source: methodText,
		}
	}
	mkClass := func(id, src string) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindClass, Name: "BKnob",
			Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(src))},
			Source: src,
		}
	}

	oldComment := mkComment("comment:old:16", "class:BKnob:0")
	newComment := mkComment("comment:new:0", "") // moved to top level
	oldMethod := mkMethod("method:old:33", "class:BKnob:0", oldSrc)
	newMethod := mkMethod("method:new:32", "class:BKnob:1", newSrc)
	oldClass := mkClass("class:BKnob:0", oldSrc)
	newClass := mkClass("class:BKnob:1", newSrc)

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldClass, New: newClass, Confidence: 1.0},
			{Old: oldMethod, New: newMethod, Confidence: 0.9},
			{Old: oldComment, New: newComment, Confidence: 0.9},
		},
	}

	CollapseMatchedSubBlocks(result)

	// The comment pair must survive: its new side is standalone.
	var commentPair *ir.CorrelatedPair
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if (p.Old != nil && p.Old.Kind == ir.KindComment) || (p.New != nil && p.New.Kind == ir.KindComment) {
			commentPair = p
		}
	}
	if commentPair == nil {
		t.Fatal("standalone new-side comment must remain in the semantic report")
	}
	if commentPair.Old == nil || commentPair.New == nil {
		t.Errorf("the moved comment must stay a matched pair, got %+v", commentPair)
	}
	// The old class must replace the absorbed comment with its reference.
	for _, p := range result.Pairs {
		if p.Old != nil && p.Old.Kind == ir.KindClass {
			if strings.Contains(p.Old.Source, "prefix note") {
				t.Errorf("old class must absorb the prefix comment into the reference:\n%s", p.Old.Source)
			}
		}
	}
}

// TestCollapse_MovedBetweenParents checks two-sided comment absorption.
func TestCollapse_MovedBetweenParents(t *testing.T) {
	classSrc := "class C {\n  // shared note\n  foo() {}\n}\n"
	commentText := "// shared note"
	methodText := "foo() {}"

	mkComment := func(id, parent string) *ir.SemanticBlock {
		start := strings.Index(classSrc, commentText)
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindComment, Name: "", Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start + len(commentText))},
			Source: commentText,
		}
	}
	mkMethod := func(id, parent string) *ir.SemanticBlock {
		start := strings.Index(classSrc, methodText)
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindMethod, Name: "foo", Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start + len(methodText))},
			Source: methodText,
		}
	}
	mkClass := func(id string) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindClass, Name: "C",
			Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(classSrc))},
			Source: classSrc,
		}
	}

	oldComment := mkComment("comment:oldA:16", "class:A:0")
	newComment := mkComment("comment:newB:16", "class:B:0")
	oldMethod := mkMethod("method:oldA:33", "class:A:0")
	newMethod := mkMethod("method:newB:33", "class:B:0")
	oldClass := mkClass("class:A:0")
	newClass := mkClass("class:B:0")

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldClass, New: newClass, Confidence: 0.8},
			{Old: oldMethod, New: newMethod, Confidence: 0.9},
			{Old: oldComment, New: newComment, Confidence: 0.9},
		},
	}

	CollapseMatchedSubBlocks(result)

	// Absorbed on both sides: the comment pair is suppressed.
	for _, p := range result.Pairs {
		if (p.Old != nil && p.Old.Kind == ir.KindComment) || (p.New != nil && p.New.Kind == ir.KindComment) {
			t.Errorf("comment absorbed on both sides must be suppressed, got %+v", p)
		}
	}
}

// TestCollapse_ChangedPrefixOneSideStandalone keeps a one-sided absorption visible.
func TestCollapse_ChangedPrefixOneSideStandalone(t *testing.T) {
	oldSrc := "class C {\n  // old wording\n  foo() {}\n}\n"
	newSrc := "class C {\n  foo() {}\n}\n// new wording elsewhere\n"

	mkBlock := func(id string, kind ir.BlockKind, name, parent, text string, src string) *ir.SemanticBlock {
		start := strings.Index(src, text)
		if start < 0 {
			t.Fatalf("%q not found in %q", text, src)
		}
		return &ir.SemanticBlock{
			ID: id, Kind: kind, Name: name, Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start) + uint(len(text))},
			Source: text,
		}
	}

	oldComment := mkBlock("comment:old:16", ir.KindComment, "", "class:C:0", "// old wording", oldSrc)
	newComment := mkBlock("comment:new:20", ir.KindComment, "", "", "// new wording elsewhere", newSrc)
	oldMethod := mkBlock("method:old:33", ir.KindMethod, "foo", "class:C:0", "foo() {}", oldSrc)
	newMethod := mkBlock("method:new:12", ir.KindMethod, "foo", "class:C:1", "foo() {}", newSrc)
	oldClass := mkBlock("class:C:0", ir.KindClass, "C", "", oldSrc, oldSrc)
	newClass := mkBlock("class:C:1", ir.KindClass, "C", "", newSrc, newSrc)

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldClass, New: newClass, Confidence: 0.8},
			{Old: oldMethod, New: newMethod, Confidence: 0.9},
			{Old: oldComment, New: newComment, Confidence: 0.6},
		},
	}

	CollapseMatchedSubBlocks(result)

	var commentPair *ir.CorrelatedPair
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if (p.Old != nil && p.Old.Kind == ir.KindComment) || (p.New != nil && p.New.Kind == ir.KindComment) {
			commentPair = p
		}
	}
	if commentPair == nil {
		t.Fatal("reworded standalone new-side comment must remain in the semantic report")
	}
}

// TestCollapse_DuplicateCommentTextOneSide keeps duplicate comment occurrences.
func TestCollapse_DuplicateCommentTextOneSide(t *testing.T) {
	commentText := "// note"
	oldSrc := "class C {\n  // note\n  foo() {}\n}\n// note\n"
	newSrc := "class C {\n  foo() {}\n}\n// note\n"

	mkComment := func(id, parent string, src string) *ir.SemanticBlock {
		start := strings.Index(src, commentText)
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindComment, Name: "", Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start) + uint(len(commentText))},
			Source: commentText,
		}
	}
	oldClassComment := mkComment("comment:oldA:16", "class:C:0", oldSrc)
	oldTopComment := mkComment("comment:oldB:33", "", oldSrc)
	newTopComment := mkComment("comment:newB:14", "", newSrc)
	mkMethod := func(id, parent string, src string) *ir.SemanticBlock {
		start := strings.Index(src, "foo() {}")
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindMethod, Name: "foo", Parent: parent,
			Span:   ir.SourceSpan{StartByte: uint(start), EndByte: uint(start) + uint(len("foo() {}"))},
			Source: "foo() {}",
		}
	}
	oldMethod := mkMethod("method:old:33", "class:C:0", oldSrc)
	newMethod := mkMethod("method:new:12", "class:C:1", newSrc)
	oldClass := &ir.SemanticBlock{
		ID: "class:C:0", Kind: ir.KindClass, Name: "C",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}
	newClass := &ir.SemanticBlock{
		ID: "class:C:1", Kind: ir.KindClass, Name: "C",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{Old: oldClass, New: newClass, Confidence: 0.8},
			{Old: oldMethod, New: newMethod, Confidence: 0.9},
			{Old: oldClassComment, New: nil, Confidence: 0.0, MatchType: ir.MatchNone},
			{Old: oldTopComment, New: newTopComment, Confidence: 1.0, MatchType: ir.MatchExactName},
		},
	}

	CollapseMatchedSubBlocks(result)

	var survivors []ir.CorrelatedPair
	for _, p := range result.Pairs {
		if (p.Old != nil && p.Old.Kind == ir.KindComment) || (p.New != nil && p.New.Kind == ir.KindComment) {
			survivors = append(survivors, p)
		}
	}
	if len(survivors) != 2 {
		t.Fatalf("both duplicate comment occurrences must survive, got %d: %+v", len(survivors), survivors)
	}
	seenIDs := map[string]bool{}
	for _, p := range survivors {
		if p.Old != nil {
			seenIDs[p.Old.ID] = true
		}
	}
	if !seenIDs["comment:oldA:16"] || !seenIDs["comment:oldB:33"] {
		t.Errorf("both the absorbed removal and top-level duplicate must remain, got %+v", survivors)
	}
}

func TestIsOnlyWhitespace(t *testing.T) {
	whitespace := []string{" ", "\t", "\n", "\r", "  \t\n  "}
	nonWhitespace := []string{"a", "// comment", "x = 1", "  code  "}

	for _, s := range whitespace {
		if !isOnlyWhitespace(s) {
			t.Errorf("expected %q to be whitespace", s)
		}
	}
	for _, s := range nonWhitespace {
		if isOnlyWhitespace(s) {
			t.Errorf("expected %q to NOT be whitespace", s)
		}
	}
}

// TestCollapse_ContainedElementInRemovedParent avoids duplicate element output.
func TestCollapse_ContainedElementInRemovedParent(t *testing.T) {
	elText := "<div id=\"sprite\">x</div>"
	oldSrc := "const HTML = (t) => html`\n  " + elText + "\n`;\n"
	newSrc := "function Comp() { return " + elText + "; }\n"

	elStart := strings.Index(oldSrc, elText)
	el := ir.SemanticBlock{
		ID:     "template:div:old:30",
		Kind:   ir.KindTemplate,
		Name:   "div",
		Parent: "",
		Span:   ir.SourceSpan{StartByte: uint(elStart), EndByte: uint(elStart + len(elText))},
		Source: elText,
	}
	htmlFn := ir.SemanticBlock{
		ID:     "arrow:HTML:old:0",
		Kind:   ir.KindArrowFunc,
		Name:   "HTML",
		Parent: "",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc))},
		Source: oldSrc,
	}
	newEl := ir.SemanticBlock{
		ID:     "jsx:div:new:30",
		Kind:   ir.KindJSX,
		Name:   "div",
		Parent: "",
		Span:   ir.SourceSpan{StartByte: uint(strings.Index(newSrc, elText)), EndByte: uint(strings.Index(newSrc, elText) + len(elText))},
		Source: elText,
	}
	comp := ir.SemanticBlock{
		ID:     "fn:Comp:new:0",
		Kind:   ir.KindFunction,
		Name:   "Comp",
		Parent: "",
		Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc))},
		Source: newSrc,
	}

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: &el, New: &newEl, Confidence: 1, MatchType: ir.MatchSimilarity}, // matched element pair
		{Old: &htmlFn}, // removed parent
		{New: &comp},   // added component
	}}
	CollapseMatchedSubBlocks(result)

	// The removed parent's source must carry the canonical reference instead
	// of the raw element text.
	if strings.Contains(htmlFn.Source, elText) {
		t.Errorf("removed parent must not keep the matched element text:\n%s", htmlFn.Source)
	}
	if !strings.Contains(htmlFn.Source, "// [matched: jsx div]") {
		t.Errorf("removed parent must carry the canonical reference, got:\n%s", htmlFn.Source)
	}
	// The matched element pair's own sources stay intact (collapse only
	// rewrites the CONTAINER sources, never the pair's own text).
	if el.Source != elText || newEl.Source != elText {
		t.Errorf("matched element sources must stay intact: %q / %q", el.Source, newEl.Source)
	}
}

func TestCollapse_TemplateMethodWithTrailingCommentOwnLine(t *testing.T) {
	// Template headers and trailing comments must fold with their methods.
	src := `class Loop {
  virtual void wakeup() = 0;                  ///< Wakeup the loop.
  template<IsLoopCallback Func>
  void add(Func&& func);                       ///< Add a callback.
  template<class Coroutine> requires IsAwaitable<ResultOf<Coroutine>>
  void add(Coroutine&& c);
}
`
	wakeupText := "virtual void wakeup() = 0;"
	wakeupStart := strings.Index(src, wakeupText)
	wakeupEnd := wakeupStart + len(wakeupText)

	comment1Text := "///< Wakeup the loop."
	comment1Start := strings.Index(src, comment1Text)

	addText := "template<IsLoopCallback Func>\n  void add(Func&& func);"
	addStart := strings.Index(src, "template<IsLoopCallback Func>")
	addEnd := strings.Index(src, "void add(Func&& func);") + len("void add(Func&& func);")

	comment2Text := "///< Add a callback."
	comment2Start := strings.Index(src, comment2Text)

	add2Text := "template<class Coroutine> requires IsAwaitable<ResultOf<Coroutine>>\n  void add(Coroutine&& c);"
	add2Start := strings.Index(src, "template<class Coroutine> requires")
	add2End := strings.Index(src, "void add(Coroutine&& c);") + len("void add(Coroutine&& c);")

	mkPair := func(kind ir.BlockKind, id, name, parent string, start, end uint, source string) ir.CorrelatedPair {
		b := ir.SemanticBlock{
			ID: id, Kind: kind, Name: name, Parent: parent,
			Span:   ir.SourceSpan{StartByte: start, EndByte: end},
			Source: source,
		}
		return ir.CorrelatedPair{Old: &b, New: &b, Confidence: 1.0, MatchType: ir.MatchExactName}
	}
	class := func(id string) *ir.SemanticBlock {
		return &ir.SemanticBlock{
			ID: id, Kind: ir.KindClass, Name: "Loop",
			Span:   ir.SourceSpan{StartByte: 0, EndByte: uint(len(src))},
			Source: src,
		}
	}
	oldClass, newClass := class("class:Loop:0"), class("class:Loop:1")
	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{
		{Old: oldClass, New: newClass, Confidence: 1.0, MatchType: ir.MatchExactName},
		mkPair(ir.KindMethod, "method:wakeup:0", "wakeup", "class:Loop:0", uint(wakeupStart), uint(wakeupEnd), wakeupText),
		mkPair(ir.KindComment, "comment:c1:0", "", "", uint(comment1Start), uint(comment1Start+len(comment1Text)), comment1Text),
		mkPair(ir.KindMethod, "method:add:0", "add", "class:Loop:0", uint(addStart), uint(addEnd), addText),
		mkPair(ir.KindComment, "comment:c2:0", "", "", uint(comment2Start), uint(comment2Start+len(comment2Text)), comment2Text),
		mkPair(ir.KindMethod, "method:add:1", "add", "class:Loop:0", uint(add2Start), uint(add2End), add2Text),
	}}

	CollapseMatchedSubBlocks(result)

	for _, p := range result.Pairs {
		if p.Old != nil && p.Old.Kind == ir.KindClass {
			col := p.Old.Source
			if !strings.Contains(col, "// [matched: method wakeup]") {
				t.Errorf("wakeup must fold to its own reference:\n%s", col)
			}
			if !strings.Contains(col, "// [matched: method add]") {
				t.Errorf("template add must fold to a reference:\n%s", col)
			}
			// Every folded reference stands on a line by itself.
			for _, line := range strings.Split(col, "\n") {
				if strings.Count(line, "[matched: method ") > 1 {
					t.Errorf("references must not share a line: %q", line)
				}
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "// [matched: method ") && !strings.HasSuffix(trimmed, "]") {
					t.Errorf("reference line must be comment-only, got %q", line)
				}
			}
			// The template headers and trailing comments fold into the refs.
			if strings.Contains(col, "template<IsLoopCallback Func>") {
				t.Errorf("template header must fold into the method reference:\n%s", col)
			}
			if strings.Contains(col, "IsAwaitable<ResultOf<Coroutine>>") {
				t.Errorf("requires-clause template header must fold into the reference:\n%s", col)
			}
			if strings.Contains(col, comment1Text) || strings.Contains(col, comment2Text) {
				t.Errorf("trailing doc comments must fold into the references:\n%s", col)
			}
		}
	}
}
