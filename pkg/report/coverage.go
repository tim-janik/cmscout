// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package report

import (
	"fmt"
	"strings"

	"cmscout/pkg/ir"
)

type contentKey struct {
	side    string
	content string
}

const (
	oldSide = "old"
	newSide = "new"
)

// beginPair prepares the reporter for rendering the given pair.
func (r *TextReporter) beginPair(p *ir.CorrelatedPair) {
	r.pairSeq++
	r.pairID = fmt.Sprintf("p%d", r.pairSeq)
	r.suppress = p.Supplemental
	if r.suppress {
		return
	}
	// Record covered file lines; skip-unchanged blocks go to a separate span map the supplement hides too.
	if r.coveredOld == nil {
		r.coveredOld = make(map[int]bool)
	}
	if r.coveredNew == nil {
		r.coveredNew = make(map[int]bool)
	}
	if r.Opts.SkipUnchanged && r.isCompletelyUnchanged(p, r.movedPairs[p]) {
		if p.Old != nil && p.Old.Span.StartByte < p.Old.Span.EndByte {
			r.skippedOldSpans = append(r.skippedOldSpans, p.Old.Span)
		}
		if p.New != nil && p.New.Span.StartByte < p.New.Span.EndByte {
			r.skippedNewSpans = append(r.skippedNewSpans, p.New.Span)
		}
		return
	}
	if p.Old != nil {
		coverSpan(p.Old.Span, r.coveredOld)
		if p.Old.Kind == ir.KindComment {
			if r.coveredCommentOld == nil {
				r.coveredCommentOld = make(map[int]bool)
			}
			coverSpan(p.Old.Span, r.coveredCommentOld)
		}
	}
	if p.New != nil {
		coverSpan(p.New.Span, r.coveredNew)
		if p.New.Kind == ir.KindComment {
			if r.coveredCommentNew == nil {
				r.coveredCommentNew = make(map[int]bool)
			}
			coverSpan(p.New.Span, r.coveredCommentNew)
		}
	}
}

// coverSpan marks the 1-based lines of a 0-based source span as covered.
func coverSpan(span ir.SourceSpan, m map[int]bool) {
	for l := int(span.StartLine) + 1; l <= int(span.EndLine)+1; l++ {
		m[l] = true
	}
}

// recordContent: first renderer wins; trimmed content also recorded for indentation-aware de-dup.
func (r *TextReporter) recordContent(side, content string) {
	if r.seen == nil {
		r.seen = make(map[contentKey]string)
		r.seenTrim = make(map[contentKey]string)
	}
	key := contentKey{side: side, content: content}
	if _, ok := r.seen[key]; !ok {
		r.seen[key] = r.pairID
	}
	if trimmed := strings.TrimSpace(content); trimmed != "" {
		tkey := contentKey{side: side, content: trimmed}
		if _, ok := r.seenTrim[tkey]; !ok {
			r.seenTrim[tkey] = r.pairID
		}
	}
}

// seenByOtherPair: rendered by a different pair? (repeats within the pair itself are fine).
func (r *TextReporter) seenByOtherPair(side, content string) bool {
	id, ok := r.seen[contentKey{side: side, content: content}]
	return ok && id != r.pairID
}

// seenTrimmedByOtherPair: trimmed content already rendered by another pair (comment indentation rule).
func (r *TextReporter) seenTrimmedByOtherPair(side, trimmed string) bool {
	id, ok := r.seenTrim[contentKey{side: side, content: trimmed}]
	return ok && id != r.pairID
}

// coveredLine reports whether a 1-based file line lies inside a rendered
// block's span on the given side.
func (r *TextReporter) coveredLine(m map[int]bool, line int) bool {
	return m != nil && m[line]
}

// coveredCommentLine: inside a rendered COMMENT block's span (see shouldSuppressSide).
func (r *TextReporter) coveredCommentLine(side string, line int) bool {
	m := r.coveredCommentOld
	if side == newSide {
		m = r.coveredCommentNew
	}
	return m != nil && m[line]
}

// shouldSuppressSide: the single coverage de-duplication rule, shared by both rendering paths.
func (r *TextReporter) shouldSuppressSide(side, content string, lineNo int, hideUnchanged bool) bool {
	if !r.suppress || lineNo <= 0 {
		return false
	}
	if hideUnchanged && r.Opts.SkipUnchanged {
		return true
	}
	if r.lineFullySkipped(side, lineNo) {
		return true
	}
	covered := r.coveredOld
	if side == newSide {
		covered = r.coveredNew
	}
	if r.seenByOtherPair(side, content) && r.coveredLine(covered, lineNo) {
		return true
	}
	// Indentation-only matches require a rendered COMMENT block owning the line (duplicates never hidden).
	if trimmed := strings.TrimSpace(content); trimmed != "" &&
		r.seenTrimmedByOtherPair(side, trimmed) && r.coveredCommentLine(side, lineNo) {
		return true
	}
	return false
}

// lineFullySkipped: entire non-whitespace line content inside a skipped block's span (shared lines are never suppressed).
func (r *TextReporter) lineFullySkipped(side string, lineNo int) bool {
	var spans []ir.SourceSpan
	var src string
	if side == newSide {
		spans = r.skippedNewSpans
		src = r.newSrc
	} else {
		spans = r.skippedOldSpans
		src = r.oldSrc
	}
	if len(spans) == 0 || src == "" || lineNo <= 0 {
		return false
	}
	start, end := r.lineContentRange(side, lineNo)
	if start < 0 || end <= start {
		return false
	}
	for _, s := range spans {
		if uint(start) >= s.StartByte && uint(end) <= s.EndByte {
			return true
		}
	}
	return false
}

// lineContentRange: non-whitespace byte range of a 1-based line, or (-1,-1) if missing or blank.
func (r *TextReporter) lineContentRange(side string, lineNo int) (int, int) {
	var ranges [][2]int
	var src string
	if side == newSide {
		if r.newLineRanges == nil && r.newSrc != "" {
			r.newLineRanges = computeLineRanges(r.newSrc)
		}
		ranges = r.newLineRanges
		src = r.newSrc
	} else {
		if r.oldLineRanges == nil && r.oldSrc != "" {
			r.oldLineRanges = computeLineRanges(r.oldSrc)
		}
		ranges = r.oldLineRanges
		src = r.oldSrc
	}
	if lineNo < 1 || lineNo > len(ranges) {
		return -1, -1
	}
	lo, hi := ranges[lineNo-1][0], ranges[lineNo-1][1]
	for lo < hi && isSpaceByte(src[lo]) {
		lo++
	}
	for hi > lo && isSpaceByte(src[hi-1]) {
		hi--
	}
	return lo, hi
}

// computeLineRanges returns the byte ranges of all lines (1-based index in
// the result) of a source text.
func computeLineRanges(src string) [][2]int {
	var out [][2]int
	start := 0
	for i := 0; i <= len(src); i++ {
		if i == len(src) || src[i] == '\n' {
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	}
	return out
}

// isSpaceByte reports whether b is an ASCII whitespace byte.
func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
