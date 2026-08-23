// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package extract

import (
	"testing"

	"cmdiff/pkg/ir"
)

func TestMergeSingleLinePrefixRuns_MergesConsecutive(t *testing.T) {
	// Three consecutive "//" lines before a function: merged into one block.
	blocks := []ir.SemanticBlock{
		{ID: "func", Kind: ir.KindFunction, Name: "f", Span: ir.SourceSpan{StartByte: 35, EndByte: 50, StartLine: 4}},
		{ID: "c3", Kind: ir.KindComment, Source: "// line three", Span: ir.SourceSpan{StartByte: 20, EndByte: 33, StartLine: 3, EndLine: 3}},
		{ID: "c2", Kind: ir.KindComment, Source: "// line two", Span: ir.SourceSpan{StartByte: 10, EndByte: 20, StartLine: 2, EndLine: 2}},
		{ID: "c1", Kind: ir.KindComment, Source: "// line one", Span: ir.SourceSpan{StartByte: 0, EndByte: 11, StartLine: 1, EndLine: 1}},
	}

	e := &Extractor{}
	e.mergeSingleLinePrefixRuns(&blocks)

	// Three comments merged into the last one, which survives.
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (merged comment + function), got %d", len(blocks))
	}
	var merged *ir.SemanticBlock
	for i := range blocks {
		if blocks[i].ID == "c3" {
			merged = &blocks[i]
		}
	}
	if merged == nil {
		t.Fatal("expected merged comment block c3 to survive")
	}
	expected := "// line one\n// line two\n// line three"
	if merged.Source != expected {
		t.Errorf("merged source = %q, want %q", merged.Source, expected)
	}
	// Span starts at the first comment's position.
	if merged.Span.StartByte != 0 {
		t.Errorf("merged StartByte = %d, want 0", merged.Span.StartByte)
	}
	if merged.Span.StartLine != 1 {
		t.Errorf("merged StartLine = %d, want 1", merged.Span.StartLine)
	}
}

func TestMergeSingleLinePrefixRuns_NoMergeIfBlankLine(t *testing.T) {
	// Two comment blocks with a blank line between them: not merged.
	blocks := []ir.SemanticBlock{
		{ID: "func", Kind: ir.KindFunction, Name: "f", Span: ir.SourceSpan{StartByte: 35, EndByte: 50, StartLine: 5}},
		{ID: "c2", Kind: ir.KindComment, Source: "// line two", Span: ir.SourceSpan{StartByte: 20, EndByte: 33, StartLine: 4, EndLine: 4}},
		{ID: "c1", Kind: ir.KindComment, Source: "// line one", Span: ir.SourceSpan{StartByte: 0, EndByte: 11, StartLine: 1, EndLine: 1}},
		// c1 followed by c2 has a gap: EndLine 1 + 1 = 2, but c2 starts at line 4 → not adjacent.
	}

	e := &Extractor{}
	e.mergeSingleLinePrefixRuns(&blocks)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks (no merge), got %d", len(blocks))
	}
}

func TestMergeSingleLinePrefixRuns_NoMergeIfNotPrefix(t *testing.T) {
	// Comment that is not directly before a non-comment (no successor).
	blocks := []ir.SemanticBlock{
		{ID: "c1", Kind: ir.KindComment, Source: "// standalone", Span: ir.SourceSpan{StartByte: 0, EndByte: 14, StartLine: 1, EndLine: 1}},
	}

	e := &Extractor{}
	e.mergeSingleLinePrefixRuns(&blocks)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (unchanged), got %d", len(blocks))
	}
}

func TestMergeSingleLinePrefixRuns_NoMergeMultiLineComment(t *testing.T) {
	// A multi-line "/* */" comment before a function is not a single-line // comment.
	blocks := []ir.SemanticBlock{
		{ID: "func", Kind: ir.KindFunction, Name: "f", Span: ir.SourceSpan{StartByte: 20, EndByte: 35, StartLine: 3}},
		{ID: "c1", Kind: ir.KindComment, Source: "/* block */", Span: ir.SourceSpan{StartByte: 0, EndByte: 12, StartLine: 1, EndLine: 1}},
	}

	e := &Extractor{}
	e.mergeSingleLinePrefixRuns(&blocks)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (no merge), got %d", len(blocks))
	}
}
