// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package stats

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"cmscout/pkg/ir"
	"cmscout/pkg/metrics"
	"cmscout/pkg/parser"
)

// Options configures the statistics analysis.
type Options struct {
	// MaxCommentLines flags comments longer than this many lines in the report
	// (0 disables the check).
	MaxCommentLines int
}

// Comment is a comment data point: either a doc-comment prefix run attached to a
// block (PrefixOf set) or a comment inside a block (Owner set). StartLine/EndLine
// are 1-based.
type Comment struct {
	StartLine    int    // first line of the comment (1-based)
	EndLine      int    // last line of the comment (1-based)
	Lines        int    // number of lines the comment occupies
	Chars        int    // number of characters (runes) in the comment text
	Owner        string // qualified name of the containing function/class block ("" = file-level)
	PrefixOf     string // qualified name of the block this comment prefixes ("" = not a prefix)
	ExceedsLimit bool   // Lines > Options.MaxCommentLines (only when MaxCommentLines > 0)
}

// BlockStats holds the statistics for one semantic block.
type BlockStats struct {
	Kind       ir.BlockKind
	Name       string // simple name
	Qualified  string // fully qualified name (e.g. "Knob.updateDelta", "Ase::LoopImpl::run")
	StartLine  int    // first line of the block (1-based)
	EndLine    int    // last line of the block (1-based)
	Lines      int    // number of lines the block occupies
	Chars      int    // number of characters (runes) in the block text
	Branches   int
	Complexity int
	Methods    int // direct method-like children (rendered for classes, interfaces, namespaces)

	PrefixLines int // number of lines of the doc-comment prefix run before the block
	PrefixChars int // number of characters (runes) of the doc-comment prefix run

	PrefixComments []Comment // doc-comment prefix run before the block (in source order)
	Comments       []Comment // comments inside the block not owned by a nested block (in source order)
}

// Report is the statistics result for one source file.
type Report struct {
	FilePath    string
	Language    string
	ParseErrors int
	Status      string
	Diagnostics []metrics.Diagnostic
	Blocks      []BlockStats // all semantic blocks, in source order
	Comments    []Comment    // file-level comments (not inside any block, not prefixes), in source order
}

// is_function_like reports whether a kind counts as a function-like block that
// owns enclosed comments and receives a complexity from the metrics engine.
func is_function_like(k ir.BlockKind) bool {
	switch k {
	case ir.KindFunction, ir.KindMethod, ir.KindObjectMethod, ir.KindLifecycle,
		ir.KindArrowFunc, ir.KindLambda, ir.KindMacroFunction:
		return true
	default:
		return false
	}
}

// is_container_kind reports whether a kind participates in qualified names
// (an ancestor scope of nested blocks).
func is_container_kind(k ir.BlockKind) bool {
	switch k {
	case ir.KindClass, ir.KindInterface, ir.KindEnum, ir.KindNamespace:
		return true
	default:
		return false
	}
}

// is_method_kind reports whether a block counts as a method-like child of a class.
func is_method_kind(k ir.BlockKind) bool {
	switch k {
	case ir.KindMethod, ir.KindObjectMethod, ir.KindLifecycle, ir.KindFunction,
		ir.KindArrowFunc, ir.KindLambda, ir.KindMacroFunction:
		return true
	default:
		return false
	}
}

// shows_method_count reports whether a block kind renders a method count
// (classes, interfaces, and namespaces own method-like children).
func shows_method_count(k ir.BlockKind) bool {
	switch k {
	case ir.KindClass, ir.KindInterface, ir.KindNamespace:
		return true
	default:
		return false
	}
}

