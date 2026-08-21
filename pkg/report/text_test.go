// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"cmdiff/pkg/diff"
	"cmdiff/pkg/ir"
)

func TestTextReport_AddedRemoved(t *testing.T) {
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{
				Old:        nil,
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "newFunc", Source: "function newFunc() {}"},
				Confidence: 0.0,
				MatchType:  ir.MatchNone,
			},
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "oldFunc", Source: "function oldFunc() {}"},
				New:        nil,
				Confidence: 0.0,
				MatchType:  ir.MatchNone,
			},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	err := r.Write(&buf, result, "old.tsx", "new.tsx")
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	t.Logf("report:\n%s", output)

	if !strings.Contains(output, "newFunc") {
		t.Error("should contain newFunc")
	}
	if !strings.Contains(output, "oldFunc") {
		t.Error("should contain oldFunc")
	}
	if !strings.Contains(output, "Summary") {
		t.Error("should contain summary")
	}
	if !strings.Contains(output, "Added:") {
		t.Error("should contain 'Added:'")
	}
	if !strings.Contains(output, "Removed:") {
		t.Error("should contain 'Removed:'")
	}
}

func TestTextReport_MatchedWithChanges(t *testing.T) {
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "update", Source: "function update() { this.value += 1; }"},
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "handleUpdate", Source: "function handleUpdate() { this.value += 2; }"},
				Confidence: 0.95,
				MatchType:  ir.MatchSimilarity,
				InnerDiff: &ir.DiffResult{
					Hunks: []ir.DiffHunk{
						{
							OldStart: 1, OldLines: 1,
							NewStart: 1, NewLines: 1,
							Lines: []ir.DiffLine{
								{Content: "function update() { this.value += 1; }", Type: ir.DiffLineRemoved, OldNo: 1},
								{Content: "function handleUpdate() { this.value += 2; }", Type: ir.DiffLineAdded, NewNo: 1},
							},
						},
					},
				},
			},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	err := r.Write(&buf, result, "old.tsx", "new.tsx")
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	t.Logf("report:\n%s", output)

	if !strings.Contains(output, "update → handleUpdate") {
		t.Error("should show renamed function")
	}
	if !strings.Contains(output, "Functions") {
		t.Error("should show Functions section")
	}
}

// TestTextReport_SkipUnchanged verifies the --skip-unchanged option: blocks
// identical on both sides (same name + same source) are suppressed entirely
// from the detailed listing, while changed/added/removed blocks still render
// and the summary still counts everything.
func TestTextReport_SkipUnchanged(t *testing.T) {
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "VERSION", Source: "const VERSION = '1.0.0';"},
				New:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "VERSION", Source: "const VERSION = '1.0.0';"},
				Confidence: 1.0,
				MatchType:  ir.MatchExactName,
				InnerDiff:  &ir.DiffResult{Hunks: nil},
			},
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "update", Source: "function update() { this.value += 1; }"},
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "handleUpdate", Source: "function handleUpdate() { this.value += 2; }"},
				Confidence: 0.95,
				MatchType:  ir.MatchSimilarity,
				InnerDiff: &ir.DiffResult{
					Hunks: []ir.DiffHunk{{Lines: []ir.DiffLine{
						{Content: "function update() { this.value += 1; }", Type: ir.DiffLineRemoved},
						{Content: "function handleUpdate() { this.value += 2; }", Type: ir.DiffLineAdded},
					}}},
				},
			},
			{
				Old:        nil,
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "added", Source: "function added() { return 1; }"},
				Confidence: 0.0,
				MatchType:  ir.MatchNone,
			},
		},
	}

	for _, opts := range []Options{{NoColor: true}, {NoColor: true, SkipUnchanged: true}} {
		r := &TextReporter{Opts: opts}
		var buf bytes.Buffer
		if err := r.Write(&buf, result, "old.tsx", "new.tsx"); err != nil {
			t.Fatal(err)
		}
		output := buf.String()

		// Changed and added blocks always render.
		for _, want := range []string{"handleUpdate", "function added()"} {
			if !strings.Contains(output, want) {
				t.Errorf("options %+v: expected %q in output\n%s", opts, want, output)
			}
		}

		// Summary always counts the unchanged block.
		if !strings.Contains(output, "Unchanged: 1") {
			t.Errorf("options %+v: summary should count the unchanged block\n%s", opts, output)
		}

		if opts.SkipUnchanged {
			// The unchanged block's source must not appear.
			if strings.Contains(output, "const VERSION = '1.0.0';") {
				t.Errorf("SkipUnchanged: unchanged block should be suppressed entirely\n%s", output)
			}
		} else {
			// Default mode renders unchanged blocks as context (coverage).
			if !strings.Contains(output, "const VERSION = '1.0.0';") {
				t.Errorf("default: unchanged block source should render as context\n%s", output)
			}
		}
	}
}

// TestTextReport_EmptySectionsOmitted verifies that section headers are not
// printed when none of their pairs produced visible content (task: empty
// sections must not appear in the output).
func TestTextReport_EmptySectionsOmitted(t *testing.T) {
	// A group whose pairs are all unchanged, with SkipUnchanged enabled:
	// nothing renders, so even the section header must be omitted.
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindImport, Name: "foo", Source: "import { foo } from './foo';"},
				New:        &ir.SemanticBlock{Kind: ir.KindImport, Name: "foo", Source: "import { foo } from './foo';"},
				Confidence: 1.0,
				MatchType:  ir.MatchExactName,
				InnerDiff:  &ir.DiffResult{Hunks: nil},
			},
		},
	}

	// Default mode: the unchanged import renders as context, so the section
	// header appears.
	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, result, "old.tsx", "new.tsx"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Imports\n") {
		t.Errorf("default: Imports section should be printed when it has content\n%s", buf.String())
	}

	// SkipUnchanged mode: the only pair renders nothing, so the header must
	// be omitted entirely.
	r = &TextReporter{Opts: Options{NoColor: true, SkipUnchanged: true}}
	buf.Reset()
	if err := r.Write(&buf, result, "old.tsx", "new.tsx"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Imports") {
		t.Errorf("SkipUnchanged: empty section header should not be printed\n%s", buf.String())
	}
}

