package correlate

import (
	"sort"
	"strings"

	"cmdiff/pkg/ir"
)

// AttachPrefixComments merges doc-prefix comment runs (e.g. `/// Do foo` before `void foo()`) into their
// component so they render as one diff unit; rules and rationale: [../../doc/prefix-comments.md](prefix-comments.md).
func AttachPrefixComments(result *ir.CorrelationResult) {
	if result == nil || len(result.Pairs) == 0 {
		return
	}
	// Build sorted all-blocks lists per side and block->pair maps.
	var oldAll, newAll []*ir.SemanticBlock
	oldBlockToPair := make(map[*ir.SemanticBlock]*ir.CorrelatedPair)
	newBlockToPair := make(map[*ir.SemanticBlock]*ir.CorrelatedPair)
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Supplemental {
			continue
		}
		if p.Old != nil {
			oldAll = append(oldAll, p.Old)
			oldBlockToPair[p.Old] = p
		}
		if p.New != nil {
			newAll = append(newAll, p.New)
			newBlockToPair[p.New] = p
		}
	}
	sort.Slice(oldAll, func(a, b int) bool {
		if oldAll[a].Span.StartByte != oldAll[b].Span.StartByte {
			return oldAll[a].Span.StartByte < oldAll[b].Span.StartByte
		}
		return oldAll[a].ID < oldAll[b].ID
	})
	sort.Slice(newAll, func(a, b int) bool {
		if newAll[a].Span.StartByte != newAll[b].Span.StartByte {
			return newAll[a].Span.StartByte < newAll[b].Span.StartByte
		}
		return newAll[a].ID < newAll[b].ID
	})

	// Build successor maps: comment block -> following component block (if prefix).
	oldCommentToSuccessor := make(map[*ir.SemanticBlock]*ir.SemanticBlock)
	newCommentToSuccessor := make(map[*ir.SemanticBlock]*ir.SemanticBlock)
	buildSuccessorMap := func(all []*ir.SemanticBlock, succMap map[*ir.SemanticBlock]*ir.SemanticBlock) {
		for i := 0; i+1 < len(all); i++ {
			cur := all[i]
			next := all[i+1]
			if cur.Kind != ir.KindComment || next.Kind == ir.KindComment {
				continue
			}
			if cur.Span.EndLine+1 != next.Span.StartLine {
				continue
			}
			succMap[cur] = next
		}
	}
	buildSuccessorMap(oldAll, oldCommentToSuccessor)
	buildSuccessorMap(newAll, newCommentToSuccessor)

	// commentPairsToSuppress marks pairs consumed by prefix attachment.
	commentPairsToSuppress := make(map[*ir.CorrelatedPair]bool)

	// commentRunBefore: maximal run of consecutive comments ending at last; comments are consecutive
	// when no blank line lies between them (prev.EndLine+1 == cur.StartLine).
	commentRunBefore := func(all []*ir.SemanticBlock, index map[*ir.SemanticBlock]int, last *ir.SemanticBlock) []*ir.SemanticBlock {
		if last == nil || last.Kind != ir.KindComment {
			return nil
		}
		idx, ok := index[last]
		if !ok {
			return nil
		}
		run := []*ir.SemanticBlock{last}
		for i := idx - 1; i >= 0; i-- {
			prev := all[i]
			if prev.Kind != ir.KindComment {
				break
			}
			if prev.Span.EndLine+1 != run[0].Span.StartLine {
				break
			}
			run = append([]*ir.SemanticBlock{prev}, run...)
		}
		return run
	}
	// commentRunThrough: the maximal run of consecutive comments containing start,
	// extending forward through contiguous comments first, then backward.
	commentRunThrough := func(all []*ir.SemanticBlock, index map[*ir.SemanticBlock]int, start *ir.SemanticBlock) []*ir.SemanticBlock {
		if start == nil || start.Kind != ir.KindComment {
			return nil
		}
		idx, ok := index[start]
		if !ok {
			return nil
		}
		end := idx
		for end+1 < len(all) && all[end+1].Kind == ir.KindComment && all[end].Span.EndLine+1 == all[end+1].Span.StartLine {
			end++
		}
		return commentRunBefore(all, index, all[end])
	}
	// trimRunToCounterparts keeps run blocks whose counterpart is in counterpartRun, so an omitted
	// block's text cannot render twice (inside the component diff and as a standalone pair).
	trimRunToCounterparts := func(run, counterpartRun []*ir.SemanticBlock, counterpartOf func(b *ir.SemanticBlock) *ir.SemanticBlock) []*ir.SemanticBlock {
		counterpartSet := make(map[*ir.SemanticBlock]bool, len(counterpartRun))
		for _, b := range counterpartRun {
			counterpartSet[b] = true
		}
		trimmed := make([]*ir.SemanticBlock, 0, len(run))
		for _, b := range run {
			c := counterpartOf(b)
			if c == nil || counterpartSet[c] {
				trimmed = append(trimmed, b)
			}
		}
		return trimmed
	}
	// counterpartOfNew/Old: the block paired with a block on the other side.
	counterpartOfNew := func(b *ir.SemanticBlock) *ir.SemanticBlock {
		if p := newBlockToPair[b]; p != nil {
			return p.Old
		}
		return nil
	}
	counterpartOfOld := func(b *ir.SemanticBlock) *ir.SemanticBlock {
		if p := oldBlockToPair[b]; p != nil {
			return p.New
		}
		return nil
	}
	matchedOldComments := func(newRun []*ir.SemanticBlock) []*ir.SemanticBlock {
		var out []*ir.SemanticBlock
		for _, b := range newRun {
			if p := newBlockToPair[b]; p != nil && p.Old != nil {
				out = append(out, p.Old)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Span.StartByte != out[j].Span.StartByte {
				return out[i].Span.StartByte < out[j].Span.StartByte
			}
			return out[i].ID < out[j].ID
		})
		return out
	}
	// isPrefixRun: comments form a contiguous run ending directly before comp (no blank lines between
	// comment lines or between run and component); an empty run is always attachable.
	isPrefixRun := func(comments []*ir.SemanticBlock, comp *ir.SemanticBlock) bool {
		if len(comments) == 0 {
			return true
		}
		if comp == nil {
			return false
		}
		for i := 1; i < len(comments); i++ {
			if comments[i-1].Span.EndLine+1 != comments[i].Span.StartLine {
				return false
			}
		}
		return comments[len(comments)-1].Span.EndLine+1 == comp.Span.StartLine
	}
	// prependComments: prepend each comment's source (in order) to comp's source and
	// reset comp's span start to the first comment; marks comp attached.
	prependComments := func(comp *ir.SemanticBlock, comments []*ir.SemanticBlock, attached map[*ir.SemanticBlock]bool, key *ir.SemanticBlock) {
		if comp == nil || len(comments) == 0 {
			return
		}
		var prefix strings.Builder
		for _, c := range comments {
			prefix.WriteString(c.Source)
			prefix.WriteString("\n")
		}
		comp.Source = prefix.String() + comp.Source
		first := comments[0]
		comp.Span.StartByte = first.Span.StartByte
		comp.Span.StartLine = first.Span.StartLine
		comp.Span.StartCol = first.Span.StartCol
		if key != nil {
			attached[key] = true
		}
	}
	// suppressRunPairs: mark every comment pair fully represented by the attached runs:
	// one-sided pairs in a run, matched pairs with old block in oldRun AND new block in newRun.
	suppressRunPairs := func(oldRun, newRun []*ir.SemanticBlock) {
		oldSet := make(map[*ir.SemanticBlock]bool, len(oldRun))
		for _, b := range oldRun {
			oldSet[b] = true
		}
		newSet := make(map[*ir.SemanticBlock]bool, len(newRun))
		for _, b := range newRun {
			newSet[b] = true
		}
		for _, b := range oldRun {
			if p := oldBlockToPair[b]; p != nil && (p.New == nil || newSet[p.New]) {
				commentPairsToSuppress[p] = true
			}
		}
		for _, b := range newRun {
			if p := newBlockToPair[b]; p != nil && (p.Old == nil || oldSet[p.Old]) {
				commentPairsToSuppress[p] = true
			}
		}
	}

	attachedOld := make(map[*ir.SemanticBlock]bool)
	attachedNew := make(map[*ir.SemanticBlock]bool)
	oldIndex := make(map[*ir.SemanticBlock]int, len(oldAll))
	for i, b := range oldAll {
		oldIndex[b] = i
	}
	newIndex := make(map[*ir.SemanticBlock]int, len(newAll))
	for i, b := range newAll {
		newIndex[b] = i
	}

	// Iterate over comment pairs to decide attachment.
	for i := range result.Pairs {
		cp := &result.Pairs[i]
		if cp.Supplemental {
			continue
		}
		isComment := false
		if cp.Old != nil && cp.Old.Kind == ir.KindComment {
			isComment = true
		}
		if cp.New != nil && cp.New.Kind == ir.KindComment {
			isComment = true
		}
		if !isComment {
			continue
		}
		var oldSucc, newSucc *ir.SemanticBlock
		var oldSuccPair, newSuccPair *ir.CorrelatedPair
		if cp.Old != nil {
			if succ, ok := oldCommentToSuccessor[cp.Old]; ok {
				oldSucc = succ
				oldSuccPair = oldBlockToPair[succ]
			}
		}
		if cp.New != nil {
			if succ, ok := newCommentToSuccessor[cp.New]; ok {
				newSucc = succ
				newSuccPair = newBlockToPair[succ]
			}
		}
		// Added comment: only new side has successor
		if cp.IsAdded() {
			if newSucc != nil && newSuccPair != nil && newSuccPair.New != nil && !attachedNew[newSucc] {
				newRun := commentRunBefore(newAll, newIndex, cp.New)
				oldRun := matchedOldComments(newRun)
				if len(oldRun) > 0 {
					// Attach the old side only if the full old run through the counterparts is a clean prefix;
					// otherwise keep the whole group standalone so no text renders twice.
					fullOldRun := commentRunThrough(oldAll, oldIndex, oldRun[len(oldRun)-1])
					if !isPrefixRun(fullOldRun, newSuccPair.Old) {
						continue
					}
					oldRun = fullOldRun
					// Trim newRun to blocks whose counterpart attaches too (or that have
					// none), so a counterpart left out of the old run stays standalone.
					newRun = trimRunToCounterparts(newRun, oldRun, counterpartOfNew)
				}
				prependComments(newSuccPair.New, newRun, attachedNew, newSucc)
				prependComments(newSuccPair.Old, oldRun, attachedOld, newSuccPair.Old)
				suppressRunPairs(oldRun, newRun)
			}
			continue
		}
		if cp.IsRemoved() {
			if oldSucc != nil && oldSuccPair != nil && oldSuccPair.Old != nil && !attachedOld[oldSucc] {
				oldRun := commentRunBefore(oldAll, oldIndex, cp.Old)
				var newRun []*ir.SemanticBlock
				for _, b := range oldRun {
					if p := oldBlockToPair[b]; p != nil && p.New != nil {
						newRun = append(newRun, p.New)
					}
				}
				sort.Slice(newRun, func(i, j int) bool {
					if newRun[i].Span.StartByte != newRun[j].Span.StartByte {
						return newRun[i].Span.StartByte < newRun[j].Span.StartByte
					}
					return newRun[i].ID < newRun[j].ID
				})
				if len(newRun) > 0 {
					// Symmetric to the added case: attach only if the full new run through the counterparts is
					// a clean prefix, keeping old-side blocks whose counterpart attaches too.
					fullNewRun := commentRunThrough(newAll, newIndex, newRun[len(newRun)-1])
					if !isPrefixRun(fullNewRun, oldSuccPair.New) {
						continue
					}
					newRun = fullNewRun
					oldRun = trimRunToCounterparts(oldRun, newRun, counterpartOfOld)
				}
				prependComments(oldSuccPair.Old, oldRun, attachedOld, oldSucc)
				prependComments(oldSuccPair.New, newRun, attachedNew, oldSuccPair.New)
				suppressRunPairs(oldRun, newRun)
			}
			continue
		}
		// Matched comment: both sides must have successors that are the same component pair
		if oldSucc == nil || newSucc == nil {
			continue
		}
		if oldSuccPair == nil || newSuccPair == nil || oldSuccPair != newSuccPair {
			// Successors are different components (e.g., comment moved between parents) -> do not attach
			continue
		}
		if attachedOld[oldSucc] || attachedNew[newSucc] {
			continue
		}
		// Same component pair on both sides and prefix-like gaps: attach both sides' comment runs,
		// trimmed to blocks whose counterpart attaches too, so no diverged block renders twice.
		if oldSuccPair.Old != nil && newSuccPair.New != nil {
			oldRun := commentRunBefore(oldAll, oldIndex, cp.Old)
			newRun := commentRunBefore(newAll, newIndex, cp.New)
			oldRun = trimRunToCounterparts(oldRun, newRun, counterpartOfOld)
			newRun = trimRunToCounterparts(newRun, oldRun, counterpartOfNew)
			prependComments(oldSuccPair.Old, oldRun, attachedOld, oldSucc)
			prependComments(newSuccPair.New, newRun, attachedNew, newSucc)
			suppressRunPairs(oldRun, newRun)
		}
	}

	if len(commentPairsToSuppress) == 0 {
		return
	}
	// Filter out the consumed comment pairs.
	filtered := result.Pairs[:0]
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if commentPairsToSuppress[p] {
			continue
		}
		filtered = append(filtered, *p)
	}
	result.Pairs = filtered
}
