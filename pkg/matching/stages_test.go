// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"math"
	"strings"
	"testing"

	"cmdiff/pkg/ir"
)

func TestMatchBlocks_ExactName(t *testing.T) {
	old := []ir.SemanticBlock{
		{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
		{Kind: ir.KindFunction, Name: "bar", Source: "function bar() {}"},
	}
	new := []ir.SemanticBlock{
		{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
		{Kind: ir.KindFunction, Name: "bar", Source: "function bar() {}"},
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs, got %d", len(pairs))
	}
	if len(uOld) != 0 {
		t.Errorf("expected 0 unmatched old, got %d", len(uOld))
	}
	if len(uNew) != 0 {
		t.Errorf("expected 0 unmatched new, got %d", len(uNew))
	}
	for _, p := range pairs {
		if p.MatchType != ir.MatchExactName {
			t.Errorf("expected MatchExactName, got %s", p.MatchType)
		}
	}
}

func TestMatchBlocks_RenamedFunction(t *testing.T) {
	// Same structure, different names — should match by distance.
	old := []ir.SemanticBlock{
		{
			Kind:   ir.KindFunction,
			Name:   "updated",
			Source: "function updated() { if (this.enabled) { this.value += 1; } }",
		},
	}
	new := []ir.SemanticBlock{
		{
			Kind:   ir.KindFunction,
			Name:   "handleUpdate",
			Source: "function handleUpdate() { if (this.isReady) { this.counter += 1; } }",
		},
	}

	pairs, _, _ := MatchBlocks(old, new, SimilarityThreshold)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].MatchType == ir.MatchExactName {
		t.Error("should not match by exact name (names differ)")
	}
	if pairs[0].Confidence < 0.5 {
		t.Errorf("expected confidence >= 0.5 for a near-identical rename, got %f", pairs[0].Confidence)
	}
}

func TestMatchBlocks_SameBodyDifferentName(t *testing.T) {
	// Two blocks with different names but (nearly) identical source text:
	// the distance table must match them as a rename.
	old := []ir.SemanticBlock{
		{Kind: ir.KindFunction, Name: "oldName", Source: "function oldName() { return 1; }"},
	}
	new := []ir.SemanticBlock{
		{Kind: ir.KindFunction, Name: "newName", Source: "function newName() { return 1; }"},
	}

	pairs, _, _ := MatchBlocks(old, new, SimilarityThreshold)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].MatchType != ir.MatchSimilarity {
		t.Errorf("expected MatchSimilarity, got %s", pairs[0].MatchType)
	}
	if pairs[0].Confidence < 0.8 {
		t.Errorf("expected high confidence for a near-identical rename, got %f", pairs[0].Confidence)
	}
}

func TestMatchBlocks_ClassToFunctionConversion(t *testing.T) {
	// A class that became a function with similar content must be matched
	// as a conversion, not reported as removed + added.
	classSrc := "class BKnob extends LitComponent {\n  render() { return html`<div></div>`; }\n}"
	funcSrc := "function Knob(props) {\n  return <div></div>;\n}"
	old := []ir.SemanticBlock{
		{Kind: ir.KindClass, Name: "BKnob", Source: classSrc},
	}
	new := []ir.SemanticBlock{
		{Kind: ir.KindFunction, Name: "Knob", Source: funcSrc},
	}

	pairs, _, _ := MatchBlocks(old, new, SimilarityThreshold)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 converted pair, got %d", len(pairs))
	}
	if pairs[0].Old.Kind == pairs[0].New.Kind {
		t.Error("expected a kind change (conversion)")
	}
	if pairs[0].Confidence < 0.5 {
		t.Errorf("expected confidence >= 0.5 for a conversion, got %f", pairs[0].Confidence)
	}
}

func TestMatchBlocks_DuplicateExactNamesUseBodySimilarity(t *testing.T) {
	old := []ir.SemanticBlock{
		{ID: "old:first", Kind: ir.KindClass, Name: "C", Source: "class C { return 1; }"},
		{ID: "old:second", Kind: ir.KindClass, Name: "C", Source: "class C { return 2; }"},
	}
	new := []ir.SemanticBlock{
		{ID: "new:first", Kind: ir.KindClass, Name: "C", Source: "class C { return 2; }"},
		{ID: "new:second", Kind: ir.KindClass, Name: "C", Source: "class C { return 1; }"},
	}

	pairs, oldRemaining, newRemaining := MatchBlocks(old, new, SimilarityThreshold)
	if len(pairs) != 2 || len(oldRemaining) != 0 || len(newRemaining) != 0 {
		t.Fatalf("expected two complete matches, got pairs=%d old=%d new=%d", len(pairs), len(oldRemaining), len(newRemaining))
	}
	for _, pair := range pairs {
		switch pair.Old.ID {
		case "old:first":
			if pair.New.ID != "new:second" {
				t.Errorf("old:first paired with %s, want new:second", pair.New.ID)
			}
		case "old:second":
			if pair.New.ID != "new:first" {
				t.Errorf("old:second paired with %s, want new:first", pair.New.ID)
			}
		}
	}
}

