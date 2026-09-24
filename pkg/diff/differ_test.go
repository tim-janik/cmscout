// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package diff

import (
	"fmt"
	"strings"
	"testing"

	"cmdiff/pkg/ir"
)

func TestDiff_NoChanges(t *testing.T) {
	d := New()
	result := d.Diff("line1\nline2\nline3", "line1\nline2\nline3")

	if result.HasChanges() {
		t.Error("diff of identical source should have no changes")
	}
	if len(result.Hunks) != 0 {
		t.Errorf("expected 0 hunks, got %d", len(result.Hunks))
	}
}

func TestDiff_AddLine(t *testing.T) {
	d := New()
	result := d.Diff("line1\nline3", "line1\nline2\nline3")

	if !result.HasChanges() {
		t.Error("diff should detect changes")
	}

	foundAdded := false
	for _, hunk := range result.Hunks {
		for _, line := range hunk.Lines {
			if line.Content == "line2" && line.Type == ir.DiffLineAdded {
				foundAdded = true
			}
		}
	}
	if !foundAdded {
		t.Error("expected 'line2' to be added")
	}
}

func TestDiff_RemoveLine(t *testing.T) {
	d := New()
	result := d.Diff("line1\nline2\nline3", "line1\nline3")

	if !result.HasChanges() {
		t.Error("diff should detect changes")
	}

	foundRemoved := false
	for _, hunk := range result.Hunks {
		for _, line := range hunk.Lines {
			if line.Content == "line2" && line.Type == ir.DiffLineRemoved {
				foundRemoved = true
			}
		}
	}
	if !foundRemoved {
		t.Error("expected 'line2' to be removed")
	}
}

func TestDiff_ReplaceLine(t *testing.T) {
	d := New()
	result := d.Diff("line1\nold\nline3", "line1\nnew\nline3")

	if !result.HasChanges() {
		t.Error("diff should detect changes")
	}

	foundOld := false
	foundNew := false
	for _, hunk := range result.Hunks {
		for _, line := range hunk.Lines {
			if line.Content == "old" && line.Type == ir.DiffLineRemoved {
				foundOld = true
			}
			if line.Content == "new" && line.Type == ir.DiffLineAdded {
				foundNew = true
			}
		}
	}
	if !foundOld {
		t.Error("expected 'old' to be removed")
	}
	if !foundNew {
		t.Error("expected 'new' to be added")
	}
}

func TestDiff_EmptyToNonEmpty(t *testing.T) {
	d := New()
	result := d.Diff("", "line1\nline2")

	if !result.HasChanges() {
		t.Error("diff should detect changes")
	}
}

func TestDiff_NonEmptyToEmpty(t *testing.T) {
	d := New()
	result := d.Diff("line1\nline2", "")

	if !result.HasChanges() {
		t.Error("diff should detect changes")
	}
}

func TestDiff_EmptyBoth(t *testing.T) {
	d := New()
	result := d.Diff("", "")

	if result.HasChanges() {
		t.Error("diff of empty strings should have no changes")
	}
}

func TestDiff_HunkMetadata(t *testing.T) {
	d := New()
	oldSrc := "a\nb\nc\nd\ne\nf\ng"
	newSrc := "a\nb\nX\nd\ne\nf\ng"
	result := d.Diff(oldSrc, newSrc)

	if len(result.Hunks) == 0 {
		t.Fatal("expected at least 1 hunk")
	}

	hunk := result.Hunks[0]
	if hunk.OldStart <= 0 {
		t.Errorf("OldStart should be > 0, got %d", hunk.OldStart)
	}
	if hunk.NewStart <= 0 {
		t.Errorf("NewStart should be > 0, got %d", hunk.NewStart)
	}
}

