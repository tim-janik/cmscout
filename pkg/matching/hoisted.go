// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"cmscout/pkg/ir"
)

// hoistedCallableKind: callables that do not capture their enclosing scope (functions hoist,
// methods belong to their class); arrows and lambdas close over scope and are excluded.
func hoistedCallableKind(k ir.BlockKind) bool {
	switch k {
	case ir.KindFunction, ir.KindMethod, ir.KindObjectMethod:
		return true
	default:
		return false
	}
}

// nearestContainer returns the closest non-namespace ancestor, or nil for
// top-level blocks (namespace members count as top-level).
func nearestContainer(b *ir.SemanticBlock, byID map[string]*ir.SemanticBlock) *ir.SemanticBlock {
	id := b.Parent
	for id != "" {
		parent := byID[id]
		if parent == nil {
			return nil
		}
		if parent.Kind != ir.KindNamespace {
			return parent
		}
		id = parent.Parent
	}
	return nil
}

// containerOrphaned reports whether the block's enclosing container is
// itself still unmatched after the earlier stages.
func containerOrphaned(b *ir.SemanticBlock, byID map[string]*ir.SemanticBlock, used []bool, key map[*ir.SemanticBlock]int) bool {
	container := nearestContainer(b, byID)
	if container == nil {
		return false
	}
	i, ok := key[container]
	return ok && i >= 0 && i < len(used) && !used[i]
}

// hoistedCallableMatches rescues orphaned named callables by exact name across scopes when both
// containers failed to match; children of matched containers keep hierarchical authority.
func hoistedCallableMatches(
	oldPtrs, newPtrs []*ir.SemanticBlock,
	usedOld, usedNew []bool,
	oldByID, newByID map[string]*ir.SemanticBlock,
	oldKey, newKey map[*ir.SemanticBlock]int,
) []ir.CorrelatedPair {
	oldGroups := make(map[string][]int)
	for i, b := range oldPtrs {
		if usedOld[i] || b.Name == "" || !hoistedCallableKind(b.Kind) ||
			nearestContainer(b, oldByID) == nil {
			continue
		}
		oldGroups[b.Name] = append(oldGroups[b.Name], i)
	}
	newGroups := make(map[string][]int)
	for j, b := range newPtrs {
		if usedNew[j] || b.Name == "" || !hoistedCallableKind(b.Kind) ||
			nearestContainer(b, newByID) == nil {
			continue
		}
		newGroups[b.Name] = append(newGroups[b.Name], j)
	}

	var pairs []ir.CorrelatedPair
	for name, oldIndexes := range oldGroups {
		newIndexes := newGroups[name]
		if len(newIndexes) == 0 {
			continue
		}
		table := make([][]float64, len(oldIndexes))
		for i, oi := range oldIndexes {
			table[i] = make([]float64, len(newIndexes))
			for j, nj := range newIndexes {
				if !containerOrphaned(oldPtrs[oi], oldByID, usedOld, oldKey) ||
					!containerOrphaned(newPtrs[nj], newByID, usedNew, newKey) {
					table[i][j] = ineligibleSim
					continue
				}
				table[i][j] = BlockSimilarity(oldPtrs[oi], newPtrs[nj],
					distanceTextFor(oldPtrs[oi]), distanceTextFor(newPtrs[nj]))
			}
		}
		for _, m := range bestMatches(table, 0) {
			oi, nj := oldIndexes[m.old], newIndexes[m.new]
			usedOld[oi] = true
			usedNew[nj] = true
			pairs = append(pairs, ir.CorrelatedPair{
				Old:        oldPtrs[oi],
				New:        newPtrs[nj],
				Confidence: m.sim,
				MatchType:  ir.MatchExactName,
			})
		}
	}
	return pairs
}
