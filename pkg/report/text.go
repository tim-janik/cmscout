// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package report

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"cmdiff/pkg/ir"
	"cmdiff/pkg/matching"
)

// WordDiffStyle configures markers and colors for word-level highlights; fields are editable,
// e.g. "{+"/"+}" instead of "+"/"~". Defaults and examples: [../../doc/word-diff.md](word-diff.md).
var WordDiffStyle = struct {
	AddedPrefix   string
	AddedSuffix   string
	RemovedPrefix string
	RemovedSuffix string
	AddedColor    string
	RemovedColor  string
}{
	AddedPrefix:   "+",
	AddedSuffix:   "+",
	RemovedPrefix: "~",
	RemovedSuffix: "~",
	AddedColor:    "green",
	RemovedColor:  "red",
}

// TextReporter renders a text-based review report.
type TextReporter struct {
	Opts Options

	// seen: rendered lines by side+content (old removed ≠ new added obligations).
	seen map[contentKey]string

	// seenTrim: trimmed content for the supplement's indentation-aware comment de-dup.
	seenTrim map[contentKey]string

	// pairID/pairSeq: repeats allowed within a pair, de-duplicated across pairs.
	pairID  string
	pairSeq int

	// suppress: cross-pair de-duplication for the pair currently being rendered (coverage supplement).
	suppress bool

	// coveredOld/coveredNew: rendered block spans; supplement lines suppressed only when shown AND span-owned.
	coveredOld map[int]bool
	coveredNew map[int]bool

	// coveredCommentOld/coveredCommentNew: rendered comment spans — ownership required before indentation-aware suppression.
	coveredCommentOld map[int]bool
	coveredCommentNew map[int]bool

	// skippedOld/NewSpans: byte ranges of skip-unchanged blocks; the supplement hides only fully-owned lines.
	// oldSrc/newSrc map supplement line numbers to byte ranges.
	skippedOldSpans []ir.SourceSpan
	skippedNewSpans []ir.SourceSpan
	oldSrc          string
	newSrc          string
	oldLineRanges   [][2]int
	newLineRanges   [][2]int

	// movedPairs: reordered pairs render and count as "Moved" even with --skip-unchanged.
	movedPairs map[*ir.CorrelatedPair]bool

	// classContainers indexes classes whose added or removed members render inline.
	classContainers map[string]*ir.SemanticBlock
}

// isCompletelyUnchanged: identical source, name, position ⇒ no review signal; whole-file
// pairs ignore the name; moved pairs always render.
func isCompletelyUnchanged(p *ir.CorrelatedPair, moved bool) bool {
	if p.Supplemental || p.Old == nil || p.New == nil {
		return false
	}
	if p.Old.Source != p.New.Source {
		return false
	}
	if p.Old.Kind != ir.KindUnknown && ((p.Old.Name != p.New.Name && !sameAnonymousNumberedName(p)) || moved) {
		return false
	}
	if p.InnerDiff != nil && p.InnerDiff.HasChanges() {
		return false
	}
	return true
}

// diffSimilarity: % of context lines in the inner diff, truncated — any change shows below 100%.
func diffSimilarity(p *ir.CorrelatedPair) float64 {
	if p.InnerDiff == nil {
		return p.Confidence * 100
	}
	total, matched := 0, 0
	for _, h := range p.InnerDiff.Hunks {
		for _, line := range h.Lines {
			total++
			if line.Type == ir.DiffLineContext {
				matched++
			}
		}
	}
	if total == 0 {
		return p.Confidence * 100
	}
	return math.Floor(100 * float64(matched) / float64(total))
}

