// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"testing"

	"cmdiff/pkg/ir"
)

// hoistedTestBlock builds a block with a deterministic ID (parent + kind + name).
func hoistedTestBlock(kind ir.BlockKind, name, source, parent string) ir.SemanticBlock {
	return ir.SemanticBlock{
		ID:     parent + "\x00" + string(kind) + ":" + name,
		Kind:   kind,
		Name:   name,
		Parent: parent,
		Source: source,
	}
}

// TestMatchBlocks_HoistedInnerCallable: a method whose class is removed matches a same-name
// nested function whose enclosing function is added; both are hoisted, containers differ.
func TestMatchBlocks_HoistedInnerCallable(t *testing.T) {
	old := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindClass, "BPartList", "class BPartList extends LitComponent {\n"+
			"  createRenderRoot() { return this; }\n"+
			"  render() { const d = {}; return HTML (this, d); }\n"+
			"  updated (changed_props) { if (changed_props.has ('track')) { const weakthis = new WeakRef (this); this.wtrack = Util.wrap_ase_object (this.track, { arranger_parts: [] }, () => weakthis.deref()?.requestUpdate()); } }\n"+
			"  dblclick (event) { this.track.create_part (0); }\n"+
			"}", ""),
		hoistedTestBlock(ir.KindMethod, "createRenderRoot",
			"createRenderRoot() { return this; }", "\x00class:BPartList"),
		hoistedTestBlock(ir.KindMethod, "dblclick",
			"dblclick (event) { this.track.create_part (0); }", "\x00class:BPartList"),
	}
	new := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindFunction, "PartList", "export function PartList (props) {\n"+
			"  const [parts, set_parts] = createSignal ([]);\n"+
			"  createEffect (() => { const track = props.track; set_parts (track.arranger_parts || []); onCleanup (() => track.cleanup()); });\n"+
			"  function dblclick (event)\n"+
			"  {\n"+
			"    if (props.track && typeof props.track.create_part === 'function')\n"+
			"      props.track.create_part (0);\n"+
			"  }\n"+
			"  return <div onDblClick={dblclick} />;\n"+
			"}", ""),
		hoistedTestBlock(ir.KindFunction, "dblclick",
			"function dblclick (event)\n"+
				"  {\n"+
				"    if (props.track && typeof props.track.create_part === 'function')\n"+
				"      props.track.create_part (0);\n"+
				"  }", "\x00function:PartList"),
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	// The hoisted dblclick callables must pair by exact name across containers.
	var dblclickPair *ir.CorrelatedPair
	for i := range pairs {
		if pairs[i].Old != nil && pairs[i].Old.Name == "dblclick" {
			dblclickPair = &pairs[i]
			break
		}
	}
	if dblclickPair == nil || dblclickPair.New == nil {
		t.Fatalf("dblclick method must match the nested dblclick function, pairs: %+v", pairs)
	}
	if dblclickPair.MatchType != ir.MatchExactName {
		t.Errorf("expected MatchExactName, got %s", dblclickPair.MatchType)
	}
	if dblclickPair.Old.Kind != ir.KindMethod || dblclickPair.New.Kind != ir.KindFunction {
		t.Errorf("expected method → function conversion, got %s → %s",
			dblclickPair.Old.Kind, dblclickPair.New.Kind)
	}

	// The containers themselves stay unmatched (deletion + addition); other
	// orphaned members (createRenderRoot) may also stay unmatched.
	foundClass, foundFunc := false, false
	for _, b := range uOld {
		if b.Kind == ir.KindClass && b.Name == "BPartList" {
			foundClass = true
		}
	}
	for _, b := range uNew {
		if b.Kind == ir.KindFunction && b.Name == "PartList" {
			foundFunc = true
		}
	}
	if !foundClass {
		t.Errorf("expected the class to stay unmatched (removed), got %v", uOld)
	}
	if !foundFunc {
		t.Errorf("expected the function to stay unmatched (added), got %v", uNew)
	}
}

// TestMatchBlocks_HoistedInnerCallable_ScopeBoundExcluded: arrow functions and
// lambdas close over their scope, so nested ones must never be rescued by name.
func TestMatchBlocks_HoistedInnerCallable_ScopeBoundExcluded(t *testing.T) {
	old := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindClass, "Widget", "class Widget {\n"+
			"  handler = (event) => { this.clicked(event); };\n"+
			"}", ""),
		hoistedTestBlock(ir.KindArrowFunc, "handler",
			"handler = (event) => { this.clicked(event); }", "\x00class:Widget"),
	}
	new := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindFunction, "View", "export function View (props) {\n"+
			"  const handler = (event) => { props.clicked(event); };\n"+
			"  return <div onClick={handler} />;\n"+
			"}", ""),
		hoistedTestBlock(ir.KindArrowFunc, "handler",
			"handler = (event) => { props.clicked(event); }", "\x00function:View"),
	}

	pairs, _, _ := MatchBlocks(old, new, SimilarityThreshold)
	for _, p := range pairs {
		if p.Old != nil && p.New != nil &&
			p.Old.Name == "handler" && p.New.Name == "handler" {
			t.Errorf("scope-bound arrow handler must not be rescued by name: %+v", p)
		}
	}
}