func TestMatchBlocks_AddedAndRemoved(t *testing.T) {
	// Different names and no body text: no evidence of a relationship, so
	// both sides stay unmatched (removed + added).
	old := []ir.SemanticBlock{{Kind: ir.KindFunction, Name: "oldFunc"}}
	new := []ir.SemanticBlock{{Kind: ir.KindFunction, Name: "newFunc"}}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	if len(uOld) != 1 {
		t.Errorf("expected 1 unmatched old, got %d", len(uOld))
	}
	if len(uNew) != 1 {
		t.Errorf("expected 1 unmatched new, got %d", len(uNew))
	}
	if len(pairs) != 0 {
		t.Errorf("expected 0 matched pairs, got %d", len(pairs))
	}
}

func TestMatchBlocks_ThresholdFiltering(t *testing.T) {
	old := []ir.SemanticBlock{
		{Name: "foo", Kind: ir.KindFunction, Source: "function foo() { very different content here }"},
	}
	new := []ir.SemanticBlock{
		{Name: "bar", Kind: ir.KindFunction, Source: "function bar() { completely unrelated stuff }"},
	}
	pairs, _, _ := MatchBlocks(old, new, 0.9) // very high threshold
	if len(pairs) != 0 {
		t.Errorf("expected 0 pairs (below threshold), got %d", len(pairs))
	}

	pairs, _, _ = MatchBlocks(old, new, 0.1) // very low threshold
	if len(pairs) != 1 {
		t.Errorf("expected 1 pair (low threshold), got %d", len(pairs))
	}
}

func TestMatchBlocks_EmptySets(t *testing.T) {
	pairs, uOld, uNew := MatchBlocks(nil, nil, SimilarityThreshold)
	if len(pairs) != 0 || len(uOld) != 0 || len(uNew) != 0 {
		t.Error("expected all empty for nil inputs")
	}
}

// TestMatchBlocks_ScopeAwareExactName guards the scope-awareness of the
// exact-name stage: two classes that both define `foo`, with the classes
// reordered, must pair old A.foo with new A.foo and old B.foo with new B.foo
// (never cross-container), so the report shows no false method changes.
func TestMatchBlocks_ScopeAwareExactName(t *testing.T) {
	mkMethod := func(id, name, parent string) ir.SemanticBlock {
		return ir.SemanticBlock{
			ID:     id,
			Kind:   ir.KindMethod,
			Name:   name,
			Parent: parent,
			Source: name + "() { return 1; }",
		}
	}
	old := []ir.SemanticBlock{
		mkMethod("old:a:foo:10", "foo", "class:A:0"),
		mkMethod("old:b:foo:40", "foo", "class:B:30"),
	}
	new := []ir.SemanticBlock{
		mkMethod("new:b:foo:10", "foo", "class:B:0"),
		mkMethod("new:a:foo:40", "foo", "class:A:30"),
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)
	if len(pairs) != 2 || len(uOld) != 0 || len(uNew) != 0 {
		t.Fatalf("expected 2 pairs and no unmatched blocks, got %d pairs, %d unmatched old, %d unmatched new",
			len(pairs), len(uOld), len(uNew))
	}
	// The exact identity must be preserved per container: old A.foo pairs
	// with new A.foo, old B.foo with new B.foo — the first same-name
	// candidate in the new file (B.foo) must not steal A.foo's partner.
	for _, p := range pairs {
		if p.Old.ID == "old:a:foo:10" && p.New.ID != "new:a:foo:40" {
			t.Errorf("old A.foo must pair with new A.foo, paired with %s", p.New.ID)
		}
		if p.Old.ID == "old:b:foo:40" && p.New.ID != "new:b:foo:10" {
			t.Errorf("old B.foo must pair with new B.foo, paired with %s", p.New.ID)
		}
		// Parent scopes must be compatible on both sides.
		if p.Old.Parent == "class:A:0" && p.New.Parent != "class:A:30" && p.New.Parent != "class:A:0" {
			t.Errorf("old A.foo paired across parent scopes: old parent %q, new parent %q", p.Old.Parent, p.New.Parent)
		}
	}
}

// TestMatchBlocks_ExactNameRequiresScopeCompatibility: same (kind, name)
// blocks whose parents are different containers must NOT be exact-name
// paired; they may still pair via distance, but never via the identity
// stage that claims certainty.
func TestMatchBlocks_ExactNameRequiresScopeCompatibility(t *testing.T) {
	old := []ir.SemanticBlock{
		{ID: "method:foo:a", Kind: ir.KindMethod, Name: "foo", Parent: "class:A:0", Source: "foo() { return 1; }"},
	}
	new := []ir.SemanticBlock{
		{ID: "method:foo:b", Kind: ir.KindMethod, Name: "foo", Parent: "class:B:0", Source: "foo() { return 2; }"},
	}
	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)
	for _, p := range pairs {
		if p.MatchType == ir.MatchExactName {
			t.Errorf("cross-container pair must not be exact-name matched: old %s -> new %s", p.Old.ID, p.New.ID)
		}
	}
	// The bodies differ (return 1 vs return 2) but the names and kinds are
	// identical, so a distance match is expected rather than an exact one.
	_ = pairs
	_ = uOld
	_ = uNew
}