// Analyze computes statistics for all semantic blocks of a parsed document.
// Complexity comes from metrics.Measure on the AST, whose source bytes must
// match the document (as produced by extract.Extract).
func Analyze(doc *ir.SemanticDocument, ast *parser.AST, opts Options) (*Report, error) {
	if doc == nil {
		return nil, fmt.Errorf("stats: nil document")
	}
	if ast == nil {
		return nil, fmt.Errorf("stats: nil AST")
	}
	doc, complexities, snapshot, err := function_stats(doc, ast)
	if err != nil {
		return nil, err
	}
	src := ast.Source()

	by_id := make(map[string]*ir.SemanticBlock, len(doc.Blocks))
	for i := range doc.Blocks {
		by_id[doc.Blocks[i].ID] = &doc.Blocks[i]
	}

	// Source-ordered block list for prefix/inline-ownership scans.
	all := make([]*ir.SemanticBlock, 0, len(doc.Blocks))
	for i := range doc.Blocks {
		all = append(all, &doc.Blocks[i])
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].Span.StartByte != all[b].Span.StartByte {
			return all[a].Span.StartByte < all[b].Span.StartByte
		}
		return all[a].ID < all[b].ID
	})

	var comments []*ir.SemanticBlock
	for _, b := range all {
		if b.Kind == ir.KindComment {
			comments = append(comments, b)
		}
	}

	// Prefixed comments: comment blocks that form the doc-comment prefix run of
	// a following block; they are excluded from inline ownership.
	prefixed := make(map[*ir.SemanticBlock]*ir.SemanticBlock) // comment -> block it prefixes
	prefix_runs := make(map[*ir.SemanticBlock][]*ir.SemanticBlock)
	for _, b := range all {
		if b.Kind == ir.KindComment {
			continue
		}
		run := prefix_run(b, comments, src)
		if len(run) > 0 && prefixed[run[0]] == nil {
			prefix_runs[b] = run
			for _, c := range run {
				prefixed[c] = b
			}
		}
	}

	// Owned comments: for each non-prefix comment, the deepest block containing it.
	owned := make(map[*ir.SemanticBlock][]*ir.SemanticBlock) // block -> contained comments
	var file_level_comments []*ir.SemanticBlock
	for _, c := range comments {
		if _, is_prefix := prefixed[c]; is_prefix {
			continue
		}
		var owner *ir.SemanticBlock
		owner_size := ^uint(0)
		for _, b := range all {
			if b.Kind == ir.KindComment {
				continue
			}
			if b.Span.StartByte >= b.Span.EndByte {
				continue
			}
			if c.Span.StartByte < b.Span.StartByte || c.Span.EndByte > b.Span.EndByte {
				continue
			}
			if size := b.Span.EndByte - b.Span.StartByte; size < owner_size {
				owner = b
				owner_size = size
			}
		}
		if owner == nil {
			file_level_comments = append(file_level_comments, c)
		} else {
			owned[owner] = append(owned[owner], c)
		}
	}

	// Per-block child counts (methods for containers) and qualified names.
	method_counts := make(map[string]int)
	qualified := make(map[string]string, len(all))
	sep := "."
	if doc.Language == "c" || doc.Language == "cpp" {
		sep = "::"
	}
	for _, b := range all {
		// A C++ member defined outside its class repeats a declaration inside it.
		if parent := by_id[b.Parent]; parent != nil && is_method_kind(b.Kind) &&
			(doc.Language == "go" || parent.Span.StartByte <= b.Span.StartByte && b.Span.EndByte <= parent.Span.EndByte) {
			method_counts[b.Parent]++
		}
		qualified[b.ID] = qualified_name(b, by_id, sep)
	}

	report := &Report{
		FilePath:    doc.FilePath,
		Language:    doc.Language,
		ParseErrors: doc.ParseErrors,
		Status:      snapshot.Status,
		Diagnostics: snapshot.Diagnostics,
		Blocks:      make([]BlockStats, 0, len(all)),
		Comments:    make([]Comment, 0, len(file_level_comments)),
	}
	for _, b := range all {
		if b.Kind == ir.KindComment {
			// Standalone comments render as file-level data points, not blocks.
			continue
		}
		bs := BlockStats{
			Kind:           b.Kind,
			Name:           b.Name,
			Qualified:      qualified[b.ID],
			StartLine:      int(b.Span.StartLine) + 1,
			EndLine:        int(b.Span.EndLine) + 1,
			Lines:          int(b.Span.EndLine-b.Span.StartLine) + 1,
			Chars:          utf8.RuneCountInString(b.Source),
			Methods:        method_counts[b.ID],
			PrefixComments: make([]Comment, 0),
			Comments:       make([]Comment, 0),
		}
		if run := prefix_runs[b]; len(run) > 0 {
			for _, c := range run {
				bs.PrefixLines += int(c.Span.EndLine-c.Span.StartLine) + 1
				bs.PrefixChars += utf8.RuneCountInString(c.Source)
				bs.PrefixComments = append(bs.PrefixComments, comment_stats(c, "", qualified[b.ID], opts.MaxCommentLines))
			}
		}
		for _, c := range owned[b] {
			bs.Comments = append(bs.Comments, comment_stats(c, qualified[b.ID], "", opts.MaxCommentLines))
		}
		if is_function_like(b.Kind) {
			bs.Complexity = complexities[b.ID]
			bs.Branches = max(0, bs.Complexity-1)
		}
		report.Blocks = append(report.Blocks, bs)
	}
	for _, c := range file_level_comments {
		report.Comments = append(report.Comments, comment_stats(c, "", "", opts.MaxCommentLines))
	}
	return report, nil
}