// Write renders the report to w.
func (r *TextReporter) Write(w io.Writer, result *ir.CorrelationResult, oldPath, newPath string) error {
	if result == nil {
		return fmt.Errorf("report: nil correlation result")
	}
	// Reset per-report state: a Reporter may be reused.
	r.seen = nil
	r.seenTrim = nil
	r.pairID = ""
	r.pairSeq = 0
	r.suppress = false
	r.coveredOld = nil
	r.coveredNew = nil
	r.coveredCommentOld = nil
	r.coveredCommentNew = nil
	r.skippedOldSpans = nil
	r.skippedNewSpans = nil
	r.oldSrc = ""
	r.newSrc = ""
	r.oldLineRanges = nil
	r.newLineRanges = nil
	r.movedPairs = nil
	r.classContainers = nil

	// The supplemental pair carries the raw inputs used for skip-unchanged byte-range bookkeeping.
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Supplemental {
			if p.Old != nil {
				r.oldSrc = p.Old.Source
			}
			if p.New != nil {
				r.newSrc = p.New.Source
			}
			break
		}
	}

	c := newColor(r.Opts.NoColor)
	var b strings.Builder

	// Header
	if oldPath != "" || newPath != "" {
		if oldPath == newPath || oldPath == "" {
			b.WriteString(fmt.Sprintf("# %s\n\n", newPath))
		} else {
			b.WriteString(fmt.Sprintf("# %s → %s\n\n", oldPath, newPath))
		}
	}

	// Group pairs by kind
	groups := r.groupByKind(result)

	// Compute moved pairs: used by --skip-unchanged and the summary's Moved counter.
	r.movedPairs = computeMovedPairs(result.Pairs)

	// Index classes so one-sided members render inside their class.
	r.classContainers = make(map[string]*ir.SemanticBlock)
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Supplemental {
			continue
		}
		if p.Old != nil && p.Old.Kind == ir.KindClass {
			r.classContainers[p.Old.ID] = p.Old
		}
		if p.New != nil && p.New.Kind == ir.KindClass {
			r.classContainers[p.New.ID] = p.New
		}
	}

	// Ordered kinds for consistent output
	// In summary-only mode, skip the detailed block listing entirely
	if !r.Opts.SummaryOnly {
		// Every pair renders (unchanged blocks appear as context lines) so the
		// coverage invariant holds; de-dup prevents repeated lines.
		for _, kind := range orderedKinds {
			if len(groups[kind]) == 0 {
				continue
			}
			// Namespaces are scope markers, not rendered components.
			if kind == ir.KindNamespace {
				continue
			}
			r.writeGroup(&b, c, kind, groups[kind])
		}

		// Extra kinds render in lexical order (no map-iteration nondeterminism).
		var extraKinds []ir.BlockKind
		for kind := range groups {
			if !containsKind(orderedKinds, kind) {
				extraKinds = append(extraKinds, kind)
			}
		}
		sort.Slice(extraKinds, func(i, j int) bool { return extraKinds[i] < extraKinds[j] })
		for _, kind := range extraKinds {
			if kind == ir.KindNamespace {
				continue
			}
			r.writeGroup(&b, c, kind, groups[kind])
		}
	}

	// Summary (blank-line separation handled by the body's trailing blank line).
	r.writeSummary(&b, c, result)

	// Trailing blank line keeps concatenated per-file reports separated.
	b.WriteString("\n")

	_, err := w.Write([]byte(b.String()))
	return err
}

// groupByKind: matched pairs group by the NEW kind (the report describes code as it is now).
func (r *TextReporter) groupByKind(result *ir.CorrelationResult) map[ir.BlockKind][]*ir.CorrelatedPair {
	groups := make(map[ir.BlockKind][]*ir.CorrelatedPair)
	for i := range result.Pairs {
		p := &result.Pairs[i]
		var kind ir.BlockKind
		switch {
		case p.Old != nil && p.New != nil:
			kind = p.New.Kind
		case p.Old != nil:
			kind = p.Old.Kind
		case p.New != nil:
			kind = p.New.Kind
		}
		if kind == "" {
			kind = ir.KindUnknown
		}
		groups[kind] = append(groups[kind], p)
	}
	return groups
}

// writeGroup: section emitted only if at least one pair rendered content (no empty headers).
func (r *TextReporter) writeGroup(b *strings.Builder, c color, kind ir.BlockKind, pairs []*ir.CorrelatedPair) {
	var body strings.Builder
	for _, p := range pairs {
		r.writePair(&body, c, p)
	}
	if body.Len() == 0 {
		return
	}
	// Section header
	header := kindHeader(kind)
	b.WriteString(fmt.Sprintf("%s%s%s\n", c.bold, header, c.reset))
	b.WriteString(body.String())
	b.WriteString("\n")
}