// TestTextReport_BlankDiffLinesOmitted verifies that blank lines inside a
// diff (empty content) are not rendered as stray "  " / "+ " / "- " rows,
// so components are not surrounded by excessive blank context lines.
func TestTextReport_BlankDiffLinesOmitted(t *testing.T) {
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "old", Source: "a\n\n\nb"},
				New:        &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "new", Source: "a\nx\n"},
				Confidence: 1.0,
				MatchType:  ir.MatchSimilarity,
				InnerDiff: &ir.DiffResult{
					Hunks: []ir.DiffHunk{{Lines: []ir.DiffLine{
						{Content: "a", Type: ir.DiffLineContext, OldNo: 1, NewNo: 1},
						{Content: "", Type: ir.DiffLineRemoved, OldNo: 2},
						{Content: "", Type: ir.DiffLineRemoved, OldNo: 3},
						{Content: "b", Type: ir.DiffLineRemoved, OldNo: 4},
						{Content: "x", Type: ir.DiffLineAdded, NewNo: 2},
					}}},
				},
			},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, result, "old", "new"); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Logf("report:\n%s", output)

	for _, stray := range []string{"\n  - \n", "\n  + \n", "\n   \n"} {
		if strings.Contains(output, stray) {
			t.Errorf("blank diff lines should not be rendered (found %q)\n%s", stray, output)
		}
	}
	// The meaningful lines still render.
	for _, want := range []string{"  - b", "  + x"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output\n%s", want, output)
		}
	}
}

// TestTextReport_SimilarityBelow100WhenChanged verifies that a pair whose
// component text actually changed displays its distance-based similarity below
// 100%, while a truly unchanged pair displays 100%. The displayed number is
// derived from the pair's final source text at render time (the same text the
// diff was computed from) — never from the stored match-time Confidence.
func TestTextReport_SimilarityBelow100WhenChanged(t *testing.T) {
	pairs := []ir.CorrelatedPair{
		{
			Old:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "VERSION", Source: "const VERSION = '1.0.0';"},
			New:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "VERSION", Source: "const VERSION = '1.0.1';"},
			Confidence: 1.0, // deliberately WRONG: the report must not trust it
			MatchType:  ir.MatchExactName,
			InnerDiff: &ir.DiffResult{
				Hunks: []ir.DiffHunk{{Lines: []ir.DiffLine{
					{Content: "const VERSION = '1.0.0';", Type: ir.DiffLineRemoved},
					{Content: "const VERSION = '1.0.1';", Type: ir.DiffLineAdded},
				}}},
			},
		},
		{
			Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"},
			New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() { return 1; }"},
			Confidence: 0.5, // deliberately WRONG: identical text must display 100%
			MatchType:  ir.MatchExactName,
			InnerDiff:  &ir.DiffResult{Hunks: nil},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, &ir.CorrelationResult{Pairs: pairs}, "old.tsx", "new.tsx"); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Logf("report:\n%s", output)

	wantChanged := fmt.Sprintf("@@ -1,1 +1,1 @@  VERSION  %.0f%% similarity", similarityFor(&pairs[0]))
	if !strings.Contains(output, wantChanged) {
		t.Errorf("changed pair should display the distance similarity of its final text (%s), got:\n%s", wantChanged, output)
	}
	if strings.Contains(output, "@@ -1,1 +1,1 @@  VERSION  100% similarity") {
		t.Errorf("changed pair must not display 100%% similarity even if stored Confidence is 1.0\n%s", output)
	}
	if !strings.Contains(output, "@@ -1,1 +1,1 @@  foo  100% similarity") {
		t.Errorf("unchanged pair should display 100%% similarity regardless of stored Confidence\n%s", output)
	}
}

