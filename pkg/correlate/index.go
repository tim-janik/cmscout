// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package correlate

import (
	"fmt"
	"sort"
	"strings"

	"cmscout/pkg/ir"
)

// numberAnonymousBlocks: per-side ordinal names (arrow_function.01, ...) in source order,
// so unchanged anonymous blocks at the same position read as [unchanged] rather than [moved].
func numberAnonymousBlocks(result *ir.CorrelationResult) {
	var oldBlocks, newBlocks []*ir.SemanticBlock
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Old != nil {
			oldBlocks = append(oldBlocks, p.Old)
		}
		if p.New != nil {
			newBlocks = append(newBlocks, p.New)
		}
	}
	// Sort by source position so numbering follows reading order.
	sort.Slice(oldBlocks, func(i, j int) bool {
		return oldBlocks[i].Span.StartByte < oldBlocks[j].Span.StartByte
	})
	sort.Slice(newBlocks, func(i, j int) bool {
		return newBlocks[i].Span.StartByte < newBlocks[j].Span.StartByte
	})
	numberSide(oldBlocks)
	numberSide(newBlocks)
}

// numberSide numbers anonymous collapsible blocks on one side using a
// per-kind counter, in the given (pre-sorted) order.
func numberSide(blocks []*ir.SemanticBlock) {
	counters := make(map[ir.BlockKind]int)
	for _, b := range blocks {
		if !isCollapsibleChild(b.Kind) || b.Name != "" {
			continue
		}
		counters[b.Kind]++
		b.Name = fmt.Sprintf("%s.%02d", b.Kind, counters[b.Kind])
	}
}

// displayName: placeholder for unnamed blocks (defensive; anonymous blocks are numbered first).
func displayName(name string) string {
	if name == "" {
		return "<anonymous>"
	}
	return name
}

// sideBlockIndex: one side's blocks by ID (old and new indexes are never merged).
func sideBlockIndex(result *ir.CorrelationResult, old bool) map[string]*ir.SemanticBlock {
	index := make(map[string]*ir.SemanticBlock)
	for i := range result.Pairs {
		block := result.Pairs[i].New
		if old {
			block = result.Pairs[i].Old
		}
		if block != nil {
			index[block.ID] = block
		}
	}
	return index
}

// childIndex builds a parent-ID → children map for one side, with each
// child list sorted by source position.
func childIndex(blockByID map[string]*ir.SemanticBlock) map[string][]*ir.SemanticBlock {
	index := make(map[string][]*ir.SemanticBlock)
	for _, b := range blockByID {
		if b.Parent == "" {
			continue
		}
		index[b.Parent] = append(index[b.Parent], b)
	}
	for _, children := range index {
		sort.Slice(children, func(i, j int) bool {
			if children[i].Span.StartByte != children[j].Span.StartByte {
				return children[i].Span.StartByte < children[j].Span.StartByte
			}
			return children[i].ID < children[j].ID
		})
	}
	return index
}

// commentIndex: comments (deliberately unparented) assigned to their deepest containing block by span.
func commentIndex(blockByID map[string]*ir.SemanticBlock) map[string][]*ir.SemanticBlock {
	index := make(map[string][]*ir.SemanticBlock)
	for _, comment := range blockByID {
		if comment.Kind != ir.KindComment || comment.Span.StartByte >= comment.Span.EndByte {
			continue
		}
		var best *ir.SemanticBlock
		bestSize := ^uint(0)
		for _, candidate := range blockByID {
			if candidate.Kind == ir.KindComment || candidate.Span.StartByte >= candidate.Span.EndByte {
				continue
			}
			if comment.Span.StartByte < candidate.Span.StartByte || comment.Span.StartByte >= candidate.Span.EndByte {
				continue
			}
			if size := candidate.Span.EndByte - candidate.Span.StartByte; size < bestSize ||
				(size == bestSize && (best == nil || candidate.ID < best.ID)) {
				best = candidate
				bestSize = size
			}
		}
		if best != nil {
			index[best.ID] = append(index[best.ID], comment)
		}
	}
	sortContained(index)
	return index
}

// containedBlockIndex: parentless JSX/template elements assigned to their deepest
// container so a matched element renders once instead of twice.
func containedBlockIndex(blockByID map[string]*ir.SemanticBlock) map[string][]*ir.SemanticBlock {
	index := make(map[string][]*ir.SemanticBlock)
	for _, b := range blockByID {
		if b.Parent != "" || (b.Kind != ir.KindJSX && b.Kind != ir.KindTemplate) ||
			b.Span.StartByte >= b.Span.EndByte {
			continue
		}
		if best := deepestContainer(b, blockByID); best != nil {
			index[best.ID] = append(index[best.ID], b)
		}
	}
	sortContained(index)
	return index
}

// deepestContainer returns the semantic block whose span STRICTLY contains
// b's span and is smallest (deepest), or nil for a top-level block.
func deepestContainer(b *ir.SemanticBlock, blockByID map[string]*ir.SemanticBlock) *ir.SemanticBlock {
	var best *ir.SemanticBlock
	bestSize := ^uint(0)
	for _, candidate := range blockByID {
		if candidate == b || candidate.Span.StartByte >= candidate.Span.EndByte {
			continue
		}
		if b.Span.StartByte < candidate.Span.StartByte || b.Span.EndByte > candidate.Span.EndByte {
			continue
		}
		if size := candidate.Span.EndByte - candidate.Span.StartByte; size < bestSize ||
			(size == bestSize && (best == nil || candidate.ID < best.ID)) {
			best = candidate
			bestSize = size
		}
	}
	return best
}

// isOnlyWhitespace reports whether the given string contains only whitespace
// (spaces, tabs, newlines).
func isOnlyWhitespace(s string) bool {
	return strings.TrimSpace(s) == ""
}

// isCollapsibleChild: kinds nested in containers; constants/imports/comments excluded (substring coincidence).
func isCollapsibleChild(kind ir.BlockKind) bool {
	switch kind {
	case ir.KindMethod, ir.KindLifecycle, ir.KindFunction,
		ir.KindArrowFunc, ir.KindObjectMethod, ir.KindJSX, ir.KindTemplate,
		ir.KindLambda:
		return true
	default:
		return false
	}
}

// sortContained sorts each index bucket by source position.
func sortContained(index map[string][]*ir.SemanticBlock) {
	for _, blocks := range index {
		sort.Slice(blocks, func(i, j int) bool {
			if blocks[i].Span.StartByte != blocks[j].Span.StartByte {
				return blocks[i].Span.StartByte < blocks[j].Span.StartByte
			}
			return blocks[i].ID < blocks[j].ID
		})
	}
}