// writePair: every rendered component emits its header first — never without a rating or tag.
func (r *TextReporter) writePair(b *strings.Builder, c color, p *ir.CorrelatedPair) {
	r.beginPair(p)
	// --skip-unchanged suppresses identical blocks (summary still counts them); moved blocks always render.
	if r.Opts.SkipUnchanged && isCompletelyUnchanged(p, r.movedPairs[p]) {
		return
	}
	// One-sided class members render only in the class diff.
	if r.nestedInClassContainer(p) {
		return
	}

	var pair strings.Builder
	switch {
	case p.IsAdded():
		r.writePairHeader(&pair, c, p)
		r.writeAdded(&pair, c, p)
	case p.IsRemoved():
		r.writePairHeader(&pair, c, p)
		r.writeRemoved(&pair, c, p)
	default:
		if p.Old != nil && p.New != nil {
			r.writePairHeader(&pair, c, p)
			r.writeMatched(&pair, c, p)
		}
	}

	// Drop pairs whose coverage de-duplication hid every source line.
	rendered := pair.String()
	keepHeader := p.InnerDiff == nil && !p.Supplemental
	if keepHeader || (strings.Contains(rendered, "\n") && strings.TrimSpace(rendered[strings.IndexByte(rendered, '\n')+1:]) != "") {
		b.WriteString(rendered)
	}
}

// nestedInClassContainer reports whether a one-sided block belongs to a class.
func (r *TextReporter) nestedInClassContainer(p *ir.CorrelatedPair) bool {
	if p.Supplemental {
		return false
	}
	var child *ir.SemanticBlock
	switch {
	case p.IsAdded():
		child = p.New
	case p.IsRemoved():
		child = p.Old
	default:
		return false
	}
	if child == nil {
		return false
	}
	parent := r.classContainers[child.Parent]
	if parent == nil || parent.Span.EndByte <= parent.Span.StartByte {
		return false
	}
	return parent.Span.StartByte <= child.Span.StartByte && child.Span.EndByte <= parent.Span.EndByte
}

// blockRange: 1-based start + count for the @@ header; synthetic/zero-span blocks fall back to line count; nil ⇒ 0,0.
func blockRange(b *ir.SemanticBlock) (start, count int) {
	if b == nil {
		return 0, 0
	}
	if b.Span.StartByte < b.Span.EndByte || b.Span.StartLine != 0 || b.Span.EndLine != 0 {
		return int(b.Span.StartLine) + 1, int(b.Span.EndLine-b.Span.StartLine) + 1
	}
	if b.Source == "" {
		return 0, 0
	}
	n := strings.Count(b.Source, "\n")
	if !strings.HasSuffix(b.Source, "\n") {
		n++
	}
	return 1, n
}

// scopeTag formats a C/C++ namespace or class annotation.
func scopeTag(p *ir.CorrelatedPair) string {
	var scope string
	switch {
	case p.IsAdded():
		scope = p.New.Scope
	case p.IsRemoved():
		scope = p.Old.Scope
	default:
		if p.New != nil {
			scope = p.New.Scope
		}
	}
	if scope == "" {
		return ""
	}
	return fmt.Sprintf("  [in %s]", scope)
}