// TestTextReport_MovedDetection verifies that reordered blocks are detected
// (rendered with a [moved] marker, counted as Moved, and not suppressed by
// --skip-unchanged), while blocks merely displaced by unrelated insertions
// keep their relative order and stay "completely unchanged".
func TestTextReport_MovedDetection(t *testing.T) {
	mk := func(kind ir.BlockKind, name, src string, oldLn, newLn int) ir.CorrelatedPair {
		return ir.CorrelatedPair{
			Old:        &ir.SemanticBlock{Kind: kind, Name: name, Source: src, Span: ir.SourceSpan{StartLine: uint(oldLn - 1), EndLine: uint(oldLn)}},
			New:        &ir.SemanticBlock{Kind: kind, Name: name, Source: src, Span: ir.SourceSpan{StartLine: uint(newLn - 1), EndLine: uint(newLn)}},
			Confidence: 1.0,
			MatchType:  ir.MatchExactName,
			InnerDiff:  &ir.DiffResult{Hunks: nil},
		}
	}

	// Old order: A(1), B(10), C(20). New order: C(1), A(2), B(3) — a
	// rotation: C is reordered; A and B keep their relative order (they are
	// only displaced by C's move, not moved themselves).
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			mk(ir.KindFunction, "A", "function A() {}", 1, 2),
			mk(ir.KindFunction, "B", "function B() {}", 10, 3),
			mk(ir.KindFunction, "C", "function C() {}", 20, 1),
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true, SkipUnchanged: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, result, "old.tsx", "new.tsx"); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Logf("report:\n%s", output)

	// C moved → rendered with the marker; A and B suppressed (displaced only).
	if !strings.Contains(output, "@@ -20,2 +1,2 @@  C  100% similarity  [moved]") {
		t.Errorf("reordered block C should render with [moved]\n%s", output)
	}
	for _, gone := range []string{"function A() {}", "function B() {}", "@@ -1,2 +2,2 @@  A ", "@@ -10,2 +3,2 @@  B "} {
		if strings.Contains(output, gone) {
			t.Errorf("--skip-unchanged: displaced block %q should be suppressed\n%s", gone, output)
		}
	}
	if !strings.Contains(output, "Unchanged: 2") || !strings.Contains(output, "Moved:     1") {
		t.Errorf("summary should count A/B as unchanged and C as moved\n%s", output)
	}
}

func TestTextReport_Unchanged(t *testing.T) {
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "VERSION", Source: "const VERSION = '1.0.0';"},
				New:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "VERSION", Source: "const VERSION = '1.0.0';"},
				Confidence: 1.0,
				MatchType:  ir.MatchExactName,
				InnerDiff:  &ir.DiffResult{Hunks: nil},
			},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	err := r.Write(&buf, result, "old.tsx", "new.tsx")
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	t.Logf("report:\n%s", output)

	// Coverage invariant: an unchanged block still renders its source as
	// context lines, so every non-empty input line appears in the report.
	if !strings.Contains(output, "const VERSION = '1.0.0';") {
		t.Error("unchanged block source should be rendered as context lines")
	}
	// The unchanged count still appears in summary.
	if !strings.Contains(output, "Unchanged:") {
		t.Error("summary should show unchanged count")
	}
}

func TestTextReport_SummaryOnly(t *testing.T) {
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
				Confidence: 1.0,
				MatchType:  ir.MatchExactName,
				InnerDiff:  &ir.DiffResult{Hunks: nil},
			},
			{
				Old:        nil,
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "bar", Source: "function bar() {}"},
				Confidence: 0.0,
				MatchType:  ir.MatchNone,
			},
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "baz", Source: "function baz() {}"},
				New:        nil,
				Confidence: 0.0,
				MatchType:  ir.MatchNone,
			},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true, SummaryOnly: true}}
	var buf bytes.Buffer
	err := r.Write(&buf, result, "old.tsx", "new.tsx")
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	t.Logf("report:\n%s", output)

	if strings.Contains(output, "function foo") {
		t.Error("summary-only mode should not include source lines")
	}
	if strings.Contains(output, "foo") {
		t.Error("summary-only mode should not include block names")
	}
	if !strings.Contains(output, "Summary") {
		t.Error("summary-only mode should include summary")
	}
}

func TestTextReport_MovedCounter(t *testing.T) {
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			// Unchanged block
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
				Confidence: 1.0,
				MatchType:  ir.MatchExactName,
				InnerDiff:  &ir.DiffResult{Hunks: nil},
			},
			// Moved block (same structure, different raw source)
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() { /* moved */ }"},
				Confidence: 1.0,
				MatchType:  ir.MatchExactName,
				InnerDiff:  &ir.DiffResult{Hunks: nil},
			},
			// Renamed block (structural change + rename)
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "foo", Source: "function foo() {}"},
				New:        &ir.SemanticBlock{Kind: ir.KindFunction, Name: "bar", Source: "function bar() {}"},
				Confidence: 0.9,
				MatchType:  ir.MatchSimilarity,
				InnerDiff: &ir.DiffResult{
					Hunks: []ir.DiffHunk{{Lines: []ir.DiffLine{
						{Content: "old", Type: ir.DiffLineRemoved},
						{Content: "new", Type: ir.DiffLineAdded},
					}}},
				},
			},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	err := r.Write(&buf, result, "old.tsx", "new.tsx")
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	t.Logf("report:\n%s", output)

	// Verify summary counts
	if !strings.Contains(output, "Unchanged: 1") {
		t.Errorf("expected Unchanged: 1, got:\n%s", output)
	}
	if !strings.Contains(output, "Moved:     1") {
		t.Errorf("expected Moved: 1, got:\n%s", output)
	}
	if !strings.Contains(output, "Renamed:   1") {
		t.Errorf("expected Renamed: 1, got:\n%s", output)
	}
}

// commentMovementReport builds a report for a matched comment pair whose
// enclosing container changed (F11). It mirrors the real pipeline's shape:
// the container pairs carry their COLLAPSED sources (matched children
// replaced by references, prefix comments absorbed), so the raw supplement
// line of a moved comment differs from the semantic rendering only by
// indentation. A whole-file supplemental pair carries the raw sources, as
// the CLI appends it.
func commentMovementReport(t *testing.T, pairs ...ir.CorrelatedPair) string {
	t.Helper()
	result := &ir.CorrelationResult{Pairs: pairs}
	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, result, "old.ts", "new.ts"); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestTextReport_CommentMovedOutOfContainer: a prefix comment moved out of