// comment_stats builds the Comment record for a comment block.
func comment_stats(c *ir.SemanticBlock, owner, prefix_of string, max_lines int) Comment {
	cm := Comment{
		StartLine: int(c.Span.StartLine) + 1,
		EndLine:   int(c.Span.EndLine) + 1,
		Lines:     int(c.Span.EndLine-c.Span.StartLine) + 1,
		Chars:     utf8.RuneCountInString(c.Source),
		Owner:     owner,
		PrefixOf:  prefix_of,
	}
	if max_lines > 0 && cm.Lines > max_lines {
		cm.ExceedsLimit = true
	}
	return cm
}

// prefix_run returns the maximal doc-comment run directly before block b: own-line
// comment blocks with no blank line between them or between the run and the block.
// The comment run must end on the line directly above the block (EndLine+1 ==
// StartLine) and each member must be an own-line comment, so a trailing comment
// on the line above never counts as a prefix.
func prefix_run(b *ir.SemanticBlock, comments []*ir.SemanticBlock, src []byte) []*ir.SemanticBlock {
	if len(comments) == 0 {
		return nil
	}
	// Last comment block ending on the line directly above the block.
	last := -1
	for i := len(comments) - 1; i >= 0; i-- {
		c := comments[i]
		if c.Span.EndByte > b.Span.StartByte {
			continue
		}
		if c.Span.EndLine+1 != b.Span.StartLine {
			continue
		}
		if !is_own_line_comment(c, src) {
			return nil
		}
		last = i
		break
	}
	if last < 0 {
		return nil
	}
	run := []*ir.SemanticBlock{comments[last]}
	for i := last - 1; i >= 0; i-- {
		prev := comments[i]
		if prev.Span.EndLine+1 != run[0].Span.StartLine || !is_own_line_comment(prev, src) {
			break
		}
		run = append([]*ir.SemanticBlock{prev}, run...)
	}
	return run
}

// is_own_line_comment reports whether only whitespace precedes the comment on its
// line; trailing comments ("x = 1; // note") must not attach as prefixes.
func is_own_line_comment(c *ir.SemanticBlock, src []byte) bool {
	start := int(c.Span.StartByte)
	if start <= 0 || start > len(src) {
		return true
	}
	line_start := start - 1
	for line_start >= 0 && src[line_start] != '\n' {
		line_start--
	}
	line_start++
	for i := line_start; i < start; i++ {
		switch src[i] {
		case ' ', '\t', '\r':
		default:
			return false
		}
	}
	return true
}

// qualified_name builds the fully qualified block name: the C/C++ Scope (already
// stamped by the extractor) joined with the name, otherwise the enclosing
// container names joined with the language separator.
func qualified_name(b *ir.SemanticBlock, by_id map[string]*ir.SemanticBlock, sep string) string {
	if b.Scope != "" {
		return b.Scope + "::" + b.Name
	}
	var parts []string
	for cur := b; ; {
		if cur.Parent == "" {
			break
		}
		parent, ok := by_id[cur.Parent]
		if !ok || parent == cur {
			break
		}
		if (is_container_kind(parent.Kind) || is_function_like(parent.Kind)) && parent.Name != "" {
			parts = append([]string{parent.Name}, parts...)
		}
		cur = parent
	}
	return strings.Join(append(parts, b.Name), sep)
}