// TestMatchBlocks_AddedMethodStaysAdded: an added `foo` method in another
// class must remain an added block; the matcher must not fabricate a match
// for it.
func TestMatchBlocks_AddedMethodStaysAdded(t *testing.T) {
	old := []ir.SemanticBlock{
		{ID: "method:foo:a", Kind: ir.KindMethod, Name: "foo", Parent: "class:A:0", Source: "foo() { return 1; }"},
	}
	new := []ir.SemanticBlock{
		{ID: "method:foo:a", Kind: ir.KindMethod, Name: "foo", Parent: "class:A:0", Source: "foo() { return 1; }"},
		{ID: "method:foo:b", Kind: ir.KindMethod, Name: "foo", Parent: "class:B:10", Source: "foo() { return 2; }"},
	}
	pairs, _, uNew := MatchBlocks(old, new, SimilarityThreshold)
	if len(pairs) != 1 {
		t.Fatalf("expected exactly 1 matched pair (A.foo), got %d", len(pairs))
	}
	if len(uNew) != 1 || uNew[0].ID != "method:foo:b" {
		t.Errorf("added B.foo must remain unmatched, got %v", uNew)
	}
}

// TestBestMatches_MaximumWeightAssignment exercises the ambiguous-table
// regression: greedy best-first selection takes the single high edge
// (old0→new0 = 0.9) and then has no partner for old1, while the
// maximum-weight assignment selects TWO credible edges
// (old0→new1 = 0.6, old1→new0 = 0.6; total 1.2 > 0.9). The implementation
// must not discard two credible matches in favor of one slightly higher
// edge.
func TestBestMatches_MaximumWeightAssignment(t *testing.T) {
	table := [][]float64{
		{0.9, 0.6},
		{0.6, 0.2},
	}
	got := bestMatches(table, SimilarityThreshold)
	if len(got) != 2 {
		t.Fatalf("expected 2 matches (maximum-weight assignment), got %d: %+v", len(got), got)
	}
	want := map[int]int{0: 1, 1: 0}
	for _, s := range got {
		if want[s.old] != s.new {
			t.Errorf("unexpected pair old%d→new%d; want %v", s.old, s.new, want)
		}
	}
}

// TestBestMatches_DeterministicTies: equal scores must resolve
// deterministically (no map-iteration or goroutine dependence) and the
// result must be stable across repeated calls.
func TestBestMatches_DeterministicTies(t *testing.T) {
	table := [][]float64{
		{0.8, 0.8},
		{0.8, 0.8},
	}
	first := bestMatches(table, SimilarityThreshold)
	for iter := 0; iter < 5; iter++ {
		again := bestMatches(table, SimilarityThreshold)
		if len(again) != len(first) {
			t.Fatalf("iteration %d: match count changed: %d vs %d", iter, len(again), len(first))
		}
		for i := range first {
			if first[i] != again[i] {
				t.Errorf("iteration %d: pair %d changed: %+v vs %+v", iter, i, first[i], again[i])
			}
		}
	}
	// Rectangular tables must also work: more old than new and vice versa.
	tall := [][]float64{{0.9}, {0.9}, {0.9}}
	if got := bestMatches(tall, SimilarityThreshold); len(got) != 1 {
		t.Errorf("tall table: expected 1 match, got %d", len(got))
	}
	wide := [][]float64{{0.9, 0.9, 0.9}}
	if got := bestMatches(wide, SimilarityThreshold); len(got) != 1 {
		t.Errorf("wide table: expected 1 match, got %d", len(got))
	}
}

// TestMatchBlocks_CommentsSimilarity: comments with a small typo or a
// moderate wording change must be paired by distance (a matched comment
// with an inner diff), while genuinely unrelated comments remain
// removed/added. Comments must never be compared against code blocks.
func TestMatchBlocks_CommentsSimilarity(t *testing.T) {
	old := []ir.SemanticBlock{
		{Kind: ir.KindComment, Name: "", Source: "// Handles the pointer down event"},
		{Kind: ir.KindComment, Name: "", Source: "// The old widget layout"},
		{Kind: ir.KindFunction, Name: "f", Source: "function f() { return 1; }"},
	}
	new := []ir.SemanticBlock{
		{Kind: ir.KindComment, Name: "", Source: "// Handles the pointerdown event"},
		{Kind: ir.KindComment, Name: "", Source: "// Totally unrelated new note"},
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	// The typo'd comment pairs by similarity.
	var commentPair *ir.CorrelatedPair
	for i := range pairs {
		if pairs[i].Old.Kind == ir.KindComment {
			commentPair = &pairs[i]
		}
	}
	if commentPair == nil {
		t.Fatalf("expected a similarity-matched comment pair; pairs=%+v unmatchedOld=%d unmatchedNew=%d",
			pairs, len(uOld), len(uNew))
	}
	if commentPair.MatchType != ir.MatchSimilarity {
		t.Errorf("comment pair must be MatchSimilarity, got %s", commentPair.MatchType)
	}
	if !strings.HasPrefix(commentPair.Old.Source, "// Handles the pointer") ||
		!strings.HasPrefix(commentPair.New.Source, "// Handles the pointer") {
		t.Errorf("unexpected comment pair: %q -> %q", commentPair.Old.Source, commentPair.New.Source)
	}
	// The unrelated comment stays unmatched on both sides (the old-side
	// function f has no counterpart either, so it is unmatched too).
	var unmatchedOldComments, unmatchedNewComments []string
	for _, b := range uOld {
		if b.Kind == ir.KindComment {
			unmatchedOldComments = append(unmatchedOldComments, b.Source)
		}
	}
	for _, b := range uNew {
		unmatchedNewComments = append(unmatchedNewComments, b.Source)
	}
	if len(unmatchedOldComments) != 1 || unmatchedOldComments[0] != "// The old widget layout" {
		t.Errorf("expected the unrelated old comment to remain unmatched, got %v", unmatchedOldComments)
	}
	if len(unmatchedNewComments) != 1 || unmatchedNewComments[0] != "// Totally unrelated new note" {
		t.Errorf("expected the unrelated new comment to remain unmatched, got %v", unmatchedNewComments)
	}
}

// TestMatchBlocks_ExactComments: identical comment text pairs in the cheap
// exact stage.
func TestMatchBlocks_ExactComments(t *testing.T) {
	old := []ir.SemanticBlock{
		{Kind: ir.KindComment, Name: "", Source: "// identical comment"},
	}
	new := []ir.SemanticBlock{
		{Kind: ir.KindComment, Name: "", Source: "// identical comment"},
		{Kind: ir.KindComment, Name: "", Source: "// another comment"},
	}
	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 exact comment pair, got %d", len(pairs))
	}
	if pairs[0].MatchType != ir.MatchSimilarity {
		t.Errorf("exact comment pair should carry MatchSimilarity, got %s", pairs[0].MatchType)
	}
	if len(uOld) != 0 || len(uNew) != 1 || uNew[0].Source != "// another comment" {
		t.Errorf("unexpected remainders: old=%v new=%v", uOld, uNew)
	}
}