// its class is a matched, unchanged comment pair whose container changed —
// it renders with [moved], counts as Moved, and the coverage supplement
// must NOT re-show the old indented comment line as a raw removal (its
// only difference from the semantic rendering is indentation, and the
// comment pair owns the line) (F11).
func TestTextReport_CommentMovedOutOfContainer(t *testing.T) {
	oldSrc := "class A {\n  // note\n  foo() {}\n}\n"
	newSrc := "// note\nclass A {\n  foo() {}\n}\n"
	commentText := "// note"
	collapsed := "class A {\n  // [matched: method foo]\n}\n"

	commentPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: commentText,
			Span: ir.SourceSpan{StartByte: 12, EndByte: 12 + uint(len(commentText)), StartLine: 1, EndLine: 1}},
		New: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: commentText,
			Span: ir.SourceSpan{StartByte: 0, EndByte: uint(len(commentText)), StartLine: 0, EndLine: 0}},
		Confidence: 1.0,
		MatchType:  ir.MatchSimilarity,
		InnerDiff:  &ir.DiffResult{},
	}
	classPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsed,
			Span: ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc)), StartLine: 0, EndLine: 3}},
		New: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsed,
			Span: ir.SourceSpan{StartByte: 8, EndByte: uint(len(newSrc)), StartLine: 1, EndLine: 3}},
		Confidence: 1.0,
		MatchType:  ir.MatchExactName,
		InnerDiff:  &ir.DiffResult{},
	}
	methodPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindMethod, Name: "foo", Source: "foo() {}",
			Span: ir.SourceSpan{StartByte: 22, EndByte: 30, StartLine: 2, EndLine: 2}},
		New: &ir.SemanticBlock{Kind: ir.KindMethod, Name: "foo", Source: "foo() {}",
			Span: ir.SourceSpan{StartByte: 18, EndByte: 26, StartLine: 2, EndLine: 2}},
		Confidence: 1.0,
		MatchType:  ir.MatchExactName,
		InnerDiff:  &ir.DiffResult{},
	}
	whole := diff.New().DiffFull(oldSrc, newSrc)
	supplemental := ir.CorrelatedPair{
		Old:          &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "old.ts", Source: oldSrc},
		New:          &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "new.ts", Source: newSrc},
		InnerDiff:    whole,
		Confidence:   1.0,
		MatchType:    ir.MatchSimilarity,
		Supplemental: true,
	}

	output := commentMovementReport(t, classPair, methodPair, commentPair, supplemental)
	t.Logf("report:\n%s", output)

	// The comment moved out of its container: [moved] marker + Moved count.
	if !strings.Contains(output, "[moved]") {
		t.Errorf("moved comment must render [moved]:\n%s", output)
	}
	if !strings.Contains(output, "Moved:     1") {
		t.Errorf("summary must count the moved comment:\n%s", output)
	}
	// The raw supplement must not duplicate the old comment line: it
	// differs from the semantic context line only by indentation and the
	// comment pair owns the line.
	if strings.Contains(output, "-   // note") {
		t.Errorf("supplement must not re-show the indented old comment as a raw removal:\n%s", output)
	}
	// The comment's content still renders once, as the semantic context
	// line of the matched pair.
	if !strings.Contains(output, "   // note") {
		t.Errorf("the comment must render as a semantic context line:\n%s", output)
	}
	// The nested method's raw line stays in the supplement (it is not
	// comment-owned), so coverage is preserved for it.
	if !strings.Contains(output, "  foo() {}") {
		t.Errorf("the nested method raw line must stay visible:\n%s", output)
	}
}

// TestTextReport_UnnamedMatchedHeaderFallsBackToKind: a matched pair whose
// blocks have no name (e.g. comments) must render a header with the kind
// name, never an empty label with a double space ("~   87% similarity") —
// the same convention added/removed headers already use.
func TestTextReport_UnnamedMatchedHeaderFallsBackToKind(t *testing.T) {
	result := &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{
			{
				Old:        &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: "// a"},
				New:        &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: "// b"},
				Confidence: 0.5,
				MatchType:  ir.MatchSimilarity,
				InnerDiff: &ir.DiffResult{
					Hunks: []ir.DiffHunk{{Lines: []ir.DiffLine{
						{Content: "// a", Type: ir.DiffLineRemoved, OldNo: 1},
						{Content: "// b", Type: ir.DiffLineAdded, NewNo: 1},
					}}},
				},
			},
		},
	}

	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, result, "old.tsx", "new.tsx"); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Logf("report:\n%s", output)

	if !strings.Contains(output, "@@ -1,1 +1,1 @@  comment  87%") {
		t.Errorf("unnamed matched pair header must fall back to the kind name:\n%s", output)
	}
	if strings.Contains(output, "@@    87%") {
		t.Errorf("unnamed matched pair header must not render an empty label:\n%s", output)
	}
}