// writePairHeader: git-style @@ range + name + similarity or [added]/[removed],
// with [converted]/[moved]/[whitespace] tags for matched pairs.
func (r *TextReporter) writePairHeader(b *strings.Builder, c color, p *ir.CorrelatedPair) {
	oldStart, oldCount := blockRange(p.Old)
	newStart, newCount := blockRange(p.New)
	hunk := fmt.Sprintf("%s@@ -%d,%d +%d,%d @@%s", c.gray, oldStart, oldCount, newStart, newCount, c.reset)

	switch {
	case p.IsAdded():
		name := p.New.Name
		if name == "" {
			name = string(p.New.Kind)
		}
		b.WriteString(fmt.Sprintf("%s  %s%s  %s[added]%s\n", hunk, name, scopeTag(p), c.green, c.reset))
	case p.IsRemoved():
		name := p.Old.Name
		if name == "" {
			name = string(p.Old.Kind)
		}
		b.WriteString(fmt.Sprintf("%s  %s%s  %s[removed]%s\n", hunk, name, scopeTag(p), c.red, c.reset))
	default:
		oldName := p.Old.Name
		newName := p.New.Name
		label := newName
		// Unnamed pairs fall back to the kind name (headers never show an empty name).
		if label == "" {
			label = string(p.New.Kind)
		}
		// Anonymous ordinals (arrow_function.02 vs .03) are numbering artifacts, not renames.
		if oldName != newName && !sameAnonymousNumberedName(p) {
			label = fmt.Sprintf("%s → %s", oldName, newName)
		}
		var tags []string
		if p.Old.Kind != ir.KindUnknown && p.Old.Kind != p.New.Kind {
			tags = append(tags, "[converted]")
		}
		if r.movedPairs[p] {
			tags = append(tags, "[moved]")
		}
		if r.whitespaceOnly(p) {
			tags = append(tags, "[whitespace]")
		}
		sim := similarityFor(p)
		b.WriteString(fmt.Sprintf("%s  %s%s  %s%.0f%% similarity%s",
			hunk, label, scopeTag(p), c.cyan, sim, c.reset))
		// Tags use yellow: gray is reserved for the @@ range, green/red for one-sided pairs.
		for _, tag := range tags {
			b.WriteString(fmt.Sprintf("  %s%s%s", c.yellow, tag, c.reset))
		}
		b.WriteString("\n")
	}
}

// similarityFor: semantic pairs show distance similarity of the CURRENT sources (same text
// as InnerDiff); whole-file/unknown pairs show line-based similarity of the shown diff.
func similarityFor(p *ir.CorrelatedPair) float64 {
	if p.Old != nil && p.New != nil &&
		p.Old.Kind != ir.KindUnknown && p.New.Kind != ir.KindUnknown &&
		!p.Supplemental {
		// Floor, never round: a 100% header always means a clean diff.
		return math.Floor(matching.PairSimilarity(p) * 100)
	}
	return diffSimilarity(p)
}

// sameAnonymousNumberedName: both sides carry ordinal names (kind.NN) — a numbering artifact, not a rename.
func sameAnonymousNumberedName(p *ir.CorrelatedPair) bool {
	if p.Old == nil || p.New == nil || p.Old.Kind != p.New.Kind {
		return false
	}
	return matching.IsAnonymousOrdinal(p.Old.Kind, p.Old.Name) &&
		matching.IsAnonymousOrdinal(p.New.Kind, p.New.Name)
}

// whitespaceOnly: language-aware lexical whitespace classification (language-neutral helper stays for tests).
func (r *TextReporter) whitespaceOnly(p *ir.CorrelatedPair) bool {
	return isWhitespaceOnlyMatchForLanguages(p, r.Opts.OldLanguage, r.Opts.NewLanguage)
}

func (r *TextReporter) writeAdded(b *strings.Builder, c color, p *ir.CorrelatedPair) {
	r.writeSource(b, c, p.New.Source, c.green, "+", newSide)
}

func (r *TextReporter) writeRemoved(b *strings.Builder, c color, p *ir.CorrelatedPair) {
	r.writeSource(b, c, p.Old.Source, c.red, "-", oldSide)
}

func (r *TextReporter) writeMatched(b *strings.Builder, c color, p *ir.CorrelatedPair) {
	// No structural changes: render sources as context so the coverage invariant holds.
	if p.InnerDiff != nil && !p.InnerDiff.HasChanges() {
		r.writeContextSource(b, c, p)
		// "(no structural changes)" label only for clean exact-name matches.
		if p.MatchType == ir.MatchExactName {
			b.WriteString(fmt.Sprintf("%s(no structural changes)%s\n", c.gray, c.reset))
		}
		return
	}

	// Write inner diff
	if p.InnerDiff != nil {
		for _, hunk := range p.InnerDiff.Hunks {
			for _, line := range hunk.Lines {
				r.writeDiffLine(b, c, &line)
			}
		}
	}
}

