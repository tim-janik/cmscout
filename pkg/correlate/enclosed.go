// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package correlate

import (
	"cmscout/pkg/ir"
)

// isFunctionLike reports whether a block kind renders enclosed comments itself, so a separate
// pair would duplicate. Classes/namespaces are excluded: their inner comments prefix methods.
func isFunctionLike(k ir.BlockKind) bool {
	switch k {
	case ir.KindFunction, ir.KindMethod, ir.KindLambda, ir.KindArrowFunc,
		ir.KindObjectMethod, ir.KindLifecycle, ir.KindMacroFunction:
		return true
	default:
		return false
	}
}

// SuppressEnclosedComments drops comment pairs that already render inside their function-like
// container; matched pairs must share it on both sides. Contract: [doc/enclosed-comments.md](../../doc/enclosed-comments.md).
func SuppressEnclosedComments(result *ir.CorrelationResult) {
	if result == nil || len(result.Pairs) == 0 {
		return
	}

	// Indexes for side lookups.
	oldByID := sideBlockIndex(result, true)
	newByID := sideBlockIndex(result, false)

	// Map from block ID to pair for container lookups.
	oldBlockToPair := make(map[string]*ir.CorrelatedPair)
	newBlockToPair := make(map[string]*ir.CorrelatedPair)
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Supplemental {
			continue
		}
		if p.Old != nil {
			oldBlockToPair[p.Old.ID] = p
		}
		if p.New != nil {
			newBlockToPair[p.New.ID] = p
		}
	}

	// Collect function-like containers per side for enclosure checks.
	var oldFuncs, newFuncs []*ir.SemanticBlock
	for _, b := range oldByID {
		if isFunctionLike(b.Kind) {
			oldFuncs = append(oldFuncs, b)
		}
	}
	for _, b := range newByID {
		if isFunctionLike(b.Kind) {
			newFuncs = append(newFuncs, b)
		}
	}

	// deepestFunctionContainer: smallest function-like block strictly containing
	// c, or nil if c is not inside any (a prefix before a function is not inside).
	deepest := func(c *ir.SemanticBlock, funcs []*ir.SemanticBlock) *ir.SemanticBlock {
		if c == nil {
			return nil
		}
		var best *ir.SemanticBlock
		bestSize := ^uint(0)
		for _, f := range funcs {
			if f.Span.StartByte >= f.Span.EndByte {
				continue
			}
			// Strict enclosure; a prefix comment has EndByte < function StartByte.
			if c.Span.StartByte < f.Span.StartByte || c.Span.EndByte > f.Span.EndByte {
				continue
			}
			// The comment must not be the function's own span.
			if c.Span.StartByte == f.Span.StartByte && c.Span.EndByte == f.Span.EndByte {
				continue
			}
			if size := f.Span.EndByte - f.Span.StartByte; size < bestSize {
				best = f
				bestSize = size
			}
		}
		return best
	}

	toSuppress := make(map[*ir.CorrelatedPair]bool)

	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Supplemental {
			continue
		}
		// Only comment pairs (a comment matched to code is not a comment pair).
		isOldComment := p.Old != nil && p.Old.Kind == ir.KindComment
		isNewComment := p.New != nil && p.New.Kind == ir.KindComment
		if !isOldComment && !isNewComment {
			continue
		}

		if p.IsAdded() {
			// New comment inside a new function-like container that is part of result.
			if !isNewComment {
				continue
			}
			container := deepest(p.New, newFuncs)
			if container == nil {
				continue
			}
			if cp, ok := newBlockToPair[container.ID]; ok && cp != nil && !cp.Supplemental {
				// The container is present in the correlation (added/matched).
				// If the container is added or matched, its diff already shows the comment.
				toSuppress[p] = true
			}
			continue
		}
		if p.IsRemoved() {
			if !isOldComment {
				continue
			}
			container := deepest(p.Old, oldFuncs)
			if container == nil {
				continue
			}
			if cp, ok := oldBlockToPair[container.ID]; ok && cp != nil && !cp.Supplemental {
				toSuppress[p] = true
			}
			continue
		}
		// Matched comment: both sides comment.
		if isOldComment && isNewComment {
			oldContainer := deepest(p.Old, oldFuncs)
			newContainer := deepest(p.New, newFuncs)
			if oldContainer == nil || newContainer == nil {
				// One side not inside a function, e.g. moved between function and top-level
				continue
			}
			oldPair, ok1 := oldBlockToPair[oldContainer.ID]
			newPair, ok2 := newBlockToPair[newContainer.ID]
			if !ok1 || !ok2 || oldPair == nil || newPair == nil {
				continue
			}
			// Must be the same logical container pair (the function that contains the comment on both sides).
			if oldPair != newPair {
				// Same pair object means same correlation entry; different pairs mean
				// the comment moved between containers and must stay visible.
				continue
			}
			// Both inside the same function pair -> suppress duplicate.
			toSuppress[p] = true
		}
	}

	if len(toSuppress) == 0 {
		return
	}
	filtered := result.Pairs[:0]
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if toSuppress[p] {
			continue
		}
		filtered = append(filtered, *p)
	}
	result.Pairs = filtered
}
