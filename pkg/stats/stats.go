// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package stats computes per-semantic-block statistics for a single source file:
// block sizes (lines/chars), doc-comment prefix sizes, inline comment sizes,
// container method counts, and best-effort branch counts as a cyclomatic
// complexity precursor. The records are meant to feed later linting rules and
// patch-complexity assessments.
package stats

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"cmscout/pkg/ir"
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
	Kind      ir.BlockKind
	Name      string // simple name
	Qualified string // fully qualified name (e.g. "Knob.updateDelta", "Ase::LoopImpl::run")
	StartLine int    // first line of the block (1-based)
	EndLine   int    // last line of the block (1-based)
	Lines     int    // number of lines the block occupies
	Chars     int    // number of characters (runes) in the block text
	Branches  int    // decision points inside function-like blocks (cyclomatic precursor)
	Methods   int    // direct method-like children (rendered for classes, interfaces, namespaces)

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
	Blocks      []BlockStats // all semantic blocks, in source order
	Comments    []Comment    // file-level comments (not inside any block, not prefixes), in source order
}

// isFunctionLike reports whether a kind counts as a function-like block that
// owns enclosed comments and receives a branch count.
func isFunctionLike(k ir.BlockKind) bool {
	switch k {
	case ir.KindFunction, ir.KindMethod, ir.KindObjectMethod, ir.KindLifecycle,
		ir.KindArrowFunc, ir.KindLambda, ir.KindMacroFunction:
		return true
	default:
		return false
	}
}

// isContainerKind reports whether a kind participates in qualified names
// (an ancestor scope of nested blocks).
func isContainerKind(k ir.BlockKind) bool {
	switch k {
	case ir.KindClass, ir.KindInterface, ir.KindEnum, ir.KindNamespace:
		return true
	default:
		return false
	}
}

// isMethodKind reports whether a block counts as a method-like child of a class.
func isMethodKind(k ir.BlockKind) bool {
	switch k {
	case ir.KindMethod, ir.KindObjectMethod, ir.KindLifecycle, ir.KindFunction,
		ir.KindArrowFunc, ir.KindLambda, ir.KindMacroFunction:
		return true
	default:
		return false
	}
}

// showsMethodCount reports whether a block kind renders a method count
// (classes, interfaces, and namespaces own method-like children).
func showsMethodCount(k ir.BlockKind) bool {
	switch k {
	case ir.KindClass, ir.KindInterface, ir.KindNamespace:
		return true
	default:
		return false
	}
}

// branchKinds are tree-sitter node kinds that each add one decision point to a
// function's branch count. Covers TS/TSX/JS/JSX, Go, Bash, C, and C++.
var branchKinds = map[string]bool{
	"if_statement":           true, // all languages (else-if chains are their own if_statement)
	"elif_clause":            true, // bash elif
	"conditional_expression": true, // C/C++ ternary ?:
	"ternary_expression":     true, // TS/JS ternary ?:
	"for_statement":          true, // all languages (incl. bash until/select loops)
	"for_in_statement":       true, // TS/JS for-in and for-of
	"while_statement":        true, // all languages (incl. bash until)
	"do_statement":           true, // TS/JS/C/C++
	"switch_case":            true, // TS/JS case label
	"switch_default":         true, // TS/JS default label
	"expression_case":        true, // Go case label
	"default_case":           true, // Go default label
	"type_case":              true, // Go type-switch label
	"communication_case":     true, // Go select label
	"case_item":              true, // bash case label
	"catch_clause":           true, // TS/JS/C++ catch
}

// caseLabelKind reports whether a node kind is a case/default label. C and C++
// model every label (case and default) as a case_statement node; in bash a
// case_statement is the whole case construct, whose labels are case_item nodes.
func caseLabelKind(k, language string) bool {
	switch k {
	case "case_statement":
		return language == "c" || language == "cpp"
	case "switch_case", "switch_default", "expression_case", "default_case",
		"type_case", "communication_case", "case_item":
		return true
	default:
		return false
	}
}

// shortCircuitKinds are node kinds whose direct && / || children are boolean
// short-circuit operators (each adds one decision point).
var shortCircuitKinds = map[string]bool{
	"binary_expression": true, // TS/JS/Go/C/C++
	"list":              true, // bash: a && b || c
}