func (r *TextReporter) writeDiffLine(b *strings.Builder, c color, line *ir.DiffLine) {
	// Blank lines carry no review signal and are not part of the coverage invariant.
	if line.Content == "" && line.NewContent == "" {
		return
	}
	switch line.Type {
	case ir.DiffLineAdded:
		if r.suppress && r.shouldSuppressSide(newSide, line.Content, line.NewNo, false) {
			return
		}
		r.recordContent(newSide, line.Content)
		// The added line of a replacement pair carries the word-level diff.
		if line.IsWordDiff && len(line.Words) > 0 {
			r.writeWordDiffLine(b, c, line)
			return
		}
		b.WriteString(fmt.Sprintf("%s+%s%s\n", c.green, c.reset, line.Content))
	case ir.DiffLineRemoved:
		// Word-diff paired removed lines are suppressed: the added line's
		// combined word diff already shows the removed words inline.
		if line.WordDiffPaired {
			r.recordContent(oldSide, line.Content)
			return
		}
		if r.suppress && r.shouldSuppressSide(oldSide, line.Content, line.OldNo, false) {
			return
		}
		r.recordContent(oldSide, line.Content)
		b.WriteString(fmt.Sprintf("%s-%s%s\n", c.red, c.reset, line.Content))
	default:
		// Context: under --ignore-all-space, both raw forms are emitted (old text never masquerades as new).
		newContent := line.Content
		if line.NewContent != "" {
			newContent = line.NewContent
		} else if line.NewBlank {
			newContent = ""
		}
		if r.suppress && r.supplementLineAlreadyShown(line) {
			return
		}
		if line.OldNo > 0 {
			r.recordContent(oldSide, line.Content)
		}
		if line.NewNo > 0 {
			r.recordContent(newSide, newContent)
		}
		if line.IsWordDiff && len(line.Words) > 0 {
			r.writeWordDiffLine(b, c, line)
			return
		}
		if line.NewContent != "" {
			// Two raw forms: render each side separately (both as context
			// lines; this is not a structural change).
			b.WriteString(fmt.Sprintf("%s %s\n", c.gray, line.Content))
			b.WriteString(fmt.Sprintf("%s %s\n", c.gray, line.NewContent))
			return
		}
		b.WriteString(fmt.Sprintf("%s %s\n", c.gray, line.Content))
	}
}

// supplementLineAlreadyShown: context lines suppress only when BOTH sides' obligations are satisfied.
func (r *TextReporter) supplementLineAlreadyShown(line *ir.DiffLine) bool {
	if line.Type == ir.DiffLineContext && r.Opts.SkipUnchanged {
		return true
	}
	newContent := line.Content
	if line.NewContent != "" {
		newContent = line.NewContent
	} else if line.NewBlank {
		newContent = ""
	}
	switch line.Type {
	case ir.DiffLineAdded:
		return r.shouldSuppressSide(newSide, line.Content, line.NewNo, false)
	case ir.DiffLineRemoved:
		return r.shouldSuppressSide(oldSide, line.Content, line.OldNo, false)
	default:
		oldShown := line.OldNo == 0 || r.shouldSuppressSide(oldSide, line.Content, line.OldNo, true)
		newShown := line.NewNo == 0 || r.shouldSuppressSide(newSide, newContent, line.NewNo, true)
		return oldShown && newShown
	}
}

// wordDiffColor resolves a WordDiffStyle color name to the actual ANSI code.
func wordDiffColor(c color, name string, fallback string) string {
	switch strings.ToLower(name) {
	case "red":
		return c.red
	case "green":
		return c.green
	case "yellow":
		return c.yellow
	case "cyan":
		return c.cyan
	case "magenta":
		return c.magenta
	case "gray", "grey":
		return c.gray
	case "bold":
		return c.bold
	case "":
		return fallback
	default:
		return fallback
	}
}

