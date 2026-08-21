// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package correlate implements the correlation engine: pairs semantic blocks via shared matching stages.
package correlate

import (
	"fmt"
	"sort"

	"cmdiff/pkg/ir"
)

// kindName identifies a block by kind and name.
type kindName struct {
	kind ir.BlockKind
	name string
}

// CollapseMatchedSubBlocks rewrites matched children out of parent sources as canonical
// "// [matched: kind name]" references — block-ID driven, collapsible kinds only.
func CollapseMatchedSubBlocks(result *ir.CorrelationResult) {
	absorbedOld, absorbedOldIDs := newAbsorption()
	absorbedNew, absorbedNewIDs := newAbsorption()
	// Keep old and new indexes separate: block IDs are per-document, not global.
	oldByID := sideBlockIndex(result, true)
	newByID := sideBlockIndex(result, false)

	// Indexes built once: children by parent, comments by span, parentless elements by span.
	oldChildren := childIndex(oldByID)
	newChildren := childIndex(newByID)
	oldComments := commentIndex(oldByID)
	newComments := commentIndex(newByID)
	oldContained := containedBlockIndex(oldByID)
	newContained := containedBlockIndex(newByID)

	// Number anonymous collapsible blocks per side in source order, before matched sets.
	numberAnonymousBlocks(result)

	// Collect matched child IDs per side; canonical refs use the NEW side's (kind, name).
	matchedIDsOld := make(map[string]struct{})
	matchedIDsNew := make(map[string]struct{})
	canonicalRefOld := make(map[string]kindName)
	canonicalRefNew := make(map[string]kindName)
	for _, p := range result.Pairs {
		if p.Old == nil || p.New == nil {
			continue
		}
		if isCollapsibleChild(p.Old.Kind) {
			matchedIDsOld[p.Old.ID] = struct{}{}
			canonicalRefOld[p.Old.ID] = kindName{p.New.Kind, p.New.Name}
		}
		if isCollapsibleChild(p.New.Kind) {
			matchedIDsNew[p.New.ID] = struct{}{}
			canonicalRefNew[p.New.ID] = kindName{p.New.Kind, p.New.Name}
		}
	}

	if len(matchedIDsOld) == 0 && len(matchedIDsNew) == 0 {
		return
	}

	// collapseSide rewrites one block's source and records absorbed comments per side.
	collapseSide := func(
		b *ir.SemanticBlock,
		children, comments, contained map[string][]*ir.SemanticBlock,
		matchedIDs map[string]struct{}, canonicalRef map[string]kindName,
		absorbed map[string]int, absorbedIDs map[string]struct{},
	) {
		src, ab := collapseChildren(b, children, comments, contained, matchedIDs, canonicalRef)
		b.Source = src
		for _, a := range ab {
			if a.id != "" {
				absorbedIDs[a.id] = struct{}{}
			} else {
				absorbed[a.text]++
			}
		}
	}
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Old != nil && p.Old.Kind == ir.KindNamespace {
			continue // namespaces are a space, not a component: nothing collapses into them
		}

		// Matched parents are collapsed too: children must not re-print inside the parent blob.
		if p.Old != nil && p.New != nil {
			collapseSide(p.Old, oldChildren, oldComments, oldContained, matchedIDsOld, canonicalRefOld, absorbedOld, absorbedOldIDs)
			collapseSide(p.New, newChildren, newComments, newContained, matchedIDsNew, canonicalRefNew, absorbedNew, absorbedNewIDs)
		} else if p.IsRemoved() {
			collapseSide(p.Old, oldChildren, oldComments, oldContained, matchedIDsOld, canonicalRefOld, absorbedOld, absorbedOldIDs)
		} else if p.IsAdded() {
			collapseSide(p.New, newChildren, newComments, newContained, matchedIDsNew, canonicalRefNew, absorbedNew, absorbedNewIDs)
		}
	}

	// Filter only unchanged comment pairs absorbed on BOTH sides; reworded or one-sided stay (F11).
	var filtered []ir.CorrelatedPair
	for _, p := range result.Pairs {
		oldComment := p.Old != nil && p.Old.Kind == ir.KindComment
		newComment := p.New != nil && p.New.Kind == ir.KindComment
		if !oldComment && !newComment {
			filtered = append(filtered, p)
			continue
		}
		oldAbsorbed := oldComment && sideAbsorbed(p.Old, absorbedOld, absorbedOldIDs)
		newAbsorbed := newComment && sideAbsorbed(p.New, absorbedNew, absorbedNewIDs)
		if oldComment && newComment && oldAbsorbed && newAbsorbed && p.Old.Source == p.New.Source {
			continue // unchanged comment is represented by identical references
		}
		filtered = append(filtered, p)
	}
	result.Pairs = filtered
}

