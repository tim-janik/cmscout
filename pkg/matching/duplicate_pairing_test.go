// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"testing"

	"cmscout/pkg/ir"
)

// TestMatchBlocks_NoDuplicatePairing: every block may be paired at most
// once across ALL stages. The hierarchical child stage used to convert
// child-relative indexes through a range-copied score, pairing unrelated
// already-used blocks a second time (e.g. the same import appearing as
// both an exact-name pair and a similarity pair).
func TestMatchBlocks_NoDuplicatePairing(t *testing.T) {
	old := []ir.SemanticBlock{
		{ID: "o:import:1", Kind: ir.KindImport, Name: "x", Parent: "", Source: "import { x } from 'x';"},
		{ID: "o:class:2", Kind: ir.KindClass, Name: "C", Parent: "", Source: "class C {\n  m() { return 1; }\n}"},
		{ID: "o:method:3", Kind: ir.KindMethod, Name: "m", Parent: "class:C:2", Source: "m() { return 1; }"},
	}
	new := []ir.SemanticBlock{
		{ID: "n:import:1", Kind: ir.KindImport, Name: "x", Parent: "", Source: "import { x } from 'x';"},
		{ID: "n:class:2", Kind: ir.KindClass, Name: "C", Parent: "", Source: "class C {\n  m() { return 2; }\n}"},
		{ID: "n:method:3", Kind: ir.KindMethod, Name: "m", Parent: "class:C:2", Source: "m() { return 2; }"},
	}

	pairs, uOld, uNew := MatchBlocks(old, new, SimilarityThreshold)

	seenOld := map[string]int{}
	seenNew := map[string]int{}
	for _, p := range pairs {
		if p.Old != nil {
			seenOld[p.Old.ID]++
		}
		if p.New != nil {
			seenNew[p.New.ID]++
		}
	}
	for id, n := range seenOld {
		if n > 1 {
			t.Errorf("old block %s paired %d times", id, n)
		}
	}
	for id, n := range seenNew {
		if n > 1 {
			t.Errorf("new block %s paired %d times", id, n)
		}
	}
	if len(uOld) != 0 || len(uNew) != 0 {
		t.Errorf("all blocks should be matched, unmatched old=%d new=%d", len(uOld), len(uNew))
	}
	if len(pairs) != 3 {
		t.Errorf("expected exactly 3 pairs, got %d", len(pairs))
	}
}
