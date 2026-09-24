// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package report

import (
	"sort"

	"cmdiff/pkg/ir"
)

// computeMovedPairs: pairs outside the old-vs-new order LCS were genuinely reordered;
// comments move only when their enclosing container changed (never by rewording).
func computeMovedPairs(pairs []ir.CorrelatedPair) map[*ir.CorrelatedPair]bool {
	type entry struct {
		ptr   *ir.CorrelatedPair
		oldLn int // 1-based old start line
		newLn int // 1-based new start line
	}
	var entries []entry
	// containers: matched non-comment pairs, used for the ordering LCS and for comment containment.
	var containers []*ir.CorrelatedPair
	moved := make(map[*ir.CorrelatedPair]bool)
	for i := range pairs {
		p := &pairs[i]
		if p.Supplemental || p.Old == nil || p.New == nil {
			continue
		}
		if p.Old.Kind == ir.KindComment {
			continue // handled after the container index is built
		}
		containers = append(containers, p)
		entries = append(entries, entry{
			ptr:   p,
			oldLn: int(p.Old.Span.StartLine) + 1,
			newLn: int(p.New.Span.StartLine) + 1,
		})
	}
	if len(entries) < 2 {
		return commentMovedPairs(pairs, containers, moved)
	}

	// Orders index entries, not the original pairs slice (which has gaps).
	oldOrder := make([]int, len(entries))
	newOrder := make([]int, len(entries))
	for i := range entries {
		oldOrder[i] = i
		newOrder[i] = i
	}
	sort.SliceStable(oldOrder, func(a, b int) bool {
		return entries[oldOrder[a]].oldLn < entries[oldOrder[b]].oldLn
	})
	sort.SliceStable(newOrder, func(a, b int) bool {
		return entries[newOrder[a]].newLn < entries[newOrder[b]].newLn
	})

	// LCS of the two orderings = LIS of old-order ranks; avoids a quadratic table.
	newRank := make(map[int]int, len(newOrder))
	for rank, entryIndex := range newOrder {
		newRank[entryIndex] = rank
	}
	sequence := make([]int, len(oldOrder))
	for i, entryIndex := range oldOrder {
		sequence[i] = newRank[entryIndex]
	}
	tails := make([]int, 0, len(sequence))
	previous := make([]int, len(sequence))
	for i := range previous {
		previous[i] = -1
	}
	for i, value := range sequence {
		pos := sort.Search(len(tails), func(j int) bool {
			return sequence[tails[j]] >= value
		})
		if pos == len(tails) {
			tails = append(tails, i)
		} else {
			tails[pos] = i
		}
		if pos > 0 {
			previous[i] = tails[pos-1]
		}
	}

	// Backtrack the LIS; elements outside it were reordered (moved).
	inLCS := make(map[int]bool, len(tails))
	if len(tails) > 0 {
		for i := tails[len(tails)-1]; i >= 0; i = previous[i] {
			inLCS[oldOrder[i]] = true
		}
	}

	for entryIndex, e := range entries {
		if !inLCS[entryIndex] {
			moved[e.ptr] = true
		}
	}
	return commentMovedPairs(pairs, containers, moved)
}

// commentMovedPairs: span-derived container changed between sides ⇒ moved; rewording never is.
func commentMovedPairs(pairs []ir.CorrelatedPair, containers []*ir.CorrelatedPair, moved map[*ir.CorrelatedPair]bool) map[*ir.CorrelatedPair]bool {
	for i := range pairs {
		p := &pairs[i]
		if p.Supplemental || p.Old == nil || p.New == nil || p.Old.Kind != ir.KindComment {
			continue
		}
		if enclosingContainer(p, containers, true) != enclosingContainer(p, containers, false) {
			moved[p] = true
		}
	}
	return moved
}

// enclosingContainer: deepest (smallest-span) pair containing the comment's start byte.
func enclosingContainer(cmt *ir.CorrelatedPair, containers []*ir.CorrelatedPair, old bool) *ir.CorrelatedPair {
	start := cmt.New.Span.StartByte
	if old {
		start = cmt.Old.Span.StartByte
	}
	var best *ir.CorrelatedPair
	bestSize := ^uint(0)
	for _, c := range containers {
		var s, e uint
		if old {
			s, e = c.Old.Span.StartByte, c.Old.Span.EndByte
		} else {
			s, e = c.New.Span.StartByte, c.New.Span.EndByte
		}
		if start >= s && start < e {
			if size := e - s; size < bestSize {
				best = c
				bestSize = size
			}
		}
	}
	return best
}
