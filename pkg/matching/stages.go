// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package matching holds the identity and distance stages shared by all correlation algorithms.
package matching

import (
	"strings"

	"cmdiff/pkg/ir"
)

// isRootLevel treats namespace members as top-level matching candidates.
func isRootLevel(b *ir.SemanticBlock, byID map[string]*ir.SemanticBlock) bool {
	id := b.Parent
	for id != "" {
		parent := byID[id]
		if parent == nil || parent.Kind != ir.KindNamespace {
			return false
		}
		id = parent.Parent
	}
	return true
}

// MatchBlocks: exact name, exact comments, then distance stages (top-level containers,
// recursive hierarchical children, scope-restricted residual, comment-only table).
func MatchBlocks(oldBlocks, newBlocks []ir.SemanticBlock, threshold float64) ([]ir.CorrelatedPair, []*ir.SemanticBlock, []*ir.SemanticBlock) {
	threshold = normalizeThreshold(threshold)

	oldPtrs := make([]*ir.SemanticBlock, len(oldBlocks))
	newPtrs := make([]*ir.SemanticBlock, len(newBlocks))
	for i := range oldBlocks {
		oldPtrs[i] = &oldBlocks[i]
	}
	for i := range newBlocks {
		newPtrs[i] = &newBlocks[i]
	}

	// Full ancestor scope paths per side (Parent IDs are only meaningful within one document).
	oldByID := make(map[string]*ir.SemanticBlock, len(oldPtrs))
	for _, b := range oldPtrs {
		oldByID[b.ID] = b
	}
	newByID := make(map[string]*ir.SemanticBlock, len(newPtrs))
	for _, b := range newPtrs {
		newByID[b.ID] = b
	}
	oldScopes := make([]string, len(oldPtrs))
	for i, b := range oldPtrs {
		oldScopes[i] = strings.Join(ancestorScopes(b, oldByID), "\x00")
	}
	newScopes := make([]string, len(newPtrs))
	for i, b := range newPtrs {
		newScopes[i] = strings.Join(ancestorScopes(b, newByID), "\x00")
	}
	oldTextFull := makeTexts(oldPtrs)
	newTextFull := makeTexts(newPtrs)

	var pairs []ir.CorrelatedPair
	usedOld := make([]bool, len(oldPtrs))
	usedNew := make([]bool, len(newPtrs))

	// Stage 1: exact name + kind; confidence from the shared distance calculation, not blindly 1.0.
	namePairs, oldRem, newRem := ExactName(oldPtrs, newPtrs, oldScopes, newScopes)
	oldKey := make(map[*ir.SemanticBlock]int, len(oldPtrs))
	for i, b := range oldPtrs {
		oldKey[b] = i
	}
	newKey := make(map[*ir.SemanticBlock]int, len(newPtrs))
	for i, b := range newPtrs {
		newKey[b] = i
	}
	for i := range namePairs {
		p := &namePairs[i]
		p.Confidence = BlockSimilarity(p.Old, p.New,
			oldTextFull[oldKey[p.Old]], newTextFull[newKey[p.New]])
		usedOld[oldKey[p.Old]] = true
		usedNew[newKey[p.New]] = true
	}
	pairs = append(pairs, namePairs...)

	// Stage 2: exact comment text (same container preferred, else source order).
	commentPairs, oldRem, newRem := exactComments(oldRem, newRem,
		commentScopeKeys(oldRem, oldPtrs), commentScopeKeys(newRem, newPtrs))
	for i := range commentPairs {
		usedOld[oldKey[commentPairs[i].Old]] = true
		usedNew[newKey[commentPairs[i].New]] = true
	}
	pairs = append(pairs, commentPairs...)

	// Stage 3: distance matching over code — 3a top-level containers, 3b hierarchical children, 3c scope-restricted residual.
	var codeOld, codeNew []*ir.SemanticBlock
	for _, b := range oldRem {
		if b.Kind != ir.KindComment {
			codeOld = append(codeOld, b)
		}
	}
	for _, b := range newRem {
		if b.Kind != ir.KindComment {
			codeNew = append(codeNew, b)
		}
	}

	var topOld, topNew []*ir.SemanticBlock
	for _, b := range codeOld {
		if isRootLevel(b, oldByID) {
			topOld = append(topOld, b)
		}
	}
	for _, b := range codeNew {
		if isRootLevel(b, newByID) {
			topNew = append(topNew, b)
		}
	}

	// 3a: full table over top-level code blocks.
	topOldText := makeTexts(topOld)
	topNewText := makeTexts(topNew)
	topMatches := distanceMatches(topOld, topNew, topOldText, topNewText, threshold)
	for _, s := range topMatches {
		usedOld[oldKey[topOld[s.old]]] = true
		usedNew[newKey[topNew[s.new]]] = true
	}
	pairs = append(pairs, matchesToPairs(topOld, topNew, topMatches)...)

	// 3b: recursive hierarchical matching inside matched container pairs; blocks marked used as the queue is built.
	childMatches := hierarchicalChildMatches(pairs, oldPtrs, newPtrs, usedOld, usedNew, threshold, oldKey, newKey)
	pairs = append(pairs, matchesToPairs(oldPtrs, newPtrs, childMatches)...)

	// 3c: residual table restricted to identical ancestor scope paths.
	var restOld, restNew []*ir.SemanticBlock
	var restOldScopes, restNewScopes []string
	for i, b := range codeOld {
		if !usedOld[oldKey[b]] {
			restOld = append(restOld, codeOld[i])
			restOldScopes = append(restOldScopes, oldScopes[oldKey[b]])
		}
	}
	for j, b := range codeNew {
		if !usedNew[newKey[b]] {
			restNew = append(restNew, codeNew[j])
			restNewScopes = append(restNewScopes, newScopes[newKey[b]])
		}
	}
	restMatches := scopeRestrictedMatches(restOld, restNew, restOldScopes, restNewScopes, threshold)
	for _, s := range restMatches {
		usedOld[oldKey[restOld[s.old]]] = true
		usedNew[newKey[restNew[s.new]]] = true
	}
	pairs = append(pairs, matchesToPairs(restOld, restNew, restMatches)...)

	// Stage 3d: rescue hoisted inner callables across scopes when both containers are orphaned;
	// rules in hoisted.go, rationale in [../../doc/pipeline.md](pipeline.md).
	rescuePairs := hoistedCallableMatches(oldPtrs, newPtrs, usedOld, usedNew, oldByID, newByID, oldKey, newKey)
	pairs = append(pairs, rescuePairs...)
	// Rescued callables may themselves contain nested named functions:
	// match their children hierarchically like any other matched container pair.
	rescueChildMatches := hierarchicalChildMatches(rescuePairs, oldPtrs, newPtrs, usedOld, usedNew, threshold, oldKey, newKey)
	pairs = append(pairs, matchesToPairs(oldPtrs, newPtrs, rescueChildMatches)...)

	// Stage 4: remaining comments pair via their own similarity table (never against code).
	var commentOld, commentNew []*ir.SemanticBlock
	for _, b := range oldRem {
		if b.Kind == ir.KindComment {
			commentOld = append(commentOld, b)
		}
	}
	for _, b := range newRem {
		if b.Kind == ir.KindComment {
			commentNew = append(commentNew, b)
		}
	}
	commentMatches := bestMatches(commentSimilarityTable(commentOld, commentNew,
		makeTexts(commentOld), makeTexts(commentNew)), threshold)
	for _, s := range commentMatches {
		usedOld[oldKey[commentOld[s.old]]] = true
		usedNew[newKey[commentNew[s.new]]] = true
	}
	pairs = append(pairs, matchesToPairs(commentOld, commentNew, commentMatches)...)

	// Remaining blocks are unmatched (deletion/addition candidates).
	var unmatchedOld, unmatchedNew []*ir.SemanticBlock
	for i, b := range oldPtrs {
		if !usedOld[i] {
			unmatchedOld = append(unmatchedOld, b)
		}
	}
	for j, b := range newPtrs {
		if !usedNew[j] {
			unmatchedNew = append(unmatchedNew, b)
		}
	}

	return pairs, unmatchedOld, unmatchedNew
}