func TestMatchBlocks_DuplicateCommentsPreferMatchingContainer(t *testing.T) {
	comment := func(id string, start uint) ir.SemanticBlock {
		return ir.SemanticBlock{ID: id, Kind: ir.KindComment, Source: "// note",
			Span: ir.SourceSpan{StartByte: start, EndByte: start + 7}}
	}
	old := []ir.SemanticBlock{
		{ID: "class:A:0", Kind: ir.KindClass, Name: "A", Source: "class A {\n  // note\n}",
			Span: ir.SourceSpan{StartByte: 0, EndByte: 22}},
		comment("comment:inside:10", 10),
		comment("comment:top:30", 30),
	}
	new := []ir.SemanticBlock{
		{ID: "class:A:0", Kind: ir.KindClass, Name: "A", Source: "class A {\n}",
			Span: ir.SourceSpan{StartByte: 0, EndByte: 11}},
		comment("comment:top:20", 20),
	}

	pairs, unmatchedOld, unmatchedNew := MatchBlocks(old, new, SimilarityThreshold)
	var commentPair *ir.CorrelatedPair
	for i := range pairs {
		if pairs[i].Old.Kind == ir.KindComment {
			commentPair = &pairs[i]
			break
		}
	}
	if commentPair == nil || commentPair.Old.ID != "comment:top:30" || commentPair.New.ID != "comment:top:20" {
		if commentPair == nil {
			t.Fatalf("duplicate comment should stay with the top-level occurrence, pairs=%+v", pairs)
		}
		t.Fatalf("duplicate comment should stay with the top-level occurrence, got %s -> %s", commentPair.Old.ID, commentPair.New.ID)
	}
	if len(unmatchedOld) != 1 || unmatchedOld[0].ID != "comment:inside:10" || len(unmatchedNew) != 0 {
		t.Fatalf("the nested duplicate should be the removed occurrence, old=%v new=%v", unmatchedOld, unmatchedNew)
	}
}