// writeWordDiffLine renders word-level highlights using WordDiffStyle markers and colors;
// the line prefix matches the line type (" " context, "+" added).
func (r *TextReporter) writeWordDiffLine(b *strings.Builder, c color, line *ir.DiffLine) {
	prefix := " "
	if line.Type == ir.DiffLineAdded {
		prefix = "+"
	}
	b.WriteString(fmt.Sprintf("%s%s%s", c.gray, prefix, c.reset))
	addedColor := wordDiffColor(c, WordDiffStyle.AddedColor, c.green)
	removedColor := wordDiffColor(c, WordDiffStyle.RemovedColor, c.red)
	for _, w := range line.Words {
		switch w.Type {
		case ir.DiffWordAdded:
			b.WriteString(fmt.Sprintf("%s%s%s%s%s", addedColor, WordDiffStyle.AddedPrefix, w.Text, WordDiffStyle.AddedSuffix, c.reset))
		case ir.DiffWordRemoved:
			b.WriteString(fmt.Sprintf("%s%s%s%s%s", removedColor, WordDiffStyle.RemovedPrefix, w.Text, WordDiffStyle.RemovedSuffix, c.reset))
		default:
			b.WriteString(w.Text)
		}
	}
	b.WriteString("\n")
}

func (r *TextReporter) writeSource(b *strings.Builder, c color, source string, col string, prefix, side string) {
	// Body coloring: default white text with colored '+'/'-' prefix only; AddedStyle="green"
	// or RemovedStyle="red" restore legacy full-line coloring.
	bodyIsWhite := true
	if col == c.green {
		// Added block
		style := r.Opts.AddedStyle
		if style == "" {
			style = "white"
		}
		bodyIsWhite = (style == "white")
	} else if col == c.red {
		// Removed block
		style := r.Opts.RemovedStyle
		if style == "" {
			style = "white"
		}
		bodyIsWhite = (style == "white")
	}
	for _, line := range strings.Split(source, "\n") {
		if line == "" {
			continue
		}
		r.recordContent(side, line)
		if bodyIsWhite {
			// Only the prefix is colored, body is white (readable).
			b.WriteString(fmt.Sprintf("%s%s%s%s\n", col, prefix, c.reset, line))
		} else {
			// Legacy: entire line colored (prefix + body same color).
			b.WriteString(fmt.Sprintf("%s%s%s\n", col, prefix, line))
		}
	}
}

// writeContextSource: old side in full, new side skips lines identical to old; under
// r.suppress, lines already shown by another pair (span-owned) are skipped too.
func (r *TextReporter) writeContextSource(b *strings.Builder, c color, p *ir.CorrelatedPair) {
	writeSide := func(src, side string, startLine uint, skip map[string]bool) {
		for idx, line := range strings.Split(src, "\n") {
			if line == "" {
				continue
			}
			if skip[line] {
				// Represented by the old context line; record so the supplement does not repeat it.
				r.recordContent(side, line)
				continue
			}
			if r.shouldSuppressSide(side, line, int(startLine)+idx+1, true) {
				continue
			}
			r.recordContent(side, line)
			b.WriteString(fmt.Sprintf("%s %s\n", c.gray, line))
		}
	}
	oldSet := map[string]bool{}
	if p.Old != nil {
		writeSide(p.Old.Source, oldSide, p.Old.Span.StartLine, map[string]bool{})
		for _, line := range strings.Split(p.Old.Source, "\n") {
			if line != "" {
				oldSet[line] = true
			}
		}
	}
	if p.New != nil {
		writeSide(p.New.Source, newSide, p.New.Span.StartLine, oldSet)
	}
}