// TestMatchBlocks_HoistedInnerCallable_ContainerAuthority: the rescue never pairs children of
// a matched container, so matched containers keep full authority over their children.
func TestMatchBlocks_HoistedInnerCallable_ContainerAuthority(t *testing.T) {
	old := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindClass, "Panel", "class Panel {\n"+
			"  render() { return this.composeB(); }\n"+
			"  buildA() { return alpha(); }\n"+
			"}", ""),
		hoistedTestBlock(ir.KindMethod, "render",
			"render() { return this.composeB(); }", "\x00class:Panel"),
		hoistedTestBlock(ir.KindMethod, "buildA",
			"buildA() { return alpha(); }", "\x00class:Panel"),
	}
	new := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindClass, "Panel", "class Panel {\n"+
			"  render() { return this.composeB(); }\n"+
			"  composeB() { const q = beta(); return q * 100 + gamma; }\n"+
			"}", ""),
		hoistedTestBlock(ir.KindMethod, "render",
			"render() { return this.composeB(); }", "\x00class:Panel"),
		hoistedTestBlock(ir.KindMethod, "composeB",
			"composeB() { const q = beta(); return q * 100 + gamma; }", "\x00class:Panel"),
		// An orphan function has a nested buildA — it must NOT pair with the
		// kept class's buildA: the matched Panel container keeps its child.
		hoistedTestBlock(ir.KindFunction, "Helper", "function Helper() {\n"+
			"  function buildA() { return alpha(); }\n"+
			"  return buildA;\n"+
			"}", ""),
		hoistedTestBlock(ir.KindFunction, "buildA",
			"function buildA() { return alpha(); }", "\x00function:Helper"),
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	for _, p := range pairs {
		if p.Old != nil && p.Old.Name == "buildA" && p.New != nil && p.New.Name == "buildA" {
			t.Errorf("rescue must not pair buildA across a matched container: %+v", p)
		}
	}

	// The old buildA (matched container) and the orphan buildA (no orphaned
	// counterpart) stay unmatched; the Panel class pair matches exactly.
	foundOldBuildA, foundNewBuildA := false, false
	for _, b := range uOld {
		if b.Name == "buildA" && b.Kind == ir.KindMethod {
			foundOldBuildA = true
		}
	}
	for _, b := range uNew {
		if b.Name == "buildA" && b.Kind == ir.KindFunction {
			foundNewBuildA = true
		}
	}
	if !foundOldBuildA {
		t.Errorf("old buildA must stay unmatched (matched container): %v", uOld)
	}
	if !foundNewBuildA {
		t.Errorf("new buildA must stay unmatched (no orphaned old counterpart): %v", uNew)
	}
}

// TestMatchBlocks_HoistedInnerCallable_NestedChildren: children of rescued
// callables are matched hierarchically like any other matched container pair.
func TestMatchBlocks_HoistedInnerCallable_NestedChildren(t *testing.T) {
	old := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindClass, "A", "class A {\n"+
			"  dblclick (event) { helper(0); }\n"+
			"}", ""),
		hoistedTestBlock(ir.KindMethod, "dblclick",
			"dblclick (event) { helper(0); }", "\x00class:A"),
	}
	new := []ir.SemanticBlock{
		hoistedTestBlock(ir.KindFunction, "B", "export function B (props) {\n"+
			"  function dblclick (event) {\n"+
			"    function helper(n) { collect(n); }\n"+
			"    helper(0);\n"+
			"  }\n"+
			"}", ""),
		hoistedTestBlock(ir.KindFunction, "dblclick",
			"function dblclick (event) {\n"+
				"    function helper(n) { collect(n); }\n"+
				"    helper(0);\n"+
				"  }", "\x00function:B"),
		hoistedTestBlock(ir.KindFunction, "helper",
			"function helper(n) { collect(n); }", "\x00function:dblclick"),
	}

	pairs, _, _ := MatchBlocks(old, new, SimilarityThreshold)
	if len(pairs) != 1 {
		t.Fatalf("expected exactly the dblclick pair, got %+v", pairs)
	}
	if pairs[0].Old == nil || pairs[0].New == nil ||
		pairs[0].Old.Name != "dblclick" || pairs[0].New.Name != "dblclick" {
		t.Fatalf("expected dblclick pair, got %+v", pairs)
	}
}