// collapseChildren rewrites one parent's source: matched child spans (block ID only)
// become canonical references; returns the new source and the absorbed comments.
func collapseChildren(
	parent *ir.SemanticBlock,
	childrenByParent, commentsByParent, containedByParent map[string][]*ir.SemanticBlock,
	matchedIDs map[string]struct{}, canonicalRef map[string]kindName,
) (string, []absorption) {
	// Merge span-contained parentless elements (JSX/templates) into children, byte-sorted;
	// replacement runs end→start so earlier offsets stay valid.
	children := childrenByParent[parent.ID]
	if extra := containedByParent[parent.ID]; len(extra) > 0 {
		children = append(append([]*ir.SemanticBlock(nil), children...), extra...)
		sort.Slice(children, func(i, j int) bool {
			if children[i].Span.StartByte != children[j].Span.StartByte {
				return children[i].Span.StartByte < children[j].Span.StartByte
			}
			return children[i].ID < children[j].ID
		})
	}
	var matched []*ir.SemanticBlock
	for _, b := range children {
		if _, ok := matchedIDs[b.ID]; ok {
			matched = append(matched, b)
		}
	}

	if len(matched) == 0 {
		return parent.Source, nil
	}

	// Prefix comments: merge direct-child + span-owned comments, de-dupe by block ID.
	var commentChildren []*ir.SemanticBlock
	seenComments := make(map[string]struct{})
	appendComment := func(comment *ir.SemanticBlock) {
		if comment == nil {
			return
		}
		if comment.ID != "" {
			if _, ok := seenComments[comment.ID]; ok {
				return
			}
			seenComments[comment.ID] = struct{}{}
		}
		commentChildren = append(commentChildren, comment)
	}
	for _, b := range children {
		if b.Kind == ir.KindComment {
			appendComment(b)
		}
	}
	for _, b := range commentsByParent[parent.ID] {
		appendComment(b)
	}
	sort.Slice(commentChildren, func(i, j int) bool {
		return commentChildren[i].Span.StartByte < commentChildren[j].Span.StartByte
	})

	src := parent.Source
	parentStart := parent.Span.StartByte

	// Track comments consumed by span expansion for later pair filtering.
	consumedComments := make(map[string]struct{})

	// Replace last→first so earlier byte offsets remain valid.
	for i := len(matched) - 1; i >= 0; i-- {
		child := matched[i]
		// Absolute → parent-relative byte offsets.
		relStart := child.Span.StartByte - parentStart
		relEnd := child.Span.EndByte - parentStart

		// Use the canonical (new-side) reference so both sides collapse identically.
		kn := kindName{child.Kind, child.Name}
		if canonical, ok := canonicalRef[child.ID]; ok {
			kn = canonical
		}
		ref := fmt.Sprintf("// [matched: %s %s]", kn.kind, displayName(kn.name))

		// Bounds sanity checks (uint underflow is caught by relStart >= relEnd).
		if relStart >= relEnd {
			continue
		}
		// Out-of-class members are rendered by their own method pair.
		if relEnd > uint(len(src)) {
			continue
		}

		// Fold only spans that start at the first content of their line.
		lineStart := relStart
		for lineStart > 0 && src[lineStart-1] != '\n' {
			lineStart--
		}
		if !isOnlyWhitespace(src[lineStart:relStart]) {
			continue
		}

		// Expand only own-line prefix comments, never trailing comments.
		expandedStart := relStart
		for _, cmt := range commentChildren {
			// Reject comments outside the parent before subtracting (unsigned underflow guard).
			if cmt.Span.StartByte < parentStart || cmt.Span.EndByte < parentStart {
				continue
			}
			cmtRelStart := cmt.Span.StartByte - parentStart
			cmtRelEnd := cmt.Span.EndByte - parentStart
			if cmtRelStart > uint(len(src)) || cmtRelEnd > uint(len(src)) {
				continue
			}
			// Leave trailing comments in source.
			cmtLineStart := cmtRelStart
			for cmtLineStart > 0 && src[cmtLineStart-1] != '\n' {
				cmtLineStart--
			}
			if !isOnlyWhitespace(src[cmtLineStart:cmtRelStart]) {
				continue
			}
			// Only own-line comments ending right before the child (whitespace gap) are folded in.
			if cmtRelEnd <= relStart && cmtRelEnd <= expandedStart {
				gapStart := cmtRelEnd
				gapEnd := expandedStart
				if gapEnd > gapStart && isOnlyWhitespace(src[gapStart:gapEnd]) {
					expandedStart = cmtRelStart
					consumedComments[cmt.ID] = struct{}{}
				}
			}
		}

		// Fold a trailing same-line comment so the reference stays standalone.
		relLineEnd := relEnd
		for relLineEnd < uint(len(src)) && src[relLineEnd] != '\n' {
			relLineEnd++
		}
		for _, cmt := range commentChildren {
			if cmt.Span.StartByte < parentStart || cmt.Span.EndByte < parentStart {
				continue
			}
			crs := cmt.Span.StartByte - parentStart
			cre := cmt.Span.EndByte - parentStart
			if crs > uint(len(src)) || cre > uint(len(src)) {
				continue
			}
			if crs >= relEnd && crs < relLineEnd && cre <= relLineEnd {
				relEnd = cre
				consumedComments[cmt.ID] = struct{}{}
				break
			}
		}

		src = src[:expandedStart] + ref + src[relEnd:]
	}

	// Post-process: absorb comment-only lines preceding reference lines (call-wrapper cases).
	src, absorbed := absorbPrefixComments(src)
	// Combine consumed comments: exact block IDs (span expansion) + trimmed texts (post-processing).
	var allAbsorbed []absorption
	for id := range consumedComments {
		allAbsorbed = append(allAbsorbed, absorption{id: id})
	}
	for _, text := range absorbed {
		allAbsorbed = append(allAbsorbed, absorption{text: text})
	}

	return src, allAbsorbed
}