// TestMatchBlocks_StableOrdering: when blocks are added/removed between
// matching stages, the result ordering must stay deterministic and stable:
// matched pairs in stage order, unmatched old blocks and unmatched new
// blocks in their original order.
func TestMatchBlocks_StableOrdering(t *testing.T) {
	old := []ir.SemanticBlock{
		{ID: "o1", Kind: ir.KindFunction, Name: "keep", Source: "function keep() { return 1; }"},
		{ID: "o2", Kind: ir.KindFunction, Name: "gone", Source: "function gone() { throw new Error(\"gone\"); }"},
		{ID: "o3", Kind: ir.KindConstant, Name: "C", Source: "const C = 1;"},
	}
	new := []ir.SemanticBlock{
		{ID: "n1", Kind: ir.KindFunction, Name: "added", Source: "function added() { return 3; }"},
		{ID: "n2", Kind: ir.KindFunction, Name: "keep", Source: "function keep() { return 1; }"},
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	if len(pairs) != 1 || pairs[0].Old.ID != "o1" || pairs[0].New.ID != "n2" {
		t.Fatalf("expected the single matched pair o1→n2, got %+v", pairs)
	}
	if len(uOld) != 2 || uOld[0].ID != "o2" || uOld[1].ID != "o3" {
		t.Errorf("unmatched old blocks must keep original order, got %v", uOld)
	}
	if len(uNew) != 1 || uNew[0].ID != "n1" {
		t.Errorf("unmatched new blocks must keep original order, got %v", uNew)
	}

	// Repeated matching must produce the identical result (no hidden
	// iteration-order dependence).
	pairs2, uOld2, uNew2 := MatchBlocks(old, new, SimilarityThreshold)
	if len(pairs2) != len(pairs) || len(uOld2) != len(uOld) || len(uNew2) != len(uNew) {
		t.Fatal("repeated matching produced a different shape")
	}
	for i := range pairs {
		if pairs2[i].Old.ID != pairs[i].Old.ID || pairs2[i].New.ID != pairs[i].New.ID {
			t.Errorf("pair %d changed across runs: %s→%s vs %s→%s",
				i, pairs2[i].Old.ID, pairs2[i].New.ID, pairs[i].Old.ID, pairs[i].New.ID)
		}
	}
}

func TestBlockSimilarity(t *testing.T) {
	a := &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"}
	b := &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"}
	score := BlockSimilarity(a, b, DistanceText(a.Source), DistanceText(b.Source))
	if score < 0.9 {
		t.Errorf("identical blocks should have high similarity, got %.2f", score)
	}

	c := &ir.SemanticBlock{Kind: ir.KindImport, Name: "x", Source: "import { x } from './x'"}
	score2 := BlockSimilarity(a, c, DistanceText(a.Source), DistanceText(c.Source))
	if score2 > 0.5 {
		t.Errorf("very different blocks should have low similarity, got %.2f", score2)
	}
}

// TestMatchBlocks_RecursiveHierarchy: matching descends recursively.
// outer → inner/renamedInner → deep: the renamed inner container must not
// orphan its unchanged grandchild `deep` — recursive descent pairs it at
// the level below the rename.
func TestMatchBlocks_RecursiveHierarchy(t *testing.T) {
	mkFn := func(id, name, parent string, start uint) ir.SemanticBlock {
		return ir.SemanticBlock{
			ID: id, Kind: ir.KindFunction, Name: name, Parent: parent,
			Span:   ir.SourceSpan{StartByte: start},
			Source: "function " + name + "() { return 1; }",
		}
	}
	old := []ir.SemanticBlock{
		mkFn("fn:outer:0", "outer", "", 0),
		mkFn("fn:inner:20", "inner", "fn:outer:0", 20),
		mkFn("fn:deep:40", "deep", "fn:inner:20", 40),
	}
	new := []ir.SemanticBlock{
		mkFn("fn:outer:0", "outer", "", 0),
		mkFn("fn:renamedInner:20", "renamedInner", "fn:outer:0", 20),
		mkFn("fn:deep:45", "deep", "fn:renamedInner:20", 45),
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)
	if len(uOld) != 0 || len(uNew) != 0 {
		t.Fatalf("all blocks must be matched, unmatched old=%d new=%d", len(uOld), len(uNew))
	}
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs (outer, inner→renamedInner, deep), got %d: %+v", len(pairs), pairs)
	}
	byOld := map[string]string{}
	for _, p := range pairs {
		byOld[p.Old.ID] = p.New.ID
	}
	if byOld["fn:inner:20"] != "fn:renamedInner:20" {
		t.Errorf("inner must pair with renamedInner (rename), got %q", byOld["fn:inner:20"])
	}
	if byOld["fn:deep:40"] != "fn:deep:45" {
		t.Errorf("deep must pair with deep through the recursive descent, got %q", byOld["fn:deep:40"])
	}
	// The unchanged deep pair must carry full confidence.
	for _, p := range pairs {
		if p.Old.ID == "fn:deep:40" && p.Confidence != 1.0 {
			t.Errorf("identical deep pair must have confidence 1.0, got %v", p.Confidence)
		}
	}
}

// TestMatchBlocks_RecursiveHierarchyAddRemove: recursion with additions
// and removals at the deepest level: `keep` pairs through two renamed
// levels, `gone` is removed, `added` is added (their bodies are too
// different to be a credible rename pair).
func TestMatchBlocks_RecursiveHierarchyAddRemove(t *testing.T) {
	mkFn := func(id, name, parent string) ir.SemanticBlock {
		return ir.SemanticBlock{
			ID: id, Kind: ir.KindFunction, Name: name, Parent: parent,
			Source: "function " + name + "() { return 1; }",
		}
	}
	old := []ir.SemanticBlock{
		mkFn("o1", "outer", ""),
		mkFn("o2", "a", "o1"),
		mkFn("o3", "keep", "o2"),
		{ID: "o4", Kind: ir.KindFunction, Name: "gone", Parent: "o2",
			Source: "function gone() { return 1; }"},
	}
	new := []ir.SemanticBlock{
		mkFn("n1", "outer", ""),
		mkFn("n2", "a2", "n1"),
		mkFn("n3", "keep", "n2"),
		{ID: "n4", Kind: ir.KindFunction, Name: "added", Parent: "n2",
			Source: "function added() { for (let i = 0; i < n; i++) { collect(i); } }"},
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)
	matched := map[string]string{}
	for _, p := range pairs {
		matched[p.Old.ID] = p.New.ID
	}
	if matched["o3"] != "n3" {
		t.Errorf("keep must pair through two renamed levels, got %q", matched["o3"])
	}
	if len(uOld) != 1 || uOld[0].ID != "o4" {
		t.Errorf("gone must remain unmatched (removed), got %v", uOld)
	}
	if len(uNew) != 1 || uNew[0].ID != "n4" {
		t.Errorf("added must remain unmatched (added), got %v", uNew)
	}
}