// Analyze computes statistics for all semantic blocks of a parsed document.
// The AST is required for branch counting; its source bytes must match the
// document (as produced by extract.Extract).
func Analyze(doc *ir.SemanticDocument, ast *parser.AST, opts Options) (*Report, error) {
	if doc == nil {
		return nil, fmt.Errorf("stats: nil document")
	}
	if ast == nil {
		return nil, fmt.Errorf("stats: nil AST")
	}
	src := ast.Source()

	byID := make(map[string]*ir.SemanticBlock, len(doc.Blocks))
	for i := range doc.Blocks {
		byID[doc.Blocks[i].ID] = &doc.Blocks[i]
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
	prefixRuns := make(map[*ir.SemanticBlock][]*ir.SemanticBlock)
	for _, b := range all {
		if b.Kind == ir.KindComment {
			continue
		}
		run := prefixRun(b, comments, src)
		if len(run) > 0 {
			prefixRuns[b] = run
			for _, c := range run {
				prefixed[c] = b
			}
		}
	}

	// Owned comments: for each non-prefix comment, the deepest block containing it.
	owned := make(map[*ir.SemanticBlock][]*ir.SemanticBlock) // block -> contained comments
	var fileLevelComments []*ir.SemanticBlock
	for _, c := range comments {
		if _, isPrefix := prefixed[c]; isPrefix {
			continue
		}
		var owner *ir.SemanticBlock
		ownerSize := ^uint(0)
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
			if size := b.Span.EndByte - b.Span.StartByte; size < ownerSize {
				owner = b
				ownerSize = size
			}
		}
		if owner == nil {
			fileLevelComments = append(fileLevelComments, c)
		} else {
			owned[owner] = append(owned[owner], c)
		}
	}

	// Per-block child counts (methods for containers) and qualified names.
	methodCounts := make(map[string]int)
	qualified := make(map[string]string, len(all))
	sep := "."
	if doc.Language == "c" || doc.Language == "cpp" {
		sep = "::"
	}
	for _, b := range all {
		if b.Parent != "" && isMethodKind(b.Kind) {
			methodCounts[b.Parent]++
		}
		qualified[b.ID] = qualifiedName(b, byID, sep)
	}

	report := &Report{
		FilePath:    doc.FilePath,
		Language:    doc.Language,
		ParseErrors: doc.ParseErrors,
		Blocks:      make([]BlockStats, 0, len(all)),
		Comments:    make([]Comment, 0, len(fileLevelComments)),
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
			Methods:        methodCounts[b.ID],
			PrefixComments: make([]Comment, 0),
			Comments:       make([]Comment, 0),
		}
		if run := prefixRuns[b]; len(run) > 0 {
			for _, c := range run {
				bs.PrefixLines += int(c.Span.EndLine-c.Span.StartLine) + 1
				bs.PrefixChars += utf8.RuneCountInString(c.Source)
				bs.PrefixComments = append(bs.PrefixComments, commentStats(c, "", qualified[b.ID], opts.MaxCommentLines))
			}
		}
		for _, c := range owned[b] {
			bs.Comments = append(bs.Comments, commentStats(c, qualified[b.ID], "", opts.MaxCommentLines))
		}
		if isFunctionLike(b.Kind) {
			bs.Branches = countBranches(ast.RootNode(), b.Span.StartByte, b.Span.EndByte, doc.Language)
		}
		report.Blocks = append(report.Blocks, bs)
	}
	for _, c := range fileLevelComments {
		report.Comments = append(report.Comments, commentStats(c, "", "", opts.MaxCommentLines))
	}
	return report, nil
}

// commentStats builds the Comment record for a comment block.
func commentStats(c *ir.SemanticBlock, owner, prefixOf string, maxLines int) Comment {
	cm := Comment{
		StartLine: int(c.Span.StartLine) + 1,
		EndLine:   int(c.Span.EndLine) + 1,
		Lines:     int(c.Span.EndLine-c.Span.StartLine) + 1,
		Chars:     utf8.RuneCountInString(c.Source),
		Owner:     owner,
		PrefixOf:  prefixOf,
	}
	if maxLines > 0 && cm.Lines > maxLines {
		cm.ExceedsLimit = true
	}
	return cm
}

// prefixRun returns the maximal doc-comment run directly before block b: own-line
// comment blocks with no blank line between them or between the run and the block.
// The comment run must end on the line directly above the block (EndLine+1 ==
// StartLine) and each member must be an own-line comment, so a trailing comment
// on the line above never counts as a prefix.
func prefixRun(b *ir.SemanticBlock, comments []*ir.SemanticBlock, src []byte) []*ir.SemanticBlock {
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
		if !isOwnLineComment(c, src) {
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
		if prev.Span.EndLine+1 != run[0].Span.StartLine || !isOwnLineComment(prev, src) {
			break
		}
		run = append([]*ir.SemanticBlock{prev}, run...)
	}
	return run
}

// isOwnLineComment reports whether only whitespace precedes the comment on its
// line; trailing comments ("x = 1; // note") must not attach as prefixes.
func isOwnLineComment(c *ir.SemanticBlock, src []byte) bool {
	start := int(c.Span.StartByte)
	if start <= 0 || start > len(src) {
		return true
	}
	lineStart := start - 1
	for lineStart >= 0 && src[lineStart] != '\n' {
		lineStart--
	}
	lineStart++
	for i := lineStart; i < start; i++ {
		switch src[i] {
		case ' ', '\t', '\r':
		default:
			return false
		}
	}
	return true
}

// qualifiedName builds the fully qualified block name: the C/C++ Scope (already
// stamped by the extractor) joined with the name, otherwise the enclosing
// container names joined with the language separator.
func qualifiedName(b *ir.SemanticBlock, byID map[string]*ir.SemanticBlock, sep string) string {
	if b.Scope != "" {
		return b.Scope + "::" + b.Name
	}
	var parts []string
	for cur := b; ; {
		if cur.Parent == "" {
			break
		}
		parent, ok := byID[cur.Parent]
		if !ok || parent == cur {
			break
		}
		if isContainerKind(parent.Kind) && parent.Name != "" {
			parts = append([]string{parent.Name}, parts...)
		}
		cur = parent
	}
	return strings.Join(append(parts, b.Name), sep)
}

// countBranches walks the AST nodes intersecting [startByte, endByte) and counts
// decision points (see branchKinds and shortCircuitKinds). Best-effort and
// language-approximate; used as a cyclomatic complexity precursor.
func countBranches(root *tree_sitter.Node, startByte, endByte uint, language string) int {
	if root == nil {
		return 0
	}
	var walk func(n *tree_sitter.Node) int
	walk = func(n *tree_sitter.Node) int {
		if n == nil {
			return 0
		}
		if n.EndByte() <= startByte || n.StartByte() >= endByte {
			return 0
		}
		count := 0
		if branchKinds[n.Kind()] || caseLabelKind(n.Kind(), language) {
			count++
		}
		if shortCircuitKinds[n.Kind()] {
			for i := uint(0); i < n.ChildCount(); i++ {
				if k := n.Child(i); k != nil && (k.Kind() == "&&" || k.Kind() == "||") {
					count++
				}
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			count += walk(n.Child(i))
		}
		return count
	}
	return walk(root)
}
