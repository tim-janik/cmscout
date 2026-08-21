// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"strings"

	"cmdiff/pkg/ir"
)

// scopeKey: Parent ID minus the byte-offset suffix — conservative container identity, only prevents pairings.
func scopeKey(parentID string) string {
	if parentID == "" {
		return ""
	}
	lastColon := strings.LastIndex(parentID, ":")
	if lastColon > 0 {
		off := parentID[lastColon+1:]
		if off != "" {
			digitsOnly := true
			for _, r := range off {
				if r < '0' || r > '9' {
					digitsOnly = false
					break
				}
			}
			if digitsOnly {
				return parentID[:lastColon]
			}
		}
	}
	return parentID
}

// ancestorScopes: offset-stripped scope path from the parent upward; empty for top-level blocks.
func ancestorScopes(b *ir.SemanticBlock, byID map[string]*ir.SemanticBlock) []string {
	var path []string
	id := b.Parent
	for id != "" {
		path = append(path, scopeKey(id))
		parent := byID[id]
		if parent == nil {
			break
		}
		id = parent.Parent
	}
	return path
}

// scopePathsCompatible: identical full ancestor scope paths only — conservative, never proves identity.
func scopePathsCompatible(a, b string) bool {
	return a == b
}

// commentScopeKeys: span-derived nearest container per comment (comments have no Parent ID).
func commentScopeKeys(comments []*ir.SemanticBlock, all []*ir.SemanticBlock) []string {
	keys := make([]string, len(comments))
	for i, comment := range comments {
		bestSize := ^uint(0)
		for _, candidate := range all {
			if candidate.Kind == ir.KindComment || candidate == comment {
				continue
			}
			start, end := candidate.Span.StartByte, candidate.Span.EndByte
			if start >= end || comment.Span.StartByte < start || comment.Span.StartByte >= end {
				continue
			}
			if size := end - start; size < bestSize {
				bestSize = size
				keys[i] = string(candidate.Kind) + "\x00" + candidate.Name
			}
		}
	}
	return keys
}

// ExactName: same name+kind within compatible ancestor scopes only (never cross-container).
func ExactName(old, new []*ir.SemanticBlock, oldScopes, newScopes []string) (pairs []ir.CorrelatedPair, remainingOld, remainingNew []*ir.SemanticBlock) {
	matchedNew := make([]bool, len(new))
	newForOld := make([]int, len(old))
	for i := range newForOld {
		newForOld[i] = -1
	}

	type identity struct {
		kind  ir.BlockKind
		name  string
		scope string
	}
	scopeAt := func(scopes []string, i int) string {
		if i < len(scopes) {
			return scopes[i]
		}
		return ""
	}

	oldGroups := make(map[identity][]int)
	newGroups := make(map[identity][]int)
	var groupOrder []identity
	seenGroups := make(map[identity]bool)
	for i, block := range old {
		if block.Name == "" {
			continue
		}
		key := identity{kind: block.Kind, name: block.Name, scope: scopeAt(oldScopes, i)}
		oldGroups[key] = append(oldGroups[key], i)
		if !seenGroups[key] {
			groupOrder = append(groupOrder, key)
			seenGroups[key] = true
		}
	}
	for j, block := range new {
		if block.Name == "" {
			continue
		}
		key := identity{kind: block.Kind, name: block.Name, scope: scopeAt(newScopes, j)}
		newGroups[key] = append(newGroups[key], j)
	}

	// Duplicate identities need body evidence to avoid pairing reordered declarations by position.
	for _, key := range groupOrder {
		oldIndexes := oldGroups[key]
		newIndexes := newGroups[key]
		if len(newIndexes) == 0 {
			continue
		}
		table := make([][]float64, len(oldIndexes))
		for i, oldIndex := range oldIndexes {
			table[i] = make([]float64, len(newIndexes))
			for j, newIndex := range newIndexes {
				table[i][j] = BlockSimilarity(old[oldIndex], new[newIndex],
					distanceTextFor(old[oldIndex]), distanceTextFor(new[newIndex]))
			}
		}
		for _, match := range bestMatches(table, 0) {
			oldIndex := oldIndexes[match.old]
			newIndex := newIndexes[match.new]
			newForOld[oldIndex] = newIndex
			matchedNew[newIndex] = true
		}
	}

	for i, oldBlock := range old {
		if newIndex := newForOld[i]; newIndex >= 0 {
			pairs = append(pairs, ir.CorrelatedPair{
				Old:        oldBlock,
				New:        new[newIndex],
				Confidence: 1,
				MatchType:  ir.MatchExactName,
			})
		} else {
			remainingOld = append(remainingOld, oldBlock)
		}
	}

	for j, newBlock := range new {
		if !matchedNew[j] {
			remainingNew = append(remainingNew, newBlock)
		}
	}
	return pairs, remainingOld, remainingNew
}

// exactComments: identical comment text pairs first; repeated strings prefer the same
// enclosing container, otherwise deterministic source order.
func exactComments(
	old, new []*ir.SemanticBlock, oldScopes, newScopes []string,
) (pairs []ir.CorrelatedPair, remainingOld, remainingNew []*ir.SemanticBlock) {
	matchedOld := make([]bool, len(old))
	matchedNew := make([]bool, len(new))
	oldOrdinals := commentOrdinals(old)
	newOrdinals := commentOrdinals(new)
	hasScopes := len(oldScopes) == len(old) && len(newScopes) == len(new)

	// Same-container candidates first, so an early nested duplicate cannot steal the top-level occurrence.
	matchOne := func(i int, sameScopeOnly bool) {
		oldBlock := old[i]
		if matchedOld[i] || oldBlock.Kind != ir.KindComment || oldBlock.Source == "" {
			return
		}
		best := -1
		bestOrderDistance := int(^uint(0) >> 1)
		for j, newBlock := range new {
			if matchedNew[j] || newBlock.Kind != ir.KindComment || oldBlock.Source != newBlock.Source {
				continue
			}
			matchingScope := hasScopes && oldScopes[i] == newScopes[j]
			if sameScopeOnly && !matchingScope {
				continue
			}
			orderDistance := absInt(oldOrdinals[i] - newOrdinals[j])
			if best < 0 || orderDistance < bestOrderDistance ||
				(orderDistance == bestOrderDistance && j < best) {
				best = j
				bestOrderDistance = orderDistance
			}
		}
		if best < 0 {
			return
		}
		pairs = append(pairs, ir.CorrelatedPair{
			Old:        oldBlock,
			New:        new[best],
			Confidence: 1,
			MatchType:  ir.MatchSimilarity,
		})
		matchedOld[i] = true
		matchedNew[best] = true
	}
	for i := range old {
		matchOne(i, true)
	}
	for i := range old {
		matchOne(i, false)
	}

	for i, oldBlock := range old {
		if !matchedOld[i] {
			remainingOld = append(remainingOld, oldBlock)
		}
	}
	for j, newBlock := range new {
		if !matchedNew[j] {
			remainingNew = append(remainingNew, newBlock)
		}
	}
	return pairs, remainingOld, remainingNew
}

func commentOrdinals(blocks []*ir.SemanticBlock) []int {
	ordinals := make([]int, len(blocks))
	n := 0
	for i, block := range blocks {
		ordinals[i] = -1
		if block.Kind == ir.KindComment {
			ordinals[i] = n
			n++
		}
	}
	return ordinals
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
