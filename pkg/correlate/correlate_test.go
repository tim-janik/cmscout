// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package correlate

import (
	"testing"

	"cmdiff/pkg/ir"
)

func newDoc(blocks ...ir.SemanticBlock) *ir.SemanticDocument {
	return &ir.SemanticDocument{
		Language: "tsx",
		FilePath: "test.tsx",
		Blocks:   blocks,
	}
}

func newBlock(kind ir.BlockKind, name, source string) ir.SemanticBlock {
	return ir.SemanticBlock{
		ID:     "test:" + string(kind) + ":" + name + ":0",
		Kind:   kind,
		Name:   name,
		Source: source,
	}
}

// matchedPairs, addedBlocks, and removedBlocks are local test accessors for
// the result slices. The IR deliberately exposes only the pair predicates
// (IsMatched/IsAdded/IsRemoved); the result-level accessor methods were
// removed as unused production API.
func matchedPairs(r *ir.CorrelationResult) []*ir.CorrelatedPair {
	var out []*ir.CorrelatedPair
	for i := range r.Pairs {
		if r.Pairs[i].IsMatched() {
			out = append(out, &r.Pairs[i])
		}
	}
	return out
}

func addedBlocks(r *ir.CorrelationResult) []*ir.SemanticBlock {
	var out []*ir.SemanticBlock
	for i := range r.Pairs {
		if r.Pairs[i].IsAdded() {
			out = append(out, r.Pairs[i].New)
		}
	}
	return out
}

func removedBlocks(r *ir.CorrelationResult) []*ir.SemanticBlock {
	var out []*ir.SemanticBlock
	for i := range r.Pairs {
		if r.Pairs[i].IsRemoved() {
			out = append(out, r.Pairs[i].Old)
		}
	}
	return out
}

func TestCorrelate_NoChanges(t *testing.T) {

	old := newDoc(
		newBlock(ir.KindFunction, "foo", "function foo() { return 1; }"),
		newBlock(ir.KindConstant, "VERSION", "const VERSION = '1.0.0';"),
	)
	new := newDoc(
		newBlock(ir.KindFunction, "foo", "function foo() { return 1; }"),
		newBlock(ir.KindConstant, "VERSION", "const VERSION = '1.0.0';"),
	)

	result := Correlate(old, new)

	if len(matchedPairs(result)) != 2 {
		t.Errorf("expected 2 matched, got %d", len(matchedPairs(result)))
	}
	if len(addedBlocks(result)) != 0 {
		t.Errorf("expected 0 added, got %d", len(addedBlocks(result)))
	}
	if len(removedBlocks(result)) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removedBlocks(result)))
	}
}

func TestCorrelate_AddedAndRemoved(t *testing.T) {

	old := newDoc(
		newBlock(ir.KindFunction, "oldFunc", ""),
	)
	new := newDoc(
		newBlock(ir.KindFunction, "newFunc", ""),
	)

	result := Correlate(old, new)

	if len(addedBlocks(result)) != 1 {
		t.Errorf("expected 1 added, got %d", len(addedBlocks(result)))
	}
	if len(removedBlocks(result)) != 1 {
		t.Errorf("expected 1 removed, got %d", len(removedBlocks(result)))
	}
	if len(matchedPairs(result)) != 0 {
		t.Errorf("expected 0 matched, got %d", len(matchedPairs(result)))
	}

	// Removed pairs should have New=nil
	for _, p := range result.Pairs {
		if p.IsRemoved() && p.New != nil {
			t.Error("removed pair should have New=nil")
		}
		if p.IsAdded() && p.Old != nil {
			t.Error("added pair should have Old=nil")
		}
	}
}

func TestCorrelate_ModifiedFunction(t *testing.T) {

	old := newDoc(
		newBlock(ir.KindFunction, "add",
			"function add(a, b) { return a + b; }",
		),
	)
	new := newDoc(
		newBlock(ir.KindFunction, "add",
			"function add(a, b) { return a + b + 1; }",
		),
	)

	result := Correlate(old, new)

	if len(matchedPairs(result)) != 1 {
		t.Errorf("expected 1 matched, got %d", len(matchedPairs(result)))
	}
	// Correlator doesn't fill InnerDiff; that's the diff stage's job
	for _, p := range matchedPairs(result) {
		if p.Old == nil || p.New == nil {
			t.Error("matched pair should have both old and new")
		}
	}
}

func TestCorrelate_RenamedFunction(t *testing.T) {

	old := newDoc(
		newBlock(ir.KindFunction, "handleClick",
			"function handleClick() { if (this.enabled) { this.value += 1; } }",
		),
	)
	new := newDoc(
		newBlock(ir.KindFunction, "onPress",
			"function onPress() { if (this.enabled) { this.value += 1; } }",
		),
	)

	result := Correlate(old, new)

	if len(matchedPairs(result)) != 1 {
		t.Errorf("expected 1 matched, got %d", len(matchedPairs(result)))
	}
	for _, p := range matchedPairs(result) {
		if p.Old.Name == p.New.Name {
			t.Error("expected renamed function (different names)")
		}
	}
}

func TestCorrelate_LifecycleToMethod(t *testing.T) {
	// A lifecycle hook (e.g., connected) replaced by a regular method with a
	// similar name and body (e.g., connectedCallback) matches via distance.

	old := newDoc(
		newBlock(ir.KindLifecycle, "connected", "connected() { this.sync(); }"),
	)
	new := newDoc(
		newBlock(ir.KindMethod, "connectedCallback", "connectedCallback() { this.sync(); }"),
	)

	result := Correlate(old, new)

	t.Logf("matched: %d, added: %d, removed: %d",
		len(matchedPairs(result)), len(addedBlocks(result)), len(removedBlocks(result)))

	if len(matchedPairs(result)) != 1 {
		t.Errorf("expected 1 matched (lifecycle→method), got %d", len(matchedPairs(result)))
	}
}

func TestCorrelate_MixedChanges(t *testing.T) {

	old := newDoc(
		newBlock(ir.KindFunction, "foo", "function foo() { return 1; }"),
		newBlock(ir.KindFunction, "bar", "function bar() { return 2; }"),
		newBlock(ir.KindConstant, "VERSION", "const VERSION = '1.0';"),
	)
	new := newDoc(
		newBlock(ir.KindFunction, "foo", "function foo() { return 1; }"),
		newBlock(ir.KindFunction, "bar", "function bar() { return 3; }"),
		newBlock(ir.KindFunction, "baz", "function baz() { return 4; }"),
	)

	result := Correlate(old, new)

	if len(matchedPairs(result)) != 2 {
		t.Errorf("expected 2 matched (foo, bar), got %d", len(matchedPairs(result)))
	}
	if len(addedBlocks(result)) != 1 {
		t.Errorf("expected 1 added (baz), got %d", len(addedBlocks(result)))
	}
	if len(removedBlocks(result)) != 1 {
		t.Errorf("expected 1 removed (VERSION), got %d", len(removedBlocks(result)))
	}
}

func TestCorrelate_EmptyDocuments(t *testing.T) {
	result := Correlate(newDoc(), newDoc())

	if len(result.Pairs) != 0 {
		t.Errorf("expected 0 pairs, got %d", len(result.Pairs))
	}
	if len(addedBlocks(result)) != 0 {
		t.Errorf("expected 0 added, got %d", len(addedBlocks(result)))
	}
	if len(removedBlocks(result)) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removedBlocks(result)))
	}
	if len(matchedPairs(result)) != 0 {
		t.Errorf("expected 0 matched, got %d", len(matchedPairs(result)))
	}
}