// TestTextReport_CommentMovedBetweenParents: a comment that moved from one
// class into another is moved (its enclosing container pair changed), even
// though it stayed nested on both sides.
func TestTextReport_CommentMovedBetweenParents(t *testing.T) {
	oldSrc := "class A {\n  // note\n  foo() {}\n}\n"
	newSrc := "class B {\n  // note\n  foo() {}\n}\n"
	commentText := "// note"
	collapsedA := "class A {\n  // [matched: method foo]\n}\n"
	collapsedB := "class B {\n  // [matched: method foo]\n}\n"

	commentPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: commentText,
			Span: ir.SourceSpan{StartByte: 12, EndByte: 12 + uint(len(commentText)), StartLine: 1, EndLine: 1}},
		New: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: commentText,
			Span: ir.SourceSpan{StartByte: 12, EndByte: 12 + uint(len(commentText)), StartLine: 1, EndLine: 1}},
		Confidence: 1.0,
		MatchType:  ir.MatchSimilarity,
		InnerDiff:  &ir.DiffResult{},
	}
	classAPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsedA,
			Span: ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc)), StartLine: 0, EndLine: 3}},
		New: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsedA,
			Span: ir.SourceSpan{StartByte: 0, EndByte: uint(len(collapsedA)), StartLine: 0, EndLine: 3}},
		Confidence: 1.0,
		MatchType:  ir.MatchExactName,
		InnerDiff:  &ir.DiffResult{},
	}
	classBPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindClass, Name: "B", Source: collapsedB,
			Span: ir.SourceSpan{StartByte: 0, EndByte: uint(len(collapsedB)), StartLine: 0, EndLine: 3}},
		New: &ir.SemanticBlock{Kind: ir.KindClass, Name: "B", Source: collapsedB,
			Span: ir.SourceSpan{StartByte: 0, EndByte: uint(len(newSrc)), StartLine: 0, EndLine: 3}},
		Confidence: 1.0,
		MatchType:  ir.MatchExactName,
		InnerDiff:  &ir.DiffResult{},
	}

	output := commentMovementReport(t, classAPair, classBPair, commentPair)
	t.Logf("report:\n%s", output)

	// The comment changed containers (A's span on the old side, B's span
	// on the new side): moved.
	if !strings.Contains(output, "[moved]") {
		t.Errorf("comment moved between parents must render [moved]:\n%s", output)
	}
	if !strings.Contains(output, "Moved:     1") {
		t.Errorf("summary must count the moved comment:\n%s", output)
	}
}

// TestTextReport_CommentRewordedWhileMoving: a comment that is BOTH
// reworded and moved renders [moved], counts as Changed (the movement
// itself is tagged, not double-counted — the same convention as code
// pairs), and its raw old/new lines are not duplicated by the supplement.
func TestTextReport_CommentRewordedWhileMoving(t *testing.T) {
	oldSrc := "class A {\n  // old note\n  foo() {}\n}\n"
	newSrc := "// new note\nclass A {\n  foo() {}\n}\n"
	collapsed := "class A {\n  // [matched: method foo]\n}\n"

	commentPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: "// old note",
			Span: ir.SourceSpan{StartByte: 12, EndByte: 23, StartLine: 1, EndLine: 1}},
		New: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: "// new note",
			Span: ir.SourceSpan{StartByte: 0, EndByte: 11, StartLine: 0, EndLine: 0}},
		Confidence: 0.9,
		MatchType:  ir.MatchSimilarity,
		InnerDiff: &ir.DiffResult{
			Hunks: []ir.DiffHunk{{Lines: []ir.DiffLine{
				{Content: "// old note", Type: ir.DiffLineRemoved, OldNo: 1},
				{Content: "// new note", Type: ir.DiffLineAdded, NewNo: 1},
			}}},
		},
	}
	classPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsed,
			Span: ir.SourceSpan{StartByte: 0, EndByte: uint(len(oldSrc)), StartLine: 0, EndLine: 3}},
		New: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsed,
			Span: ir.SourceSpan{StartByte: 11, EndByte: uint(len(newSrc)), StartLine: 1, EndLine: 3}},
		Confidence: 1.0,
		MatchType:  ir.MatchExactName,
		InnerDiff:  &ir.DiffResult{},
	}
	whole := diff.New().DiffFull(oldSrc, newSrc)
	supplemental := ir.CorrelatedPair{
		Old:          &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "old.ts", Source: oldSrc},
		New:          &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "new.ts", Source: newSrc},
		InnerDiff:    whole,
		Confidence:   1.0,
		MatchType:    ir.MatchSimilarity,
		Supplemental: true,
	}

	output := commentMovementReport(t, classPair, commentPair, supplemental)
	t.Logf("report:\n%s", output)

	if !strings.Contains(output, "[moved]") {
		t.Errorf("reworded-while-moving comment must render [moved]:\n%s", output)
	}
	if !strings.Contains(output, "Changed:   1") {
		t.Errorf("the rewording must count as Changed:\n%s", output)
	}
	if strings.Contains(output, "-   // old note") || strings.Contains(output, "+   // new note") {
		t.Errorf("supplement must not duplicate the reworded comment lines:\n%s", output)
	}
}

