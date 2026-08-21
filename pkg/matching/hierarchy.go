// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"cmdiff/pkg/ir"
)

// makeTexts returns the distance text for every block (JSX and Lit template
// elements use the shared canonical element shape — see distanceTextFor).
func makeTexts(blocks []*ir.SemanticBlock) []string {
	texts := make([]string, len(blocks))
	for i, b := range blocks {
		texts[i] = distanceTextFor(b)
	}
	return texts
}

// distanceMatches runs the full similarity table over old×new and returns
// the maximum-weight 1:1 assignment above the threshold.
func distanceMatches(old, new []*ir.SemanticBlock, oldText, newText []string, threshold float64) []score {
	if len(old) == 0 || len(new) == 0 {
		return nil
	}
	return bestMatches(SimilarityTable(old, new, oldText, newText), threshold)
}

// matchesToPairs converts assignment scores into correlated pairs.
func matchesToPairs(old, new []*ir.SemanticBlock, matches []score) []ir.CorrelatedPair {
	pairs := make([]ir.CorrelatedPair, 0, len(matches))
	for _, s := range matches {
		pairs = append(pairs, ir.CorrelatedPair{
			Old:        old[s.old],
			New:        new[s.new],
			Confidence: s.sim,
			MatchType:  ir.MatchSimilarity,
		})
	}
	return pairs
}

// hierarchicalChildMatches: direct children of each matched container pair, recursively
// enqueuing new pairs; comments are excluded (they match only in comment stages).
func hierarchicalChildMatches(pairs []ir.CorrelatedPair, oldPtrs, newPtrs []*ir.SemanticBlock,
	usedOld, usedNew []bool, threshold float64, oldKey, newKey map[*ir.SemanticBlock]int) []score {

	oldChildren := make(map[string][]*ir.SemanticBlock)
	newChildren := make(map[string][]*ir.SemanticBlock)
	for i, b := range oldPtrs {
		if b.Parent != "" && b.Kind != ir.KindComment {
			oldChildren[b.Parent] = append(oldChildren[b.Parent], oldPtrs[i])
		}
	}
	for j, b := range newPtrs {
		if b.Parent != "" && b.Kind != ir.KindComment {
			newChildren[b.Parent] = append(newChildren[b.Parent], newPtrs[j])
		}
	}

	var matches []score
	queue := make([]*ir.CorrelatedPair, 0, len(pairs))
	for i := range pairs {
		if pairs[i].Old != nil && pairs[i].New != nil {
			queue = append(queue, &pairs[i])
		}
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p.Old == nil || p.New == nil {
			continue
		}
		var oldKids, newKids []*ir.SemanticBlock
		for _, b := range oldChildren[p.Old.ID] {
			if !usedOld[oldKey[b]] {
				oldKids = append(oldKids, b)
			}
		}
		for _, b := range newChildren[p.New.ID] {
			if !usedNew[newKey[b]] {
				newKids = append(newKids, b)
			}
		}
		sub := distanceMatches(oldKids, newKids, makeTexts(oldKids), makeTexts(newKids), threshold)
		// Convert kid-relative indexes to global ones (mutate in place), mark used, enqueue new pairs.
		for i := range sub {
			oi := oldKey[oldKids[sub[i].old]]
			ni := newKey[newKids[sub[i].new]]
			sub[i].old = oi
			sub[i].new = ni
			usedOld[oi] = true
			usedNew[ni] = true
			queue = append(queue, &ir.CorrelatedPair{Old: oldPtrs[oi], New: newPtrs[ni]})
		}
		matches = append(matches, sub...)
	}
	return matches
}

// scopeRestrictedMatches: residual table admitting only identical-ancestor-scope edges (ineligibleSim sentinel).
func scopeRestrictedMatches(old, new []*ir.SemanticBlock, oldScopes, newScopes []string, threshold float64) []score {
	if len(old) == 0 || len(new) == 0 {
		return nil
	}
	oldText := makeTexts(old)
	newText := makeTexts(new)
	table := SimilarityTable(old, new, oldText, newText)
	for i := range table {
		for j := range table[i] {
			if !scopePathsCompatible(oldScopes[i], newScopes[j]) {
				table[i][j] = ineligibleSim // never admissible at any valid threshold
			}
		}
	}
	return bestMatches(table, threshold)
}
