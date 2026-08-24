// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package diff implements the inner diff (Phase 6): LCS line diff with optional word-diff and ignore-all-space.
package diff

import (
	"strings"

	"cmdiff/pkg/ir"
)

// DefaultWordDiffSpanThreshold: collapse a line's word diff to one span when more than this
// fraction of its words changed. Span policy: [../../doc/word-diff.md](word-diff.md).
const DefaultWordDiffSpanThreshold = 0.4

// Options controls diff behaviour.
type Options struct {
	WordDiff    bool // split into words and diff at word granularity
	IgnoreSpace bool // ignore all whitespace when comparing lines/words
	FullContext bool // include unchanged lines outside change hunks
	// Fraction of changed words above which the word diff renders as one span.
	// 0 uses DefaultWordDiffSpanThreshold; a negative value disables the collapse.
	WordDiffSpanThreshold float64
}

// isWordBoundary reports whether a character separates words for word-level
// diffing: whitespace and common punctuation.
func isWordBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '(', ')', '{', '}', '[', ']', ';', ',', '=', ':':
		return true
	}
	return false
}

// splitWords: whitespace/punctuation stay separate tokens so a line reconstructs exactly.
func splitWords(line string) []string {
	if line == "" {
		return []string{}
	}
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range line {
		if isWordBoundary(r) {
			flush()
			tokens = append(tokens, string(r))
		} else {
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// normalizeForCompare optionally strips all whitespace for ignore-space mode.
func normalizeForCompare(s string, ignoreSpace bool) string {
	if !ignoreSpace {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}

// normalizeLines normalizes lines for ignore-space comparison.
func normalizeLines(lines []string, ignoreSpace bool) []string {
	result := make([]string, len(lines))
	for i, l := range lines {
		result[i] = normalizeForCompare(l, ignoreSpace)
	}
	return result
}

// Differ produces diffs between matched blocks.
type Differ struct {
	opts Options
}

// New creates a new Differ.
func New() *Differ { return &Differ{} }

// NewWithOpts creates a new Differ with the given options.
func NewWithOpts(opts Options) *Differ {
	return &Differ{opts: opts}
}

// Diff computes a diff between two source strings and returns
// a DiffResult with hunks.
func (d *Differ) Diff(oldSource, newSource string) *ir.DiffResult {
	oldLines := splitLines(oldSource)
	newLines := splitLines(newSource)

	hunks := computeHunksWithContext(oldLines, newLines, d.opts.FullContext, d.opts.IgnoreSpace)

	// Word diff runs on the original lines (even under ignore-all-space) so highlights match the file.
	if d.opts.WordDiff {
		threshold := d.opts.WordDiffSpanThreshold
		if threshold == 0 {
			threshold = DefaultWordDiffSpanThreshold
		}
		applyWordDiff(hunks, oldLines, newLines, d.opts.IgnoreSpace, threshold)
	}

	return &ir.DiffResult{Hunks: hunks}
}

// DiffFull: same diff with full context, for whole-file reports (coverage contract).
func (d *Differ) DiffFull(oldSource, newSource string) *ir.DiffResult {
	full := *d
	full.opts.FullContext = true
	return full.Diff(oldSource, newSource)
}

// applyWordDiff: word diffs on normalization-hidden context lines and on the added line of replacement pairs.
// ignoreSpace controls whether whitespace-only word differences are suppressed.
func applyWordDiff(hunks []ir.DiffHunk, oldLines, newLines []string, ignoreSpace bool, spanThreshold float64) {
	for hi := range hunks {
		for li := range hunks[hi].Lines {
			line := &hunks[hi].Lines[li]
			if line.Type != ir.DiffLineContext {
				continue
			}
			var oldLine, newLine string
			if line.OldNo > 0 && line.OldNo <= len(oldLines) {
				oldLine = oldLines[line.OldNo-1]
			}
			if line.NewNo > 0 && line.NewNo <= len(newLines) {
				newLine = newLines[line.NewNo-1]
			}
			if oldLine == newLine {
				continue
			}
			words := computeWordDiff(oldLine, newLine, ignoreSpace, spanThreshold)
			if hasWordDiffChanges(words) {
				line.Words = words
				line.IsWordDiff = true
			}
		}
	}

	// Pair adjacent removed→added runs (replacement blocks); the added line carries the word diff.
	// The removed line is suppressed (WordDiffPaired) so only the combined word-diff line is rendered.
	for hi := range hunks {
		lines := hunks[hi].Lines
		i := 0
		for i < len(lines) {
			if lines[i].Type != ir.DiffLineRemoved {
				i++
				continue
			}
			removedStart := i
			for i < len(lines) && lines[i].Type == ir.DiffLineRemoved {
				i++
			}
			removedRun := lines[removedStart:i]
			if i >= len(lines) || lines[i].Type != ir.DiffLineAdded {
				continue
			}
			addedStart := i
			for i < len(lines) && lines[i].Type == ir.DiffLineAdded {
				i++
			}
			addedRun := lines[addedStart:i]
			k := min(len(removedRun), len(addedRun))
			for idx := 0; idx < k; idx++ {
				words := computeWordDiff(removedRun[idx].Content, addedRun[idx].Content, ignoreSpace, spanThreshold)
				if hasWordDiffChanges(words) {
					addedRun[idx].Words = words
					addedRun[idx].IsWordDiff = true
					removedRun[idx].WordDiffPaired = true
				}
			}
		}
	}
}

// isSpaceWord reports whether a word token is purely whitespace (space, tab, etc.).
func isSpaceWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// hasWordDiffChanges reports whether a word diff contains any added or removed words.
func hasWordDiffChanges(words []ir.DiffWord) bool {
	for _, w := range words {
		if w.Type == ir.DiffWordAdded || w.Type == ir.DiffWordRemoved {
			return true
		}
	}
	return false
}

// computeWordDiff computes the word-level diff of two lines; ignoreSpace suppresses
// whitespace-only word differences. Span consolidation policy: [../../doc/word-diff.md](word-diff.md).
func computeWordDiff(oldLine, newLine string, ignoreSpace bool, spanThreshold float64) []ir.DiffWord {
	oldWords := splitWords(oldLine)
	newWords := splitWords(newLine)

	// For LCS comparison, normalize whitespace tokens when ignoring space:
	// all whitespace tokens become a single canonical space so they match.
	normalize := func(words []string) []string {
		if !ignoreSpace {
			return words
		}
		norm := make([]string, len(words))
		for i, w := range words {
			if isSpaceWord(w) {
				norm[i] = " "
			} else {
				norm[i] = w
			}
		}
		return norm
	}
	oldNorm := normalize(oldWords)
	newNorm := normalize(newWords)

	// Single-pass LCS walk emitting removed-before-added at each change site,
	// instead of merging two separate diffs out of order.
	lcs := computeLCS(oldNorm, newNorm)
	i, j := len(oldWords), len(newWords)
	// Build reversed result then reverse at end.
	var rev []ir.DiffWord
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldNorm[i-1] == newNorm[j-1] {
			rev = append(rev, ir.DiffWord{Text: oldWords[i-1], Type: ir.DiffWordContext, Side: ir.DiffWordSideBoth})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			// Insertion in new (added)
			if ignoreSpace && isSpaceWord(newWords[j-1]) {
				// New-side-only whitespace: context, but must not leak into
				// the removed span during consolidation.
				rev = append(rev, ir.DiffWord{Text: newWords[j-1], Type: ir.DiffWordContext, Side: ir.DiffWordSideNew})
			} else {
				rev = append(rev, ir.DiffWord{Text: newWords[j-1], Type: ir.DiffWordAdded})
			}
			j--
		} else if i > 0 {
			if ignoreSpace && isSpaceWord(oldWords[i-1]) {
				rev = append(rev, ir.DiffWord{Text: oldWords[i-1], Type: ir.DiffWordContext, Side: ir.DiffWordSideOld})
			} else {
				rev = append(rev, ir.DiffWord{Text: oldWords[i-1], Type: ir.DiffWordRemoved})
			}
			i--
		}
	}
	// Reverse to get correct order.
	for l, r := 0, len(rev)-1; l < r; l, r = l+1, r-1 {
		rev[l], rev[r] = rev[r], rev[l]
	}
	// Words are compared the same way the LCS compared them: under
	// ignoreSpace all whitespace tokens are one canonical word.
	equal := func(a, b string) bool {
		if ignoreSpace && isSpaceWord(a) && isSpaceWord(b) {
			return true
		}
		return a == b
	}
	return coalesceWordSpans(rev, spanThreshold, ignoreSpace, equal)
}

// isBothContextWord reports whether a word is context present in both lines; one-sided words
// (whitespace under ignore-all-space) stay inside changed spans instead of splitting them.
func isBothContextWord(w ir.DiffWord) bool {
	return w.Type == ir.DiffWordContext && w.Side == ir.DiffWordSideBoth
}

// isRegionBreak: a word that separates changed regions. Common whitespace never breaks one:
// short changed tokens like "+" would otherwise render as isolated unreadable marks.
func isRegionBreak(w ir.DiffWord, ignoreSpace bool) bool {
	return isBothContextWord(w) && !(ignoreSpace && isSpaceWord(w.Text))
}

// coalesceWordSpans collapses per-word changes into consecutive removed+added spans with
// context trimmed; past spanThreshold all runs merge into one span (negative disables).
func coalesceWordSpans(words []ir.DiffWord, spanThreshold float64, ignoreSpace bool, equal func(a, b string) bool) []ir.DiffWord {
	changed, realChanged := 0, 0
	for _, w := range words {
		if !isBothContextWord(w) {
			changed++
			if w.Type != ir.DiffWordContext {
				realChanged++
			}
		}
	}
	if realChanged == 0 {
		return words // whitespace-only differences stay plain context
	}
	// A mostly-rewritten line reads better as one consecutive span than as many small
	// fragments; collapseWordSpan keeps leading and trailing context.
	if spanThreshold >= 0 && float64(changed)/float64(len(words)) > spanThreshold {
		return collapseWordSpan(words, 0, len(words), ignoreSpace, equal)
	}
	var out []ir.DiffWord
	for i := 0; i < len(words); {
		if isRegionBreak(words[i], ignoreSpace) {
			out = append(out, words[i])
			i++
			continue
		}
		j := i
		for j < len(words) && !isRegionBreak(words[j], ignoreSpace) {
			j++
		}
		out = append(out, collapseWordSpan(words, i, j, ignoreSpace, equal)...)
		i = j
	}
	return out
}

// collapseWordSpan turns [start,end) into trimmed context prefix/suffix around at most one
// removed and one added span; edge whitespace re-emits as one plain space per side that had it.
func collapseWordSpan(words []ir.DiffWord, start, end int, ignoreSpace bool, equal func(a, b string) bool) []ir.DiffWord {
	var removed, added []ir.DiffWord
	for _, w := range words[start:end] {
		switch w.Type {
		case ir.DiffWordRemoved:
			removed = append(removed, w)
		case ir.DiffWordAdded:
			added = append(added, w)
		default:
			// One-sided context words (whitespace under ignore-all-space)
			// must not leak into the other side's span.
			switch w.Side {
			case ir.DiffWordSideNew:
				added = append(added, w)
			case ir.DiffWordSideOld:
				removed = append(removed, w)
			default:
				removed = append(removed, w)
				added = append(added, w)
			}
		}
	}
	// Trim common prefix, then common suffix (never overlapping the prefix).
	prefix := 0
	for prefix < len(removed) && prefix < len(added) && equal(removed[prefix].Text, added[prefix].Text) {
		prefix++
	}
	suffix := 0
	for suffix < len(removed)-prefix && suffix < len(added)-prefix &&
		equal(removed[len(removed)-1-suffix].Text, added[len(added)-1-suffix].Text) {
		suffix++
	}
	removedMid := removed[prefix : len(removed)-suffix]
	addedMid := added[prefix : len(added)-suffix]
	var leadOld, leadNew, midOld, midNew, trailOld, trailNew bool
	if ignoreSpace {
		removedMid, addedMid, leadOld, leadNew, midOld, midNew, trailOld, trailNew = stripSpanEdgeWhitespace(removedMid, addedMid)
	}
	out := make([]ir.DiffWord, 0, prefix+suffix+4)
	appendJunction := func(both, old, new bool) {
		switch {
		case both:
			out = append(out, ir.DiffWord{Text: " ", Type: ir.DiffWordContext, Side: ir.DiffWordSideBoth})
		case old:
			out = append(out, ir.DiffWord{Text: " ", Type: ir.DiffWordContext, Side: ir.DiffWordSideOld})
		case new:
			out = append(out, ir.DiffWord{Text: " ", Type: ir.DiffWordContext, Side: ir.DiffWordSideNew})
		}
	}
	for _, w := range removed[:prefix] {
		out = append(out, ir.DiffWord{Text: w.Text, Type: ir.DiffWordContext, Side: ir.DiffWordSideBoth})
	}
	appendJunction(leadOld && leadNew, leadOld, leadNew)
	if len(removedMid) > 0 {
		out = append(out, ir.DiffWord{Text: joinWords(removedMid), Type: ir.DiffWordRemoved})
	}
	appendJunction(midOld && midNew, midOld, midNew)
	if len(addedMid) > 0 {
		out = append(out, ir.DiffWord{Text: joinWords(addedMid), Type: ir.DiffWordAdded})
	}
	appendJunction(trailOld && trailNew, trailOld, trailNew)
	for _, w := range removed[len(removed)-suffix:] {
		out = append(out, ir.DiffWord{Text: w.Text, Type: ir.DiffWordContext, Side: ir.DiffWordSideBoth})
	}
	return out
}

// stripSpanEdgeWhitespace trims context whitespace from the span edges and re-emits one plain
// space per junction ("~ ~" fragments are noise under ignore-all-space). Each junction space is
// one-sided: only the side(s) that actually had whitespace there get it back, so the other side
// keeps its exact text.
func stripSpanEdgeWhitespace(removed, added []ir.DiffWord) (rm, am []ir.DiffWord, leadOld, leadNew, midOld, midNew, trailOld, trailNew bool) {
	stripLead := func(list []ir.DiffWord) ([]ir.DiffWord, bool) {
		n := 0
		for n < len(list) && list[n].Type == ir.DiffWordContext && isSpaceWord(list[n].Text) {
			n++
		}
		return list[n:], n > 0
	}
	stripTrail := func(list []ir.DiffWord) ([]ir.DiffWord, bool) {
		n := len(list)
		for n > 0 && list[n-1].Type == ir.DiffWordContext && isSpaceWord(list[n-1].Text) {
			n--
		}
		return list[:n], n < len(list)
	}
	var rLead, rTrail, aLead, aTrail bool
	removed, rLead = stripLead(removed)
	removed, rTrail = stripTrail(removed)
	added, aLead = stripLead(added)
	added, aTrail = stripTrail(added)
	// Lead junction: old-side space when the removed span began with whitespace; new-side on a
	// pure insertion (no removed words) whose added span began with whitespace.
	leadOld = rLead
	leadNew = len(removed) == 0 && aLead
	// Mid junction between the removed and added spans: each side re-emits its own edge whitespace.
	midOld = rTrail && len(removed) > 0 && len(added) > 0
	midNew = aLead && len(removed) > 0 && len(added) > 0
	// Trail junction: new-side space when the added span ended with whitespace; old-side on a
	// pure deletion (no added words) whose removed span ended with whitespace.
	trailOld = len(added) == 0 && rTrail
	trailNew = aTrail
	return removed, added, leadOld, leadNew, midOld, midNew, trailOld, trailNew
}

// joinWords concatenates word texts into a single span.
func joinWords(words []ir.DiffWord) string {
	var b strings.Builder
	for _, w := range words {
		b.WriteString(w.Text)
	}
	return b.String()
}

// splitLines splits source text into individual lines.
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// computeHunksWithContext: LCS hunks; emitted content is always the ORIGINAL line text (both raw forms under ignore-space).
func computeHunksWithContext(oldLines, newLines []string, fullContext, ignoreSpace bool) []ir.DiffHunk {
	cmpOld := normalizeLines(oldLines, ignoreSpace)
	cmpNew := normalizeLines(newLines, ignoreSpace)
	if equalLines(cmpOld, cmpNew) {
		// Whitespace-only differences emit both raw forms as context lines; HasChanges stays false.
		if ignoreSpace && !equalLines(oldLines, newLines) {
			return whitespaceOnlyHunk(oldLines, newLines)
		}
		return nil
	}
	// Full LCS always computed: no coarse fallback for large inputs.
	lcs := computeLCS(cmpOld, cmpNew)
	oldDiff, newDiff := backtrack(lcs, cmpOld, cmpNew, oldLines, newLines)
	return groupHunksWithContext(oldDiff, newDiff, fullContext)
}

// whitespaceOnlyHunk: all-context hunk carrying both raw forms (NewBlank marks a line that became blank).
func whitespaceOnlyHunk(oldLines, newLines []string) []ir.DiffHunk {
	lines := make([]ir.DiffLine, 0, len(oldLines))
	for i := range oldLines {
		line := ir.DiffLine{
			Content: oldLines[i],
			Type:    ir.DiffLineContext,
			OldNo:   i + 1,
			NewNo:   i + 1,
		}
		if newLines[i] != oldLines[i] {
			line.NewContent = newLines[i]
			if newLines[i] == "" {
				line.NewBlank = true
			}
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil
	}
	return []ir.DiffHunk{{
		OldStart: 1,
		OldLines: len(lines),
		NewStart: 1,
		NewLines: len(lines),
		Lines:    lines,
	}}
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// computeLCS computes the LCS DP table.
func computeLCS(a, b []string) [][]int {
	m := len(a)
	n := len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}
	return dp
}

// backtrack: LCS walk emitting original text (dispA/dispB), appended in reverse.
func backtrack(lcs [][]int, a, b, dispA, dispB []string) ([]diffOp, []diffOp) {
	var oldDiff, newDiff []diffOp
	i := len(a)
	j := len(b)
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			oldDiff = append(oldDiff, diffOp{Type: opContext, Content: dispA[i-1], OldNo: i, NewNo: j})
			newDiff = append(newDiff, diffOp{Type: opContext, Content: dispB[j-1], OldNo: i, NewNo: j})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			newDiff = append(newDiff, diffOp{Type: opAdd, Content: dispB[j-1], OldNo: 0, NewNo: j})
			j--
		} else {
			oldDiff = append(oldDiff, diffOp{Type: opDel, Content: dispA[i-1], OldNo: i, NewNo: 0})
			i--
		}
	}
	reverseOps(oldDiff)
	reverseOps(newDiff)
	return oldDiff, newDiff
}

// reverseOps reverses a diff operation slice in place.
func reverseOps(ops []diffOp) {
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
}

type diffOpType int

const (
	opContext diffOpType = iota
	opAdd
	opDel
)

type diffOp struct {
	Type    diffOpType
	Content string
	OldNo   int
	NewNo   int
}

// groupHunksWithContext: ≤3 context lines per change (or one full-span hunk with fullContext).
func groupHunksWithContext(oldDiff, newDiff []diffOp, fullContext bool) []ir.DiffHunk {
	type entry struct {
		op         diffOpType
		content    string
		oldNo      int
		newNo      int
		newContent string // raw new text when it differs from content (ignore-space)
		newBlank   bool   // newContent is empty because the new raw line is blank
	}

	var merged []entry
	oi, ni := 0, 0
	for oi < len(oldDiff) || ni < len(newDiff) {
		switch {
		case oi < len(oldDiff) && ni < len(newDiff) &&
			oldDiff[oi].Type == opContext && newDiff[ni].Type == opContext:
			e := entry{op: opContext, content: oldDiff[oi].Content, oldNo: oldDiff[oi].OldNo, newNo: newDiff[ni].NewNo}
			if oldDiff[oi].Content != newDiff[ni].Content {
				e.newContent = newDiff[ni].Content
				if e.newContent == "" {
					e.newBlank = true
				}
			}
			merged = append(merged, e)
			oi++
			ni++
		case oi < len(oldDiff) && oldDiff[oi].Type == opDel:
			merged = append(merged, entry{op: opDel, content: oldDiff[oi].Content, oldNo: oldDiff[oi].OldNo})
			oi++
		case ni < len(newDiff) && newDiff[ni].Type == opAdd:
			merged = append(merged, entry{op: opAdd, content: newDiff[ni].Content, newNo: newDiff[ni].NewNo})
			ni++
		default:
			// A malformed operation stream must not loop forever: preserve remaining ops as context.
			if oi < len(oldDiff) {
				merged = append(merged, entry{op: opContext, content: oldDiff[oi].Content, oldNo: oldDiff[oi].OldNo})
				oi++
			}
			if ni < len(newDiff) {
				merged = append(merged, entry{op: opContext, content: newDiff[ni].Content, newNo: newDiff[ni].NewNo})
				ni++
			}
		}
	}

	const contextSize = 3
	type span struct{ start, end int }
	var spans []span
	if fullContext {
		if len(merged) > 0 {
			spans = append(spans, span{start: 0, end: len(merged)})
		}
	} else {
		for i, entry := range merged {
			if entry.op == opContext {
				continue
			}
			start := max(0, i-contextSize)
			end := i + contextSize + 1
			if end > len(merged) {
				end = len(merged)
			}
			if len(spans) > 0 && start <= spans[len(spans)-1].end {
				if end > spans[len(spans)-1].end {
					spans[len(spans)-1].end = end
				}
				continue
			}
			spans = append(spans, span{start: start, end: end})
		}
	}

	hunks := make([]ir.DiffHunk, 0, len(spans))
	for _, s := range spans {
		hunk := ir.DiffHunk{}
		for _, entry := range merged[s.start:s.end] {
			lineType := ir.DiffLineContext
			switch entry.op {
			case opAdd:
				lineType = ir.DiffLineAdded
			case opDel:
				lineType = ir.DiffLineRemoved
			}
			hunk.Lines = append(hunk.Lines, ir.DiffLine{
				Content:    entry.content,
				Type:       lineType,
				OldNo:      entry.oldNo,
				NewNo:      entry.newNo,
				NewContent: entry.newContent,
				NewBlank:   entry.newBlank,
			})
			switch lineType {
			case ir.DiffLineRemoved:
				hunk.OldLines++
			case ir.DiffLineAdded:
				hunk.NewLines++
			default:
				hunk.OldLines++
				hunk.NewLines++
			}
		}
		for _, line := range hunk.Lines {
			if line.OldNo > 0 {
				hunk.OldStart = line.OldNo
				break
			}
		}
		for _, line := range hunk.Lines {
			if line.NewNo > 0 {
				hunk.NewStart = line.NewNo
				break
			}
		}
		hunks = append(hunks, hunk)
	}
	return hunks
}
