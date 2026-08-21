// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package ir defines the IR shared by all pipeline stages (internal, deliberately unversioned).
package ir

// SourceSpan describes a contiguous range in the original source text.
type SourceSpan struct {
	StartByte uint
	EndByte   uint
	StartLine uint
	StartCol  uint
	EndLine   uint
	EndCol    uint
}

// BlockKind classifies the semantic role of a block.
type BlockKind string

const (
	KindImport        BlockKind = "import"
	KindExport        BlockKind = "export"
	KindConstant      BlockKind = "constant"
	KindVariable      BlockKind = "variable"
	KindFunction      BlockKind = "function"
	KindMethod        BlockKind = "method"
	KindArrowFunc     BlockKind = "arrow_function"
	KindLambda        BlockKind = "lambda"
	KindClass         BlockKind = "class"
	KindInterface     BlockKind = "interface"
	KindTypeAlias     BlockKind = "type_alias"
	KindEnum          BlockKind = "enum"
	KindObjectMethod  BlockKind = "object_method"
	KindJSX           BlockKind = "jsx"
	KindTemplate      BlockKind = "template"
	KindMacroFunction BlockKind = "macro_function"
	KindNamespace     BlockKind = "namespace"
	KindConcept       BlockKind = "concept"
	KindComment       BlockKind = "comment"
	KindDecorator     BlockKind = "decorator"
	KindLifecycle     BlockKind = "lifecycle"
	KindUnknown       BlockKind = "unknown"
)

// SemanticBlock is the core IR entity — correlated, never judged "equivalent".
type SemanticBlock struct {
	ID     string     // identifier unique within its source document (kind/name/offset)
	Kind   BlockKind  // semantic classification
	Name   string     // best-effort name (function/method/const name)
	Scope  string     // enclosing namespace/class path for C/C++ blocks ("Ase::LoopImpl"; "" outside containers)
	Parent string     // ID of parent block (e.g. class for methods)
	Span   SourceSpan // byte range in source
	Source string     // original source text
}

// SemanticDocument is a collection of extracted blocks from a single source file.
type SemanticDocument struct {
	Language    string          // "ts", "tsx", "js", "jsx", "go", "bash", "c", "cpp"
	FilePath    string          // path to the source file (may be empty for stdin)
	Blocks      []SemanticBlock // extracted semantic blocks
	ParseErrors int             // number of tree-sitter ERROR/MISSING nodes; 0 = clean parse
}

// DiffLineType classifies a line in a diff.
type DiffLineType int

const (
	DiffLineContext DiffLineType = iota // unchanged line
	DiffLineAdded                       // line only in new
	DiffLineRemoved                     // line only in old
)

// DiffWordType classifies a word in a word-level diff.
type DiffWordType int

const (
	DiffWordContext DiffWordType = iota // unchanged word
	DiffWordAdded                       // word only in new
	DiffWordRemoved                     // word only in old
)

// DiffWord represents a single word in a word-level diff.
type DiffWord struct {
	Text string       // the word text
	Type DiffWordType // context, added, or removed
}

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Content    string       // the line text (without trailing newline)
	Type       DiffLineType // context, added, or removed
	OldNo      int          // 1-based line number in old; 0 for added lines
	NewNo      int          // 1-based line number in new; 0 for removed lines
	IsWordDiff bool         // true if Words contains word-level changes
	Words      []DiffWord   // word-level diff entries (word-diff mode)

	// NewContent: raw new-side text when a context line matches only after
	// whitespace normalization; renderers must show both raw forms.
	NewContent string

	// NewBlank: the new-side context line is blank (vs. identical raw text).
	NewBlank bool
}

// DiffHunk represents a contiguous section of a diff.
type DiffHunk struct {
	OldStart int        // 1-based start line in old
	OldLines int        // number of lines in old
	NewStart int        // 1-based start line in new
	NewLines int        // number of lines in new
	Lines    []DiffLine // lines in this hunk
}

// DiffResult represents a diff between two source blocks.
type DiffResult struct {
	Hunks []DiffHunk // computed diff hunks
}

// HasChanges reports whether the diff contains any added or removed lines.
func (d *DiffResult) HasChanges() bool {
	for _, h := range d.Hunks {
		for _, l := range h.Lines {
			if l.Type == DiffLineAdded || l.Type == DiffLineRemoved {
				return true
			}
		}
	}
	return false
}

// MatchType describes how two blocks were matched.
type MatchType string

const (
	MatchExactName  MatchType = "exact_name"
	MatchSimilarity MatchType = "similarity"
	MatchNone       MatchType = "no_match"
)

// CorrelatedPair is a matched old/new pair; Confidence is match-decision data, never displayed.
type CorrelatedPair struct {
	Old        *SemanticBlock // old block (nil if added)
	New        *SemanticBlock // new block (nil if removed)
	Confidence float64        // 0.0–1.0 match-time similarity (decision data only, not displayed)
	MatchType  MatchType      // how the pair was matched
	InnerDiff  *DiffResult    // diff between old and new source (filled by diff stage)

	// Supplemental: synthetic pair (e.g. whole-file diff) upholding the coverage
	// invariant; rendered like any pair but excluded from the summary counters.
	Supplemental bool
}

// IsAdded reports whether this pair represents a newly added block.
func (p *CorrelatedPair) IsAdded() bool { return p.Old == nil && p.New != nil }

// IsRemoved reports whether this pair represents a deleted block.
func (p *CorrelatedPair) IsRemoved() bool { return p.Old != nil && p.New == nil }

// IsMatched reports whether both sides of the pair contain a block.
func (p *CorrelatedPair) IsMatched() bool { return p.Old != nil && p.New != nil }

// CorrelationResult is the output of the correlation stage.
type CorrelationResult struct {
	Pairs          []CorrelatedPair // matched old/new pairs (including added/removed)
	OldParseErrors int              // ERROR/MISSING nodes in the old parse (0 = clean)
	NewParseErrors int              // ERROR/MISSING nodes in the new parse (0 = clean)
}