// TestMatchBlocks_ThresholdZero: threshold 0 admits every eligible edge
// (the maximum-weight assignment picks the best pairing), but scope-
// incompatible edges are still never selected: the sentinel marking an
// ineligible cell must not become a real match merely because the caller
// supplied threshold 0.
func TestMatchBlocks_ThresholdZero(t *testing.T) {
	// Two genuinely different but eligible blocks: matched at threshold 0.
	old := []ir.SemanticBlock{
		{ID: "o1", Kind: ir.KindFunction, Name: "a", Source: "function a() { return 1; }"},
	}
	new := []ir.SemanticBlock{
		{ID: "n1", Kind: ir.KindFunction, Name: "b", Source: "function b() { return 2; }"},
	}
	pairs, _, _ := MatchBlocks(old, new, 0)
	if len(pairs) != 1 {
		t.Errorf("threshold 0 must admit eligible edges, got %d pairs", len(pairs))
	}

	// Scope-incompatible pair (same name, same body, different parent
	// classes): the edge is ineligible and must NOT be selected at 0.
	oldNested := []ir.SemanticBlock{
		{ID: "method:foo:old", Kind: ir.KindMethod, Name: "foo", Parent: "class:A:0", Source: "foo() { return 1; }"},
	}
	newNested := []ir.SemanticBlock{
		{ID: "method:foo:new", Kind: ir.KindMethod, Name: "foo", Parent: "class:B:0", Source: "foo() { return 1; }"},
	}
	pairs, uOld, uNew := MatchBlocks(oldNested, newNested, 0)
	if len(pairs) != 0 {
		t.Errorf("scope-incompatible edge must never be selected at threshold 0, got %d pairs: %+v", len(pairs), pairs)
	}
	if len(uOld) != 1 || len(uNew) != 1 {
		t.Errorf("both sides of the ineligible pair stay unmatched, got old=%d new=%d", len(uOld), len(uNew))
	}
}

