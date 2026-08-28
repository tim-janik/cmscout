// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package extract

import (
	"strings"
	"testing"

	"cmscout/pkg/ir"
)

// commentBlocks filters extracted blocks to comments in document order.
func commentBlocks(t *testing.T, src string) []ir.SemanticBlock {
	t.Helper()
	var out []ir.SemanticBlock
	for _, b := range extractFull(t, src) {
		if b.Kind == ir.KindComment {
			out = append(out, b)
		}
	}
	return out
}

// TestMergeCommentRuns_ConsecutiveRun: a run of own-line "//" lines becomes one block.
func TestMergeCommentRuns_ConsecutiveRun(t *testing.T) {
	blocks := commentBlocks(t, "// # cmscout\n// Code Motion Scout.\n// Amalgamation of review requirements.\n")
	if len(blocks) != 1 {
		t.Fatalf("expected 1 merged comment block, got %d: %+v", len(blocks), blocks)
	}
	want := "// # cmscout\n// Code Motion Scout.\n// Amalgamation of review requirements."
	if blocks[0].Source != want {
		t.Errorf("source = %q, want %q", blocks[0].Source, want)
	}
	if blocks[0].Span.StartLine != 0 || blocks[0].Span.EndLine != 2 {
		t.Errorf("span lines = %d..%d, want 0..2", blocks[0].Span.StartLine, blocks[0].Span.EndLine)
	}
}

// TestMergeCommentRuns_StandaloneRun: a run that prefixes no component merges too.
func TestMergeCommentRuns_StandaloneRun(t *testing.T) {
	blocks := commentBlocks(t, "// first\n// second\n// third\n")
	if len(blocks) != 1 {
		t.Fatalf("expected 1 merged comment block, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Source != "// first\n// second\n// third" {
		t.Errorf("source = %q", blocks[0].Source)
	}
}

// TestMergeCommentRuns_MultiLineBlock: a multi-line "/* ... */" block stays one block.
func TestMergeCommentRuns_MultiLineBlock(t *testing.T) {
	blocks := commentBlocks(t, "/* multi\n   line block */\n// after\n")
	if len(blocks) != 2 {
		t.Fatalf("expected 2 comment blocks, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Source != "/* multi\n   line block */" {
		t.Errorf("block 0 source = %q", blocks[0].Source)
	}
	if blocks[1].Source != "// after" {
		t.Errorf("block 1 source = %q", blocks[1].Source)
	}
}

// TestMergeCommentRuns_BlankLineSeparates: a blank line ends a run.
func TestMergeCommentRuns_BlankLineSeparates(t *testing.T) {
	blocks := commentBlocks(t, "// first\n// second\n\n// next paragraph\n")
	if len(blocks) != 2 {
		t.Fatalf("expected 2 comment blocks, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Source != "// first\n// second" {
		t.Errorf("block 0 source = %q", blocks[0].Source)
	}
	if blocks[1].Source != "// next paragraph" {
		t.Errorf("block 1 source = %q", blocks[1].Source)
	}
}

// TestMergeCommentRuns_InlineCommentNotSwallowed: a trailing comment joins no run.
func TestMergeCommentRuns_InlineCommentNotSwallowed(t *testing.T) {
	blocks := commentBlocks(t, "// first\nconst x = 1; // trailing note\n// second\n")
	if len(blocks) != 3 {
		t.Fatalf("expected 3 comment blocks, got %d: %+v", len(blocks), blocks)
	}
	for i, b := range blocks {
		if strings.Contains(b.Source, "trailing") && i != 1 {
			t.Errorf("inline comment must stay its own block, got index %d: %+v", i, blocks)
		}
	}
}
