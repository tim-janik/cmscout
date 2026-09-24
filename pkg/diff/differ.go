// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package diff implements the inner diff (Phase 6): LCS line diff with optional word-diff and ignore-all-space.
package diff

import (
	"strings"

	"cmdiff/pkg/ir"
)

// Options controls diff behaviour.
type Options struct {
	WordDiff    bool // split into words and diff at word granularity
	IgnoreSpace bool // ignore all whitespace when comparing lines/words
	FullContext bool // include unchanged lines outside change hunks
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
		applyWordDiff(hunks, oldLines, newLines)
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
func applyWordDiff(hunks []ir.DiffHunk, oldLines, newLines []string) {
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
			words := computeWordDiff(oldLine, newLine)
			if len(words) > 0 {
				line.Words = words
				line.IsWordDiff = true
			}
		}
	}

	// Pair adjacent removed→added runs (replacement blocks); the added line carries the word diff.
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
				words := computeWordDiff(removedRun[idx].Content, addedRun[idx].Content)
				if len(words) > 0 {
					addedRun[idx].Words = words
					addedRun[idx].IsWordDiff = true
				}
			}
		}
	}
}

// computeWordDiff computes a word-level diff between two lines.
func computeWordDiff(oldLine, newLine string) []ir.DiffWord {
	oldWords := splitWords(oldLine)
	newWords := splitWords(newLine)

	lcs := computeLCS(oldWords, newWords)
	oldDiff, newDiff := backtrackWord(lcs, oldWords, newWords)

	var result []ir.DiffWord
	wi, wj := 0, 0
	for wi < len(oldDiff) || wj < len(newDiff) {
		if wi < len(oldDiff) && wj < len(newDiff) &&
			oldDiff[wi].Type == opContext && newDiff[wj].Type == opContext &&
			oldDiff[wi].Content == newDiff[wj].Content {
			result = append(result, ir.DiffWord{
				Text: oldDiff[wi].Content,
				Type: ir.DiffWordContext,
			})
			wi++
			wj++
		} else if wj < len(newDiff) && newDiff[wj].Type == opAdd {
			result = append(result, ir.DiffWord{
				Text: newDiff[wj].Content,
				Type: ir.DiffWordAdded,
			})
			wj++
		} else if wi < len(oldDiff) && oldDiff[wi].Type == opDel {
			result = append(result, ir.DiffWord{
				Text: oldDiff[wi].Content,
				Type: ir.DiffWordRemoved,
			})
			wi++
		} else {
			// Mismatch — treat as context to avoid infinite loop
			if wi < len(oldDiff) {
				result = append(result, ir.DiffWord{
					Text: oldDiff[wi].Content,
					Type: ir.DiffWordContext,
				})
				wi++
			}
			if wj < len(newDiff) {
				result = append(result, ir.DiffWord{
					Text: newDiff[wj].Content,
					Type: ir.DiffWordContext,
				})
				wj++
			}
		}
	}
	return result
}

// backtrackWord: rebuild ops from the LCS table, appended in reverse (avoids quadratic prepending).
func backtrackWord(lcs [][]int, a, b []string) ([]diffOp, []diffOp) {
	var oldDiff, newDiff []diffOp
	i := len(a)
	j := len(b)
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			oldDiff = append(oldDiff, diffOp{Type: opContext, Content: a[i-1]})
			newDiff = append(newDiff, diffOp{Type: opContext, Content: b[j-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			newDiff = append(newDiff, diffOp{Type: opAdd, Content: b[j-1]})
			j--
		} else {
			oldDiff = append(oldDiff, diffOp{Type: opDel, Content: a[i-1]})
			i--
		}
	}
	reverseOps(oldDiff)
	reverseOps(newDiff)
	return oldDiff, newDiff
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