// TestMatchBlocks_ThresholdOne: threshold 1 admits only perfect matches
// (identical name, kind, and text).
func TestMatchBlocks_ThresholdOne(t *testing.T) {
	identical := []ir.SemanticBlock{
		{ID: "o1", Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"},
	}
	twin := []ir.SemanticBlock{
		{ID: "n1", Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"},
	}
	pairs, _, _ := MatchBlocks(identical, twin, 1)
	if len(pairs) != 1 {
		t.Errorf("threshold 1 must admit an identical pair, got %d", len(pairs))
	}

	changed := []ir.SemanticBlock{
		{ID: "n2", Kind: ir.KindFunction, Name: "foo2", Source: "function foo2() { return 2; }"},
	}
	pairs, uOld, uNew := MatchBlocks(identical, changed, 1)
	if len(pairs) != 0 || len(uOld) != 1 || len(uNew) != 1 {
		t.Errorf("threshold 1 must reject any below-1 pair, got pairs=%d uOld=%d uNew=%d", len(pairs), len(uOld), len(uNew))
	}
}

// TestMatchBlocks_InvalidThresholds: negative values behave like 0,
// values above 1 and NaN behave like 1 (normalized threshold contract).
func TestMatchBlocks_InvalidThresholds(t *testing.T) {
	identical := []ir.SemanticBlock{
		{ID: "o1", Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"},
	}
	twin := []ir.SemanticBlock{
		{ID: "n1", Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"},
	}
	different := []ir.SemanticBlock{
		{ID: "n2", Kind: ir.KindFunction, Name: "bar", Source: "function bar() { return 2; }"},
	}

	// -1 behaves like 0: the eligible identical pair matches, and so does
	// a moderately similar pair.
	pairs, _, _ := MatchBlocks(identical, twin, -1)
	if len(pairs) != 1 {
		t.Errorf("negative threshold must behave like 0 (identical pair), got %d", len(pairs))
	}
	pairs, _, _ = MatchBlocks(identical, different, -1)
	if len(pairs) != 1 {
		t.Errorf("negative threshold must behave like 0 (similar pair), got %d", len(pairs))
	}

	// 1.5 and NaN behave like 1: only the perfect match is admitted.
	for _, bad := range []float64{1.5, math.NaN()} {
		pairs, _, _ := MatchBlocks(identical, twin, bad)
		if len(pairs) != 1 {
			t.Errorf("threshold %v must behave like 1 (identical pair), got %d", bad, len(pairs))
		}
		pairs, _, _ = MatchBlocks(identical, different, bad)
		if len(pairs) != 0 {
			t.Errorf("threshold %v must behave like 1 (different pair), got %d", bad, len(pairs))
		}
	}
}

// TestMatchBlocks_CustomThresholdTrueSimilarity: a pair whose TRUE
// similarity lies between 0.3 and 0.5 (below the default threshold) must
// be matched at a custom threshold of 0.3 — and its returned confidence
// must be the exact similarity value used for the thresholding decision.
// The historical body-similarity cutoff capped far-apart bodies at the
// default threshold's band, inflating this pair's score above 0.5 and
// falsely matching it at 0.5 with a wrong confidence.
func TestMatchBlocks_CustomThresholdTrueSimilarity(t *testing.T) {
	oldSrc := "function ab() { abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ }"
	newSrc := "function ac() { go }"
	old := []ir.SemanticBlock{{ID: "o1", Kind: ir.KindFunction, Name: "ab", Source: oldSrc}}
	new := []ir.SemanticBlock{{ID: "n1", Kind: ir.KindFunction, Name: "ac", Source: newSrc}}

	// Expected confidence: the full-distance weighted similarity.
	oldText, newText := DistanceText(oldSrc), DistanceText(newSrc)
	maxLen := max(len(oldText), len(newText))
	bodySim := 1 - float64(EditDistance(oldText, newText))/float64(maxLen)
	nameSim := nameSimilarity("ab", "ac")
	want := 0.35*nameSim + 0.5*bodySim + 0.15 // same kind: kind term is 1.0
	if want <= 0.3 || want >= 0.5 {
		t.Fatalf("test setup: expected true similarity in (0.3, 0.5), got %v (body %v, name %v)", want, bodySim, nameSim)
	}

	// Custom threshold below 0.5: the pair matches, and the returned
	// confidence is the same value used for the thresholding decision.
	pairs, _, _ := MatchBlocks(old, new, 0.3)
	if len(pairs) != 1 {
		t.Fatalf("threshold 0.3 must match the pair (true similarity %v), got %d pairs", want, len(pairs))
	}
	if got := pairs[0].Confidence; got != want {
		t.Errorf("returned confidence %v must equal the thresholded similarity %v", got, want)
	}

	// At the default threshold (0.5) and above, the same pair must stay
	// unmatched: the true similarity is below 0.5 and must not be
	// inflated by a score capped for the default threshold.
	for _, th := range []float64{0.5, 0.7} {
		pairs, _, _ := MatchBlocks(old, new, th)
		if len(pairs) != 0 {
			t.Errorf("threshold %v must reject the pair whose true similarity is %v, got %d pairs", th, want, len(pairs))
		}
	}
}

// TestMatchBlocks_HierarchyExcludesComments: a comment nested under a
// matched container must never be paired against a code child, even at
// threshold 0 where every eligible edge is admitted. Hierarchical
// code-child candidate lists exclude comments, so the comment reaches only
// the exact-comment stage and the comment-only similarity table (F9).
func TestMatchBlocks_HierarchyExcludesComments(t *testing.T) {
	mk := func(id string, kind ir.BlockKind, name, parent, src string) ir.SemanticBlock {
		return ir.SemanticBlock{ID: id, Kind: kind, Name: name, Parent: parent, Source: src}
	}
	// The comment and the code child share nearly identical text: with a
	// comment in the hierarchical table, threshold 0 would pair them.
	commentSrc := "// renderWidget() { return 2; }"
	codeSrc := "renderWidget() { return 2; }"
	old := []ir.SemanticBlock{
		mk("class:old:0", ir.KindClass, "A", "", "class A {}"),
		mk("comment:old:12", ir.KindComment, "", "class:old:0", commentSrc),
		mk("method:old:44", ir.KindMethod, "render", "class:old:0", "render() { return 1; }"),
	}
	new := []ir.SemanticBlock{
		mk("class:new:0", ir.KindClass, "A", "", "class A {}"),
		mk("method:new:12", ir.KindMethod, "render", "class:new:0", "render() { return 1; }"),
		mk("method:new:30", ir.KindMethod, "renderWidget", "class:new:0", codeSrc),
	}

	pairs, uOld, uNew := MatchBlocks(old, new, 0)

	// No produced pair may join a comment with a code block.
	for _, p := range pairs {
		if (p.Old.Kind == ir.KindComment) != (p.New.Kind == ir.KindComment) {
			t.Fatalf("comment/code pair produced: %s → %s (%+v)", p.Old.Kind, p.New.Kind, p)
		}
	}
	// The comment has no comment partner: it must stay unmatched rather
	// than pair with the similar code child.
	if len(uOld) != 1 || uOld[0].ID != "comment:old:12" {
		t.Errorf("the comment must stay unmatched (no comment partner), got %v", uOld)
	}
	// The code child must not be consumed by the comment: render already
	// has its exact-name partner, so the code child stays unmatched.
	if len(uNew) != 1 || uNew[0].ID != "method:new:30" {
		t.Errorf("the code child must stay unmatched, got %v", uNew)
	}
}

// TestMatchBlocks_CommentPairsWithCommentNotCode: a reworded comment
// nested under a matched container pairs with its reworded counterpart via
// the comment-only similarity table even when a code child with nearly
// identical text exists on the new side (F9).
func TestMatchBlocks_CommentPairsWithCommentNotCode(t *testing.T) {
	mk := func(id string, kind ir.BlockKind, name, parent, src string) ir.SemanticBlock {
		return ir.SemanticBlock{ID: id, Kind: kind, Name: name, Parent: parent, Source: src}
	}
	old := []ir.SemanticBlock{
		mk("class:old:0", ir.KindClass, "A", "", "class A {}"),
		mk("comment:old:12", ir.KindComment, "", "class:old:0", "// renderWidget() { return 2; }"),
		mk("method:old:44", ir.KindMethod, "render", "class:old:0", "render() { return 1; }"),
	}
	new := []ir.SemanticBlock{
		mk("class:new:0", ir.KindClass, "A", "", "class A {}"),
		mk("comment:new:12", ir.KindComment, "", "class:new:0", "// renderWidget() { return 3; }"),
		mk("method:new:30", ir.KindMethod, "render", "class:new:0", "render() { return 1; }"),
		mk("method:new:60", ir.KindMethod, "renderWidget", "class:new:0", "renderWidget() { return 2; }"),
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	var commentPair *ir.CorrelatedPair
	for i := range pairs {
		p := &pairs[i]
		if p.Old.Kind == ir.KindComment || p.New.Kind == ir.KindComment {
			commentPair = p
		}
	}
	if commentPair == nil {
		t.Fatalf("reworded comment must pair with its counterpart via the comment table; pairs=%+v uOld=%d uNew=%d",
			pairs, len(uOld), len(uNew))
	}
	if commentPair.Old.Kind != ir.KindComment || commentPair.New.Kind != ir.KindComment {
		t.Fatalf("comment pair must join two comments, got %s → %s",
			commentPair.Old.Kind, commentPair.New.Kind)
	}
	if commentPair.MatchType != ir.MatchSimilarity {
		t.Errorf("changed comment must be similarity-matched, got %s", commentPair.MatchType)
	}
	// The similar code child stays unmatched (its text is not a comment
	// partner and render already has its exact partner).
	if len(uNew) != 1 || uNew[0].ID != "method:new:60" {
		t.Errorf("the code child must stay unmatched, got %v", uNew)
	}
}

func TestCompatible(t *testing.T) {
	tests := []struct {
		a, b     ir.BlockKind
		expected bool
	}{
		{ir.KindFunction, ir.KindFunction, true},
		{ir.KindFunction, ir.KindMethod, true},
		{ir.KindFunction, ir.KindArrowFunc, true},
		{ir.KindFunction, ir.KindClass, false},
		{ir.KindConstant, ir.KindVariable, true},
		{ir.KindImport, ir.KindImport, true},
		{ir.KindImport, ir.KindFunction, false},
		{ir.KindClass, ir.KindClass, true},
		{ir.KindJSX, ir.KindJSX, true},
		{ir.KindTemplate, ir.KindTemplate, true},
		{ir.KindTemplate, ir.KindJSX, true}, // Lit element → JSX conversion
		{ir.KindJSX, ir.KindTemplate, true},
		{ir.KindTemplate, ir.KindFunction, false},
		{ir.KindJSX, ir.KindComment, false},
		{ir.KindInterface, ir.KindInterface, true},
		{ir.KindJSX, ir.KindJSX, true},
		{ir.KindComment, ir.KindComment, true},
	}
	for _, tt := range tests {
		if got := Compatible(tt.a, tt.b); got != tt.expected {
			t.Errorf("Compatible(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.expected)
		}
	}
}

// TestMatchBlocks_LitTemplateMatchesJSX: a Lit html-template element (old)
// must pair with the JSX element it was converted to — even though the old
// side is a template block and the new side a JSX block — while an unrelated
// sibling JSX element stays unmatched. The canonical element shape makes the
// sprite pair score well above the outer-wrapper pair.
func TestMatchBlocks_LitTemplateMatchesJSX(t *testing.T) {
	lit := "<div id=\"sprite\" ?bidir=${d.bidir}\n    @wheel=${{handleEvent: e => t.wheel_event (e), passive: false }}\n    @pointerdown=\"${t.pointerdown}\"\n    @dblclick=\"${Util.prevent_event}\">\n  </div>"
	jsx := "<div id=\"sprite\" bool:bidir={bidir()} ref={sprite_el}\n        onWheel={wheel_event}\n        onPointerDown={pointerdown}\n        onDblClick={Util.prevent_event}\n      >\n      </div>"
	outer := "<div class=\"b-knob\" aria-disabled={props.disabled || undefined} ref={root_el}>\n      <div id=\"sprite\">\n      </div>\n    </div>"

	old := []ir.SemanticBlock{
		{ID: "template:div:old:0", Kind: ir.KindTemplate, Name: "div", Source: lit},
	}
	new := []ir.SemanticBlock{
		{ID: "jsx:div:new:0", Kind: ir.KindJSX, Name: "div", Source: outer},
		{ID: "jsx:div:new:1", Kind: ir.KindJSX, Name: "div", Source: jsx},
	}

	pairs, unmatchedOld, unmatchedNew := MatchBlocks(old, new, SimilarityThreshold)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 matched pair, got %d (unmatchedOld=%d, unmatchedNew=%d)",
			len(pairs), len(unmatchedOld), len(unmatchedNew))
	}
	p := pairs[0]
	if p.Old.ID != "template:div:old:0" || p.New.ID != "jsx:div:new:1" {
		t.Errorf("template div must pair with the sprite JSX div, got old=%s new=%s", p.Old.ID, p.New.ID)
	}
	if len(unmatchedNew) != 1 || unmatchedNew[0].ID != "jsx:div:new:0" {
		t.Errorf("the outer wrapper div must stay unmatched, got %v", unmatchedNew)
	}
}