// TestTextReport_DuplicateCommentTextNotHidden: two identical comments on
// the old side (one inside a class, one top-level), one matching comment on
// the new side. The surviving top-level pair is NOT moved, and the raw
// indented line of the OTHER identical comment stays visible in the
// supplement: the indentation-aware de-duplication requires the comment
// pair to OWN the line (span), so a duplicate text elsewhere is never
// hidden (F11).
func TestTextReport_DuplicateCommentTextNotHidden(t *testing.T) {
	oldSrc := "class A {\n  // note\n  foo() {}\n}\n// note\n"
	newSrc := "class A {\n  foo() {}\n}\n// note\n"
	commentText := "// note"
	collapsed := "class A {\n  // [matched: method foo]\n}\n"

	commentPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: commentText,
			Span: ir.SourceSpan{StartByte: 33, EndByte: 33 + uint(len(commentText)), StartLine: 4, EndLine: 4}},
		New: &ir.SemanticBlock{Kind: ir.KindComment, Name: "", Source: commentText,
			Span: ir.SourceSpan{StartByte: 23, EndByte: 23 + uint(len(commentText)), StartLine: 2, EndLine: 2}},
		Confidence: 1.0,
		MatchType:  ir.MatchSimilarity,
		InnerDiff:  &ir.DiffResult{},
	}
	classPair := ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsed,
			Span: ir.SourceSpan{StartByte: 0, EndByte: 33, StartLine: 0, EndLine: 3}},
		New: &ir.SemanticBlock{Kind: ir.KindClass, Name: "A", Source: collapsed,
			Span: ir.SourceSpan{StartByte: 0, EndByte: 23, StartLine: 1, EndLine: 3}},
		Confidence: 1.0,
		MatchType:  ir.MatchExactName,
		InnerDiff:  &ir.DiffResult{},
	}
	whole := diff.New().DiffFull(oldSrc, newSrc)
	supplemental := ir.CorrelatedPair{
		Old:          &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "old.ts", Source: oldSrc},
		New:          &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "new.ts", Source: newSrc},
		InnerDiff:    whole,
		Confidence:   1.0,
		MatchType:    ir.MatchSimilarity,
		Supplemental: true,
	}

	output := commentMovementReport(t, classPair, commentPair, supplemental)
	t.Logf("report:\n%s", output)

	// The surviving pair is container-less on both sides: not moved.
	if strings.Contains(output, "[moved]") {
		t.Errorf("top-level comment pair must not be marked moved:\n%s", output)
	}
	// The raw indented line of the OTHER (absorbed) comment is not owned
	// by the surviving pair and must stay visible as raw context.
	if !strings.Contains(output, "  // note") {
		t.Errorf("the duplicate comment line must stay visible in the supplement:\n%s", output)
	}
}

// TestTextReport_DeterministicOrdering: rendering must be byte-identical
// across repeated calls, including when similarity scores tie and when
// added/removed pairs interleave with matched ones (no map-iteration or
// goroutine dependence).
func TestTextReport_DeterministicOrdering(t *testing.T) {
	mk := func(kind ir.BlockKind, name, src string) ir.CorrelatedPair {
		return ir.CorrelatedPair{
			Old:        &ir.SemanticBlock{Kind: kind, Name: name, Source: src},
			New:        &ir.SemanticBlock{Kind: kind, Name: name, Source: src},
			Confidence: 0.8, // identical text must still display 100% via PairSimilarity
			MatchType:  ir.MatchExactName,
			InnerDiff:  &ir.DiffResult{Hunks: nil},
		}
	}
	result := &ir.CorrelationResult{

		Pairs: []ir.CorrelatedPair{
			{Old: nil, New: &ir.SemanticBlock{Kind: ir.KindFunction, Name: "added", Source: "function added() { return 1; }"}},
			mk(ir.KindFunction, "zzz", "function zzz() { return 1; }"),
			{Old: &ir.SemanticBlock{Kind: ir.KindConstant, Name: "gone", Source: "const gone = 1;"}, New: nil},
			mk(ir.KindMethod, "foo", "foo() { return 1; }"),
			mk(ir.KindClass, "Aaa", "class Aaa {}"),
			mk(ir.KindFunction, "aaa", "function aaa() { return 1; }"),
		},
	}

	render := func() string {
		r := &TextReporter{Opts: Options{NoColor: true}}
		var buf bytes.Buffer
		if err := r.Write(&buf, result, "old.tsx", "new.tsx"); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	first := render()
	for i := 0; i < 5; i++ {
		if again := render(); again != first {
			t.Fatalf("render %d differs from the first rendering\n--- first:\n%s\n--- again:\n%s", i, first, again)
		}
	}
	// Kinds render in canonical order regardless of pair order: Constants,
	// Classes, Functions, Methods.
	vIdx := strings.Index(first, "Constants\n")
	cIdx := strings.Index(first, "Classes\n")
	fIdx := strings.Index(first, "Functions\n")
	mIdx := strings.Index(first, "Methods\n")
	if vIdx < 0 || cIdx < 0 || fIdx < 0 || mIdx < 0 {
		t.Fatalf("expected Constants, Classes, Functions, Methods sections:\n%s", first)
	}
	if !(vIdx < cIdx && cIdx < fIdx && fIdx < mIdx) {
		t.Errorf("section order must be canonical (Constants < Classes < Functions < Methods), got %d %d %d %d", vIdx, cIdx, fIdx, mIdx)
	}
}

// TestWhitespaceClassificationLexical: whitespace-only classification must
// be lexical, not byte-naive. Meaningful changes inside strings, template
// literals, comments, regex literals, template interpolations, and at
// identifier/operator boundaries increment Changed; pure formatting
// changes (indentation, trailing space, operator and brace spacing) alone
// increment Whitespace. Multi-character operators are single tokens, so
// splitting one apart (x === y → x = = = y) is a change while spacing
// around it is not (F10).
func TestWhitespaceClassificationLexical(t *testing.T) {
	cases := []struct {
		name   string
		oldSrc string
		newSrc string
		wantWS bool // true: pure formatting → Whitespace; false: semantic → Changed
	}{
		{"indentation-only", "const a = 1;", "  const a = 1;", true},
		{"trailing space", "const a = 1;", "const a = 1; ", true},
		{"operator spacing", "a + b", "a+b", true},
		{"brace spacing", "foo (x)", "foo(x)", true},
		{"newline vs space", "foo(\n  x\n)", "foo( x )", true},
		{"string content", `const s = "a b";`, `const s = "ab";`, false},
		{"string quote style", `const s = "a";`, `const s = 'a';`, false},
		{"template literal", "const t = `a b`;", "const t = `ab`;", false},
		{"comment wording join", "// a b", "// ab", false},
		{"comment text change", "// note", "// note changed", false},
		{"identifier boundary", "return value", "returnvalue", false},
		{"mixed formatting plus semantic", "const s = \"a b\";\n", "  const s = \"ab\";\n", false},

		// Multi-character operators are single tokens (F10).
		{"arrow split", "(x) => x", "(x) = > x", false},
		{"optional chain split", "a?.b", "a ? .b", false},
		{"strict equality split", "x === y", "x = = = y", false},
		{"inequality split", "x !== y", "x ! == y", false},
		{"logical and split", "a && b", "a & & b", false},
		{"logical or split", "a || b", "a | | b", false},
		{"nullish split", "a ?? b", "a ? ? b", false},
		{"decrement split", "a--b", "a - -b", false},
		{"shift-assign split", "x <<= 1", "x < <= 1", false},
		{"go assign split", "x := 1", "x : = 1", false},
		{"bash case split", "a) ;;", "a) ; ;", false},
		{"equality spacing", "x == y", "x ==  y", true},
		{"arrow spacing", "(x) => x", "(x) =>  x", true},
		{"optional chain spacing", "a?.b", "a ?.b", true},
		{"shift spacing", "x >>= 1", "x >>=  1", true},
		{"go assign spacing", "x := 1", "x:=1", true},
		{"go channel spacing", "a <- b", "a<-b", true},
		{"rest spacing", "f(...args)", "f( ... args )", true},

		// Regex literals are atomic; division is punctuation (F10).
		{"regex content", "const r = /a b/;", "const r = /ab/;", false},
		{"regex internal spacing", "const r = /a b/;", "const r = /a  b/;", false},
		{"regex flags spacing", "const r = /a b/g;", "const r = /a b /g;", false},
		{"regex vs division", "x = a / b;", "x = a/b;", true},
		{"division chain", "x = a / b / c;", "x = a/b/c;", true},
		{"regex spacing", "const r = /a/;", "const r =  /a/;", true},

		// Template interpolations are tokenized structurally (F10).
		{"interpolation spacing", "const t = `a ${x} b`;", "const t = `a ${ x } b`;", true},
		{"interpolation member spacing", "const t = `a ${x.y} b`;", "const t = `a ${x . y} b`;", true},
		{"interpolation expression change", "const t = `a ${x} b`;", "const t = `a ${y} b`;", false},
		{"template text change", "const t = `a ${x} b`;", "const t = `a ${x} c`;", false},

		// JSX text: word changes are semantic, spacing between words is
		// formatting (F10 decision).
		{"jsx word join", "<div>hello world</div>", "<div>helloworld</div>", false},
		{"jsx text spacing", "<div>hello world</div>", "<div>hello  world</div>", true},

		// Bash/Go comment and string forms (F10).
		{"bash comment wording", "# a b", "# ab", false},
		{"bash comment spacing is content", "# a b", "# a  b", false},
		{"go raw string", "const s = `a b`;", "const s = `ab`;", false},
		{"go rune", "r := 'a'", "r := 'a '", false},
		{"bash double-quoted", `echo "a b"`, `echo "ab"`, false},
		{"bash arithmetic spacing", "x=$((a / b))", "x=$((a/b))", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &ir.CorrelatedPair{
				Old: &ir.SemanticBlock{Source: tc.oldSrc},
				New: &ir.SemanticBlock{Source: tc.newSrc},
			}
			if got := isWhitespaceOnlyMatch(p); got != tc.wantWS {
				t.Errorf("isWhitespaceOnlyMatch(%q, %q) = %v, want %v", tc.oldSrc, tc.newSrc, got, tc.wantWS)
			}
		})
	}
}