func TestDiff_FunctionBody(t *testing.T) {
	d := New()
	oldFn := "function add(a, b) {\n  return a + b;\n}"
	newFn := "function add(a, b) {\n  return a + b + 1;\n}"
	result := d.Diff(oldFn, newFn)

	if !result.HasChanges() {
		t.Error("diff should detect changes")
	}

	t.Logf("old source: %q", oldFn)
	t.Logf("new source: %q", newFn)
	for _, hunk := range result.Hunks {
		t.Logf("hunk: old=%d-%d new=%d-%d", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
		for _, line := range hunk.Lines {
			t.Logf("  %c %q", lineRune(line.Type), line.Content)
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", []string{}},
		{"one", []string{"one"}},
		{"one\ntwo", []string{"one", "two"}},
		{"one\ntwo\n", []string{"one", "two"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		result := splitLines(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitLines(%q) = %v, want %v (len %d vs %d)",
				tt.input, result, tt.expected, len(result), len(tt.expected))
		}
	}
}

func lineRune(t ir.DiffLineType) rune {
	switch t {
	case ir.DiffLineAdded:
		return '+'
	case ir.DiffLineRemoved:
		return '-'
	default:
		return ' '
	}
}

func TestDiff_IgnoreAllSpace(t *testing.T) {
	// With ignore-space, lines that only differ in whitespace should
	// match and produce no changes.
	d := NewWithOpts(Options{IgnoreSpace: true})
	result := d.Diff("val = 1\nfoo = 2", "val=1\nfoo=2")
	if result.HasChanges() {
		t.Error("ignore-space should produce no changes")
	}

	// Without ignore-space, the same lines should show changes.
	d2 := New()
	result2 := d2.Diff("val = 1\nfoo = 2", "val=1\nfoo=2")
	if !result2.HasChanges() {
		t.Error("without ignore-space, whitespace-only changes should be detected")
	}
}

func TestSplitWords(t *testing.T) {
	result := splitWords("const val = 1")
	expected := []string{"const", " ", "val", " ", "=", " ", "1"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d words, got %d: %v", len(expected), len(result), result)
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("word %d: expected %q, got %q", i, expected[i], result[i])
		}
	}
	// Concatenating the tokens must reproduce the original line, so the
	// word-diff renderer always shows the reviewed file's text.
	if joined := strings.Join(result, ""); joined != "const val = 1" {
		t.Errorf("tokens must concatenate to the original line, got %q", joined)
	}
}

func TestNormalizeForCompare(t *testing.T) {
	if normalizeForCompare("  hello  ", false) != "  hello  " {
		t.Error("non-ignore mode should preserve whitespace")
	}
	if normalizeForCompare("  hello  ", true) != "hello" {
		t.Error("ignore mode should strip whitespace")
	}
}

func TestDiff_DistantChangesUseSeparateHunks(t *testing.T) {
	d := New()
	old := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve"
	new := "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\nELEVEN\ntwelve"

	result := d.Diff(old, new)
	if len(result.Hunks) != 2 {
		t.Fatalf("expected two distant hunks, got %d", len(result.Hunks))
	}
}

func TestDiffFull_IncludesUnchangedLinesOutsideHunks(t *testing.T) {
	d := New()
	result := d.DiffFull("a\nb\nc\nd\ne", "a\nB\nc\nd\ne")
	if len(result.Hunks) != 1 {
		t.Fatalf("expected one full-context hunk, got %d", len(result.Hunks))
	}
	if got := len(result.Hunks[0].Lines); got != 6 {
		t.Fatalf("expected all old/new lines in full-context hunk, got %d", got)
	}
}

// reconstructLines rebuilds the old and new line sequences from a diff's
// hunk lines. A context line represents one old line and one new line;
// under --ignore-all-space those raw forms may differ (DiffLine.NewContent
// carries the new side's raw text). Content-only assertions cannot detect a
// missing occurrence or an old line placed where the new line belongs, so
// tests must reconstruct both sequences separately.
func reconstructLines(result *ir.DiffResult) (old, new []string) {
	for _, h := range result.Hunks {
		for _, l := range h.Lines {
			switch l.Type {
			case ir.DiffLineRemoved:
				old = append(old, l.Content)
			case ir.DiffLineAdded:
				new = append(new, l.Content)
			case ir.DiffLineContext:
				old = append(old, l.Content)
				switch {
				case l.NewContent != "":
					new = append(new, l.NewContent)
				case l.NewBlank:
					new = append(new, "")
				default:
					new = append(new, l.Content)
				}
			}
		}
	}
	return old, new
}

func assertSequences(t *testing.T, result *ir.DiffResult, wantOld, wantNew []string) {
	t.Helper()
	old, new := reconstructLines(result)
	if len(old) != len(wantOld) {
		t.Errorf("old sequence: got %d lines %v, want %d lines %v", len(old), old, len(wantOld), wantOld)
	} else {
		for i := range wantOld {
			if old[i] != wantOld[i] {
				t.Errorf("old line %d: got %q, want %q (full old: %v)", i, old[i], wantOld[i], old)
			}
		}
	}
	if len(new) != len(wantNew) {
		t.Errorf("new sequence: got %d lines %v, want %d lines %v", len(new), new, len(wantNew), wantNew)
	} else {
		for i := range wantNew {
			if new[i] != wantNew[i] {
				t.Errorf("new line %d: got %q, want %q (full new: %v)", i, new[i], wantNew[i], new)
			}
		}
	}
}

// TestWordDiff_ReplacementLine: word-diff must apply to ordinary line
// replacements, not only to context lines hidden by whitespace
// normalization. The paired added line carries the word-level diff with
// both removed and added words present.
func TestWordDiff_ReplacementLine(t *testing.T) {
	d := NewWithOpts(Options{WordDiff: true})
	result := d.Diff("const x = 1;", "const y = 2;")

	if !result.HasChanges() {
		t.Fatal("replacement must be a change")
	}
	found := false
	for _, h := range result.Hunks {
		for _, l := range h.Lines {
			if l.Type != ir.DiffLineAdded || !l.IsWordDiff {
				continue
			}
			found = true
			var removed, added bool
			for _, w := range l.Words {
				if w.Type == ir.DiffWordRemoved && w.Text == "x" {
					removed = true
				}
				if w.Type == ir.DiffWordAdded && w.Text == "y" {
					added = true
				}
			}
			if !removed || !added {
				t.Errorf("word diff on replacement must contain removed word 'x' and added word 'y', got %+v", l.Words)
			}
		}
	}
	if !found {
		t.Error("replacement line was not word-diffed")
	}
}

// TestWordDiff_ReplacementSequence: word-diff mode must still reconstruct
// the exact old and new line sequences (raw removed and added lines are
// preserved alongside the word annotation).
func TestWordDiff_ReplacementSequence(t *testing.T) {
	d := NewWithOpts(Options{WordDiff: true})
	result := d.Diff("a\nconst x = 1;\nz", "a\nconst y = 2;\nz")
	assertSequences(t, result, []string{"a", "const x = 1;", "z"}, []string{"a", "const y = 2;", "z"})
}

// TestIgnoreSpace_BothRawForms: under --ignore-all-space a line that
// compares equal only after whitespace stripping is a context line whose
// raw old and raw new texts BOTH survive in the diff (via NewContent), so
// the old text is never silently presented as the new text.
func TestIgnoreSpace_BothRawForms(t *testing.T) {
	d := NewWithOpts(Options{IgnoreSpace: true})
	result := d.Diff("val = 1\nfoo = 2", "val=1\nfoo = 3")
	assertSequences(t, result, []string{"val = 1", "foo = 2"}, []string{"val=1", "foo = 3"})

	// The whitespace-only change must NOT count as a structural change.
	// The real change (foo = 2 → foo = 3) is still detected.
	if !result.HasChanges() {
		t.Error("the real change must still be detected")
	}
}

// TestIgnoreSpace_WhitespaceOnlyNoChanges: a file that differs only by
// whitespace has no structural changes under --ignore-all-space, but both
// raw forms remain recoverable.
func TestIgnoreSpace_WhitespaceOnlyNoChanges(t *testing.T) {
	d := NewWithOpts(Options{IgnoreSpace: true})
	result := d.Diff("val = 1", "val=1")
	if result.HasChanges() {
		t.Error("whitespace-only differences must not count as changes")
	}
	assertSequences(t, result, []string{"val = 1"}, []string{"val=1"})
}

// TestDiff_ReconstructSequences: ordinary diffs reconstruct exactly, with
// every occurrence preserved (repeated lines are never collapsed).
// TestIgnoreSpace_BlankNewRawLine: a whitespace-only line that became
// blank on the new side is a context line whose NewContent is empty — the
// empty string is the TRUE new raw text, not "identical to the old".
// DiffLine.NewBlank keeps reconstruction exact: the old text must never
// be substituted for the blank new line.
func TestIgnoreSpace_BlankNewRawLine(t *testing.T) {
	d := NewWithOpts(Options{IgnoreSpace: true})
	result := d.Diff("  \nb", "\nb")
	assertSequences(t, result, []string{"  ", "b"}, []string{"", "b"})

	// The same shape inside an otherwise changed file (LCS path, not the
	// whitespace-only shortcut) must also reconstruct exactly.
	result = d.Diff("  \nkeep\nc", "\nkeep\nCHANGED")
	assertSequences(t, result, []string{"  ", "keep", "c"}, []string{"", "keep", "CHANGED"})
}

// TestDiff_LargeInputStaysAligned: with the coarse fallback removed, a
// diff whose LCS table exceeds the historical 4-million-cell bound must
// still produce a properly aligned diff — common lines as context, the
// real change as removed+added — never an unaligned all-removed/
// all-added coarse hunk.
func TestDiff_LargeInputStaysAligned(t *testing.T) {
	const n = 2100 // 2100×2100 = 4.41M cells, above the old maxLCSCells cap
	old := make([]string, n)
	new := make([]string, n)
	for i := 0; i < n; i++ {
		line := fmt.Sprintf("line %04d", i)
		old[i] = line
		new[i] = line
	}
	new[1234] = "line CHANGED"

	d := New()
	result := d.DiffFull(strings.Join(old, "\n"), strings.Join(new, "\n"))
	if !result.HasChanges() {
		t.Fatal("large diff with one changed line must report changes")
	}
	// Full reconstruction: every old and new line, exactly, in order.
	assertSequences(t, result, old, new)
	// Alignment: common lines must appear as context, not as a coarse
	// removed/added dump of the whole file.
	context := 0
	removed := 0
	added := 0
	for _, h := range result.Hunks {
		for _, l := range h.Lines {
			switch l.Type {
			case ir.DiffLineContext:
				context++
			case ir.DiffLineRemoved:
				removed++
			case ir.DiffLineAdded:
				added++
			}
		}
	}
	if context == 0 {
		t.Errorf("large diff must keep common-line alignment (no coarse fallback); removed=%d added=%d", removed, added)
	}
	if removed != 1 || added != 1 {
		t.Errorf("exactly one line changed: removed=%d added=%d, want 1/1", removed, added)
	}

	// Word-diff and ignore-space combinations on the same large input stay
	// correct and reconstructable.
	for _, opts := range []Options{{WordDiff: true}, {IgnoreSpace: true}, {WordDiff: true, IgnoreSpace: true}} {
		dw := NewWithOpts(opts)
		res := dw.DiffFull(strings.Join(old, "\n"), strings.Join(new, "\n"))
		if !res.HasChanges() {
			t.Errorf("opts %+v: large diff must report changes", opts)
		}
		assertSequences(t, res, old, new)
	}
}

func TestDiff_ReconstructSequences(t *testing.T) {
	d := New()
	result := d.Diff("a\nb\nb\nc", "a\nb\nc\nc")
	assertSequences(t, result, []string{"a", "b", "b", "c"}, []string{"a", "b", "c", "c"})
}