func (r *TextReporter) writeSummary(b *strings.Builder, c color, result *ir.CorrelationResult) {
	matched := 0
	unchanged := 0
	changed := 0
	renamed := 0
	moved := 0
	added := 0
	removed := 0
	whitespace := 0

	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.Supplemental {
			continue // whole-file coverage supplement is not a semantic block
		}
		if p.IsAdded() {
			added++
			continue
		}
		if p.IsRemoved() {
			removed++
			continue
		}
		matched++
		isRename := p.Old != nil && p.New != nil && p.Old.Kind != ir.KindUnknown &&
			p.Old.Name != p.New.Name && !sameAnonymousNumberedName(p)
		hasChanges := p.InnerDiff != nil && p.InnerDiff.HasChanges()
		if hasChanges && !isRename && r.whitespaceOnly(p) {
			// Whitespace-only differences count as cosmetic, not structural "Changed".
			whitespace++
		} else if hasChanges {
			changed++
			if isRename {
				renamed++
			}
		} else {
			// No structural changes
			if r.whitespaceOnly(p) {
				whitespace++
			} else if isRename {
				renamed++
			} else if p.Old != nil && (p.Old.Source != p.New.Source || r.movedPairs[p]) {
				// Same name and structure: a different raw text or a reordering counts as Moved.
				moved++
			} else {
				unchanged++
			}
		}
	}

	b.WriteString(fmt.Sprintf("%sSummary%s\n", c.bold, c.reset))
	b.WriteString(fmt.Sprintf("  Matched:   %d\n", matched))
	b.WriteString(fmt.Sprintf("  Unchanged: %s%d%s\n", c.gray, unchanged, c.reset))
	b.WriteString(fmt.Sprintf("  Changed:   %s%d%s\n", c.yellow, changed, c.reset))
	b.WriteString(fmt.Sprintf("  Renamed:   %s%d%s\n", c.magenta, renamed, c.reset))
	b.WriteString(fmt.Sprintf("  Moved:     %s%d%s\n", c.cyan, moved, c.reset))
	b.WriteString(fmt.Sprintf("  Added:     %s%d%s\n", c.green, added, c.reset))
	b.WriteString(fmt.Sprintf("  Removed:   %s%d%s\n", c.red, removed, c.reset))
	// Whitespace counter only when there is at least one such change.
	if whitespace > 0 {
		b.WriteString(fmt.Sprintf("  Whitespace: %s%d%s\n", c.gray, whitespace, c.reset))
	}
	// Parse-error indicator: non-zero counts mean extraction is incomplete — treat output with caution.
	if result.OldParseErrors > 0 || result.NewParseErrors > 0 {
		b.WriteString(fmt.Sprintf("  Parse Errors: %s%d%s (old: %d, new: %d)\n",
			c.red, result.OldParseErrors+result.NewParseErrors, c.reset,
			result.OldParseErrors, result.NewParseErrors))
	}
}

func containsKind(kinds []ir.BlockKind, want ir.BlockKind) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

// kindHeaders maps block kinds to their section headers.
var kindHeaders = map[ir.BlockKind]string{
	ir.KindImport:        "Imports",
	ir.KindExport:        "Exports",
	ir.KindConstant:      "Constants",
	ir.KindVariable:      "Variables",
	ir.KindFunction:      "Functions",
	ir.KindMacroFunction: "Macro Functions",
	ir.KindMethod:        "Methods",
	ir.KindArrowFunc:     "Arrow Functions",
	ir.KindLambda:        "Lambdas",
	ir.KindClass:         "Classes",
	ir.KindNamespace:     "Namespaces",
	ir.KindInterface:     "Interfaces",
	ir.KindTypeAlias:     "Type Aliases",
	ir.KindConcept:       "Concepts",
	ir.KindEnum:          "Enums",
	ir.KindObjectMethod:  "Object Methods",
	ir.KindJSX:           "JSX",
	ir.KindTemplate:      "Templates",
	ir.KindComment:       "Comments",
	ir.KindDecorator:     "Decorators",
	ir.KindLifecycle:     "Lifecycle",
	ir.KindUnknown:       "Other",
}

// orderedKinds lists kinds in their canonical output order.
var orderedKinds = []ir.BlockKind{
	ir.KindImport,
	ir.KindConstant,
	ir.KindVariable,
	ir.KindClass,
	ir.KindInterface,
	ir.KindTypeAlias,
	ir.KindConcept,
	ir.KindEnum,
	ir.KindFunction,
	ir.KindMacroFunction,
	ir.KindMethod,
	ir.KindArrowFunc,
	ir.KindLambda,
	ir.KindObjectMethod,
	ir.KindLifecycle,
	ir.KindJSX,
	ir.KindTemplate,
	ir.KindExport,
	ir.KindComment,
	ir.KindDecorator,
}

// kindHeader returns a human-readable header for a block kind.
func kindHeader(kind ir.BlockKind) string {
	if h, ok := kindHeaders[kind]; ok {
		return h
	}
	return string(kind)
}