// TestSummary_WhitespaceClassificationLexical: the summary counters must
// follow the lexical classification — a meaningful string/comment/token
// change increments Changed (and no Whitespace row appears), while a pure
// formatting change increments Whitespace.
func TestWhitespaceClassificationUsesSourceLanguage(t *testing.T) {
	pair := &ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Source: "var s = `a ${ x } b`"},
		New: &ir.SemanticBlock{Source: "var s = `a ${  x  } b`"},
	}
	if isWhitespaceOnlyMatchForLanguages(pair, "go", "go") {
		t.Error("whitespace inside a Go raw string is source content, not formatting")
	}
	if !isWhitespaceOnlyMatchForLanguages(pair, "ts", "ts") {
		t.Error("spacing inside a TypeScript template interpolation should be formatting")
	}
	// A language conversion must tokenize each side with its own rules; a Go
	// raw string and a TypeScript template are not interchangeable literals.
	pair.New.Source = "const s = `a ${ x } b`"
	if isWhitespaceOnlyMatchForLanguages(pair, "go", "ts") {
		t.Error("Go raw-string to TypeScript-template conversion is not whitespace-only")
	}

	bashComment := &ir.CorrelatedPair{
		Old: &ir.SemanticBlock{Source: "#note here"},
		New: &ir.SemanticBlock{Source: "#note  here"},
	}
	if isWhitespaceOnlyMatchForLanguages(bashComment, "bash", "bash") {
		t.Error("spacing inside an unspaced Bash comment is source content")
	}
}

func TestSummary_WhitespaceClassificationLexical(t *testing.T) {
	mkPair := func(oldSrc, newSrc string) ir.CorrelatedPair {
		return ir.CorrelatedPair{
			Old:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "x", Source: oldSrc},
			New:        &ir.SemanticBlock{Kind: ir.KindConstant, Name: "x", Source: newSrc},
			Confidence: 0.9,
			MatchType:  ir.MatchSimilarity,
			InnerDiff: &ir.DiffResult{Hunks: []ir.DiffHunk{{Lines: []ir.DiffLine{
				{Content: oldSrc, Type: ir.DiffLineRemoved, OldNo: 1},
				{Content: newSrc, Type: ir.DiffLineAdded, NewNo: 1},
			}}}},
		}
	}
	cases := []struct {
		name       string
		pair       ir.CorrelatedPair
		wantChange bool // true: counts as Changed; false: counts as Whitespace
	}{
		{"string content", mkPair(`const s = "a b";`, `const s = "ab";`), true},
		{"comment wording", mkPair("// a b", "// ab"), true},
		{"identifier boundary", mkPair("return value", "returnvalue"), true},
		{"operator split", mkPair("x === y", "x = = = y"), true},
		{"indentation", mkPair("const a = 1;", "  const a = 1;"), false},
		{"operator spacing", mkPair("x === y", "x ===  y"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &TextReporter{Opts: Options{NoColor: true}}
			var buf bytes.Buffer
			if err := r.Write(&buf, &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{tc.pair}}, "old.ts", "new.ts"); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			if tc.wantChange {
				if !strings.Contains(out, "Changed:   1") {
					t.Errorf("meaningful change must increment Changed:\n%s", out)
				}
				if strings.Contains(out, "Whitespace:") {
					t.Errorf("a lexical change must not be counted as Whitespace:\n%s", out)
				}
			} else {
				if !strings.Contains(out, "Whitespace: 1") {
					t.Errorf("pure formatting must increment Whitespace:\n%s", out)
				}
				if strings.Contains(out, "Changed:   1") {
					t.Errorf("pure formatting must not increment Changed:\n%s", out)
				}
			}
		})
	}
}

// TestTextReport_NewContentNewBlankRendering: the report renders both raw
// forms of an ignore-all-space context line (old via Content, new via
// NewContent) and never presents the old text as the new text; a blank new
// raw line (NewBlank) contributes nothing beyond the old form (F14).
func TestTextReport_NewContentNewBlankRendering(t *testing.T) {
	oldSrc := "a = 1\n   \n"
	newSrc := "a=1\n\n"
	whole := diff.NewWithOpts(diff.Options{IgnoreSpace: true}).DiffFull(oldSrc, newSrc)

	// Sanity: the whole-file diff is a whitespace-only hunk whose second
	// line is a context line with an empty new raw form (NewBlank).
	if len(whole.Hunks) != 1 {
		t.Fatalf("expected one whitespace-only hunk, got %d", len(whole.Hunks))
	}
	if len(whole.Hunks[0].Lines) != 2 || !whole.Hunks[0].Lines[1].NewBlank {
		t.Fatalf("expected line 2 to carry NewBlank, got %+v", whole.Hunks[0].Lines)
	}

	result := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{{
		Old:        &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "old.txt", Source: oldSrc},
		New:        &ir.SemanticBlock{Kind: ir.KindUnknown, Name: "new.txt", Source: newSrc},
		InnerDiff:  whole,
		Confidence: 1.0,
		MatchType:  ir.MatchSimilarity,
	}}}
	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, result, "old.txt", "new.txt"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	t.Logf("report:\n%s", out)

	// Both raw forms of line 1 are rendered as context lines (the old text
	// is never presented as the new text).
	if !strings.Contains(out, "   a = 1") || !strings.Contains(out, "   a=1") {
		t.Errorf("both raw forms of the changed line must render:\n%s", out)
	}
	// The old whitespace-only line renders once as context; its blank new
	// counterpart adds nothing.
	if !strings.Contains(out, "      \n") {
		t.Errorf("the old whitespace-only line must render:\n%s", out)
	}
	// No line may carry a + / - prefix for the normalized-equal pair.
	if strings.Contains(out, "+ a = 1") || strings.Contains(out, "+   ") ||
		strings.Contains(out, "- a = 1") {
		t.Errorf("whitespace-only differences must render as context, not +/-:\n%s", out)
	}
}

func TestTextReport_ParseErrorsRow(t *testing.T) {
	// A result carrying parse-error counts (Issue D) surfaces a numeric
	// "Parse Errors: N (old: X, new: Y)" row in the summary so readers know
	// the input was not fully parsed and the semantic extraction is partial.
	withErrors := &ir.CorrelationResult{
		OldParseErrors: 0,
		NewParseErrors: 3,
		Pairs:          []ir.CorrelatedPair{},
	}
	r := &TextReporter{Opts: Options{NoColor: true}}
	var buf bytes.Buffer
	if err := r.Write(&buf, withErrors, "old.tsx", "new.tsx"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	t.Logf("report with parse errors:\n%s", out)
	if !strings.Contains(out, "Parse Errors:") {
		t.Error("expected a 'Parse Errors:' row when counts are non-zero")
	}
	if !strings.Contains(out, "Parse Errors: 3 (old: 0, new: 3)") {
		t.Errorf("row text mismatch; got:\n%s", out)
	}

	// A clean result (both counts zero) must NOT emit the row, so the
	// indicator only appears when there is actually something to flag.
	clean := &ir.CorrelationResult{Pairs: []ir.CorrelatedPair{}}
	var buf2 bytes.Buffer
	if err := r.Write(&buf2, clean, "old.tsx", "new.tsx"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf2.String(), "Parse Errors:") {
		t.Errorf("clean result should not include a Parse Errors row:\n%s", buf2.String())
	}
}
