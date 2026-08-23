// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

// Invariant tests for the coverage guarantee of the `cmdiff` semantic
// report and its `--simple-diff` mode.
//
// Invariant 1 (coverage — the hard requirement): for ANY pair of input
// files — regardless of whether tree-sitter knows the language — every
// non-empty line of both inputs must appear in the command's output. Comment
// indentation may be normalized by the deliberate span-owned comment
// de-duplication rule, but comment content is retained. No line may be
// silently dropped by the parser, extractor, matcher, or report layer.
//
// Invariant 2 (de-duplication): each non-empty line should appear only
// once; duplicates must at least be minimized. The raw `--simple-diff`
// output shows every line exactly once (unique lines are never repeated),
// and the semantic report's whole-file coverage supplement never repeats a
// line already shown by the semantic sections.
//
// The cases cover: combinations of known languages (same and mixed
// extensions), exactly one input file of a definitely-unknown language,
// two input files of definitely-unknown languages, and single-input
// (added/deleted file) shapes with an unknown language.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"cmdiff/pkg/lang"
	"cmdiff/pkg/parser"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// Known-language fixtures deliberately mix: unchanged blocks, changed
// blocks, added/removed blocks, nested blocks (methods inside a class), and
// top-level statements the extractor does not emit as blocks (the coverage
// supplement must surface those).
const tsOld = `import { foo } from './foo';
const a = 1;
const shared = 42;
setup();
function helper(x) {
  return x * 2;
}
class Widget {
  render() {
    return 'old';
  }
  static describe() {
    return 'widget';
  }
}
export function exportedUtil() {
  return 'exp';
}
`

const tsNew = `import { foo } from './foo';
const a = 2;
const shared = 42;
setup(1);
function helper(x) {
  return x * 3;
}
function added() {
  return 'new';
}
class Widget {
  render() {
    return 'new';
  }
  static describe() {
    return 'widget';
  }
}
export function exportedUtil() {
  return 'exp';
}
`

const tsxOld = `import React from 'react';
const App = () => <div>hello</div>;
function useCounter() {
  return { count: 0 };
}
`

const tsxNew = `import React from 'react';
const App = () => <div>bye</div>;
function useCounter() {
  return { count: 1 };
}
`

const goOld = `package main

import "fmt"

const greeting = "hi"

func main() {
	fmt.Println(greeting)
}
`

const goNew = `package main

import "fmt"

const greeting = "hello"

func main() {
	fmt.Println(greeting)
	fmt.Println("done")
}
`

const shOld = `#!/bin/bash
NAME="world"
echo "hello $NAME"
function greet() {
  echo "hi $NAME"
}
greet
`

const shNew = `#!/bin/bash
NAME="world"
echo "hello $NAME!"
function greet() {
  echo "hi $NAME"
}
greet
`

const jsOld = `const port = 8080;
function start() {
  return port;
}
start();
`

const jsNew = `const port = 9090;
function start() {
  return port;
}
start();
`

// C fixture covers semantic blocks and an unextracted top-level call.
const cOld = `#include <stdio.h>
#define MAX 100
#define ADD(a, b) ((a) + (b))
typedef struct Point { int x; int y; } Point;
int counter = 0;
int greet(void) {
  return MAX;
}
int add_real(int a, int b) {
  return a + b;
}
setup();
`

const cNew = `#include <stdio.h>
#define MAX 200
#define ADD(a, b) ((a) - (b))
typedef struct Point { int x; int y; } Point;
int counter = 1;
int greet(void) {
  return MAX;
}
int added(void) {
  return 0;
}
setup(1);
`

// C++ fixture covers namespaces, classes, macros, and an unextracted call.
const cppOld = `#include <iostream>
#define MAX 100
#define ADD(a, b) ((a) + (b))
namespace app {
class Widget {
 public:
  Widget() {}
  ~Widget() {}
  int value() const { return 0; }
};
int greet(int x) {
  return x;
}
}
run();
`

const cppNew = `#include <iostream>
#define MAX 200
#define ADD(a, b) ((a) - (b))
namespace app {
class Widget {
 public:
  Widget() {}
  ~Widget() {}
  int value() const { return 1; }
};
int greet(int x) {
  return x;
}
int added(int x) {
  return x;
}
}
run(1);
`

// Definitely-unknown languages: extensions not recognized by lang.Detect,
// with contents that are not valid in any supported grammar.
const xyzOld = `syntax unknown 1
key = value one
[section]
item.a = 1
item.b = 2
`

const xyzNew = `syntax unknown 1
key = value two
[section]
item.a = 1
item.b = 3
`

const quuxSrc = `QUUX-BEGIN
widget color: red
widget size: 10
QUUX-END
`

type invCase struct {
	name   string
	old    string // old file name (extension drives language detection)
	oldSrc string
	new    string
	newSrc string
}

var invariantCases = []invCase{
	// Combinations of known languages (same and mixed).
	{"ts/ts", "old.ts", tsOld, "new.ts", tsNew},
	{"ts/ts identical", "old.ts", tsOld, "same.ts", tsOld},
	{"tsx/tsx", "old.tsx", tsxOld, "new.tsx", tsxNew},
	{"ts/tsx", "old.ts", tsOld, "new.tsx", tsxNew},
	{"go/go", "old.go", goOld, "new.go", goNew},
	{"go/go identical", "old.go", goOld, "same.go", goOld},
	{"sh/sh", "old.sh", shOld, "new.sh", shNew},
	{"js/js", "old.js", jsOld, "new.js", jsNew},
	{"ts/js", "old.ts", tsOld, "new.js", jsNew},
	{"ts/go", "old.ts", tsOld, "new.go", goNew},
	{"go/sh", "old.go", goOld, "new.sh", shNew},

	// C/C++ grammar pairs, cross-language pairs, and fallback cases.
	{"c/c", "old.c", cOld, "new.c", cNew},
	{"c/c identical", "old.c", cOld, "same.c", cOld},
	{"cpp/cpp", "old.cpp", cppOld, "new.cpp", cppNew},
	{"cpp/cpp identical", "old.cpp", cppOld, "same.cpp", cppOld},
	{"c/cpp", "old.c", cOld, "new.cpp", cppNew},
	{"c + unknown", "old.c", cOld, "new.xyz", xyzNew},
	{"cpp + unknown", "old.cpp", cppOld, "new.xyz", xyzNew},
	{"c → go", "old.c", cOld, "new.go", goOld},

	// Exactly one input file is a definitely-unknown language.
	{"ts + unknown", "old.ts", tsOld, "new.xyz", xyzNew},
	{"unknown + ts", "old.xyz", xyzOld, "new.ts", tsNew},
	{"go + unknown", "old.go", goOld, "new.xyz", xyzNew},

	// Both input files are definitely-unknown languages.
	{"unknown/unknown", "old.xyz", xyzOld, "new.xyz", xyzNew},
	{"unknown/unknown identical", "old.xyz", xyzOld, "same.xyz", xyzOld},
	{"unknown/unknown different exts", "old.xyz", xyzOld, "new.quux", quuxSrc},

	// A single real input file of an unknown language (the other side is
	// empty): added-file and deleted-file shapes.
	{"added unknown (empty old)", "old.xyz", "", "new.xyz", xyzNew},
	{"deleted unknown (empty new)", "old.xyz", xyzOld, "new.xyz", ""},

	// Supported-language edge shapes: empty inputs, one-sided inputs, and
	// malformed (parse-error) inputs. Coverage must hold in every case.
	{"ts added (empty old)", "old.ts", "", "new.ts", tsNew},
	{"ts deleted (empty new)", "old.ts", tsOld, "new.ts", ""},
	{"ts both empty", "old.ts", "", "same.ts", ""},
	{"ts malformed", "old.ts", "function ( {\nconst = 1;\n", "new.ts", "function ( {\nconst = 2;\n"},
	{"ts export clause", "old.ts", "export { a, b };\nconst x = 1;\n", "new.ts", "export { a, b, c };\nconst x = 2;\n"},
	{"ts changed comment", "old.ts", "// handle the pointer\nfunction f() { return 1; }\n", "new.ts", "// handle the pointer event\nfunction f() { return 1; }\n"},
	{"ts whitespace-only", "old.ts", "const a = 1;\n", "new.ts", "  const a = 1;\n"},
	{"ts anonymous arrows", "old.ts", "function f() {\n  const cb = () => 1;\n  return cb;\n}\n", "new.ts", "function f() {\n  const cb = () => 2;\n  return cb;\n}\n"},
	{"ts reordered dup methods", "old.ts", "class A {\n  foo() { return 1; }\n  bar() { return 10; }\n}\nclass B {\n  foo() { return 2; }\n}\n", "new.ts", "class B {\n  foo() { return 2; }\n}\nclass A {\n  foo() { return 1; }\n  bar() { return 11; }\n}\n"},
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// skipIfNoParser skips the test when the vendored tree-sitter grammars are
// unavailable (e.g. CGO disabled), mirroring the other parser-dependent tests.
func skipIfNoParser(t *testing.T) {
	t.Helper()
	p, err := parser.New(lang.Language{Name: "ts", Ext: ".ts"})
	if err != nil {
		t.Skipf("tree-sitter parser not available: %v", err)
	}
	p.Close()
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runTool invokes the cmdiff run() entry point and returns stdout.
func runTool(t *testing.T, args ...string) string {
	t.Helper()
	var out, errBuf bytes.Buffer
	if err := run(args, strings.NewReader(""), &out, &errBuf); err != nil {
		t.Fatalf("run %v failed: %v\nstderr:\n%s", args, err, errBuf.String())
	}
	return out.String()
}

type contentLine struct {
	typ     string // "added", "removed", or "context"
	content string
}

// extractContentLines returns non-empty +, -, and context rows from a report.
// Headers and summary rows are ignored.
func extractContentLines(output string) []contentLine {
	var out []contentLine
	for _, line := range strings.Split(output, "\n") {
		if cl := contentLineOf(line); cl != nil {
			out = append(out, *cl)
		}
	}
	return out
}

// nonEmptyLines returns the non-empty lines of a source string.
func nonEmptyLines(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// splitSections splits a report into sections keyed by their header line
// (kind headers, "Other", "Summary", the "diff --cmdiff" header).
func splitSections(output string) []section {
	var out []section
	current := -1
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		// Section headers are non-content lines that start a section.
		if isSectionHeader(line) {
			out = append(out, section{header: line})
			current = len(out) - 1
			continue
		}
		cl := contentLineOf(line)
		if cl == nil || current < 0 {
			continue
		}
		out[current].lines = append(out[current].lines, *cl)
	}
	return out
}

// section is one header plus its content lines.
type section struct {
	header string
	lines  []contentLine
}

// isSectionHeader reports whether a line starts a new section in a report
// (kind headers, "Summary", the "diff --cmdiff" header, "Other", etc.).
// Content lines always start with "+", "-", or " ".
func isSectionHeader(line string) bool {
	return contentLineOf(line) == nil
}

// contentLineOf parses one output line as a content line, or nil.
func contentLineOf(line string) *contentLine {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(line, "  ") && isSummaryRow(trimmed) {
		return nil
	}
	var typ, content string
	switch {
	case strings.HasPrefix(line, "+"):
		typ, content = "added", line[1:]
	case strings.HasPrefix(line, "-"):
		typ, content = "removed", line[1:]
	case strings.HasPrefix(line, " "):
		typ, content = "context", line[1:]
	default:
		return nil
	}
	if content == "" {
		return nil
	}
	return &contentLine{typ: typ, content: content}
}

func isSummaryRow(line string) bool {
	for _, prefix := range []string{
		"Matched:", "Unchanged:", "Changed:", "Renamed:", "Moved:",
		"Added:", "Removed:", "Whitespace:", "Parse Errors:",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Invariant tests
// ---------------------------------------------------------------------------

// TestCoverageInvariant verifies the hard invariant for both commands: every
// non-empty line of the old and new inputs appears in the command output,
// for every language combination (known, one-unknown, two-unknown).
func TestCoverageInvariant(t *testing.T) {
	skipIfNoParser(t)

	for _, tc := range invariantCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			oldPath := writeFile(t, dir, tc.old, tc.oldSrc)
			newPath := writeFile(t, dir, tc.new, tc.newSrc)

			t.Run("simple-diff", func(t *testing.T) {
				out := runTool(t, "--simple-diff", "--no-color", oldPath, newPath)
				assertCoverage(t, "simple-diff", out, tc.oldSrc, tc.newSrc)
				assertDiffNoDuplication(t, out, tc.oldSrc, tc.newSrc)
			})

			t.Run("semantic", func(t *testing.T) {
				out := runTool(t, "--no-color", oldPath, newPath)
				assertCoverage(t, "semantic", out, tc.oldSrc, tc.newSrc)
				assertReviewDedupMinimized(t, out, tc.oldSrc, tc.newSrc)
			})
		})
	}
}

// headerPct matches a matched-pair header line and captures the displayed
// similarity percentage:
//
//	"@@ -63,186 +64,118 @@  spin_drag_pointermove  100% similarity  [moved]"
var headerPct = regexp.MustCompile(`^@@ -[0-9]+,[0-9]+ \+[0-9]+,[0-9]+ @@  .*  ([0-9]+)% similarity(  .*)?$`)

// blockMarker matches a standalone added or removed block header.
var blockMarker = regexp.MustCompile(`^@@ -[0-9]+,[0-9]+ \+[0-9]+,[0-9]+ @@  .*  \[(added|removed)\]$`)

// assertHeaderSimilarityMatchesDiff checks that displayed percentages match the rendered diff.
func assertHeaderSimilarityMatchesDiff(t *testing.T, output string) {
	t.Helper()
	pct := -1
	whitespaceTagged := false
	inPair := false
	for _, line := range strings.Split(output, "\n") {
		if m := headerPct.FindStringSubmatch(line); m != nil {
			p, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("bad header percentage %q", m[1])
			}
			pct = p
			whitespaceTagged = strings.Contains(line, "[whitespace]")
			inPair = true
			continue
		}
		// Standalone added/removed blocks have no header; their lines must
		// not be attributed to the previous pair.
		if blockMarker.MatchString(line) {
			inPair = false
			continue
		}
		if !inPair {
			continue
		}
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			if pct == 100 && !whitespaceTagged {
				t.Errorf("pair header claims 100%% similarity but its diff shows a change:\n%s\noutput:\n%s", line, output)
			}
		}
		// A pair's diff is contiguous: a blank line or the next header ends it.
		if line == "" {
			inPair = false
		}
	}
}

// TestHeaderSimilarityMatchesDiff checks percentages across every fixture.
func TestHeaderSimilarityMatchesDiff(t *testing.T) {
	skipIfNoParser(t)

	for _, tc := range invariantCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			oldPath := writeFile(t, dir, tc.old, tc.oldSrc)
			newPath := writeFile(t, dir, tc.new, tc.newSrc)

			out := runTool(t, "--no-color", oldPath, newPath)
			assertHeaderSimilarityMatchesDiff(t, out)
		})
	}
}

// TestRepeatedLinesNotCollapsed keeps repeated source lines in each snippet.
func TestRepeatedLinesNotCollapsed(t *testing.T) {
	skipIfNoParser(t)

	// A block with two identical lines. When rendered unchanged, both
	// occurrences must appear.
	old := "function outer() {\n  foo();\n  foo();\n  return 1;\n}\n"
	new := old

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	if n := strings.Count(out, "foo();"); n != 2 {
		t.Errorf("review: unchanged block with two identical lines rendered %d occurrences, want 2\noutput:\n%s", n, out)
	}

	out = runTool(t, "--simple-diff", "--no-color", oldPath, newPath)
	if n := strings.Count(out, "foo();"); n != 2 {
		t.Errorf("diff: identical files with repeated line rendered %d occurrences, want 2\noutput:\n%s", n, out)
	}
}

// TestSupplementRepeatedLines keeps repeated lines in the coverage supplement.
func TestSupplementRepeatedLines(t *testing.T) {
	skipIfNoParser(t)

	// Top-level calls are not extracted as blocks; the coverage supplement
	// (raw whole-file diff) must show both removed occurrences of foo();.
	old := "const a = 1;\nfoo();\nfoo();\nfoo();\n"
	new := "const a = 2;\nfoo();\n"

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	// LCS matches one foo(); as context, the other two are removed: the
	// supplement must show all three occurrences (1 context + 2 removed).
	if n := strings.Count(out, "foo();"); n != 3 {
		t.Errorf("review: raw-diff supplement rendered %d foo(); occurrences, want 3 (1 context + 2 removed)\noutput:\n%s", n, out)
	}
}

// TestSkipUnchangedFlag checks suppression, changed output, and summary counts.
func TestSkipUnchangedFlag(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", tsOld)
	newPath := writeFile(t, dir, "new.ts", tsNew)

	// Default mode renders unchanged blocks as context lines (coverage).
	out := runTool(t, "--no-color", oldPath, newPath)
	for _, want := range []string{"const shared = 42;", "export function exportedUtil() {", "  return 'exp';"} {
		if !strings.Contains(out, want) {
			t.Errorf("default: expected %q in output\n%s", want, out)
		}
	}

	// --skip-unchanged suppresses unchanged blocks entirely.
	out = runTool(t, "--no-color", "--skip-unchanged", oldPath, newPath)
	for _, gone := range []string{"const shared = 42;", "export function exportedUtil", "return 'exp'", "import { foo } from './foo';"} {
		if strings.Contains(out, gone) {
			t.Errorf("--skip-unchanged: %q should be suppressed entirely\n%s", gone, out)
		}
	}
	// Changed content still appears.
	for _, want := range []string{"a = 2", "const a = 2;", "helper", "function added()"} {
		if !strings.Contains(out, want) {
			t.Errorf("--skip-unchanged: expected changed content %q in output\n%s", want, out)
		}
	}
	// The summary still counts unchanged blocks and the changed method separately.
	if !strings.Contains(out, "Unchanged: 5") {
		t.Errorf("--skip-unchanged: summary should still count unchanged blocks\n%s", out)
	}
}

// assertCoverage checks side-aware, occurrence-aware coverage for both inputs.
func assertCoverage(t *testing.T, cmd, output, oldSrc, newSrc string) {
	t.Helper()
	oldRendered := map[string]int{}
	newRendered := map[string]int{}
	for _, cl := range extractContentLines(output) {
		switch cl.typ {
		case "removed":
			oldRendered[cl.content]++
		case "added":
			newRendered[cl.content]++
		case "context":
			oldRendered[cl.content]++
			newRendered[cl.content]++
		}
	}
	assertSide := func(side, src string, rendered map[string]int) {
		counts := map[string]int{}
		for _, line := range nonEmptyLines(src) {
			counts[line]++
		}
		for line, want := range counts {
			if got := rendered[line]; got < want {
				t.Errorf("%s: %s input line %q occurs %d time(s) but is rendered %d time(s) on the %s side\noutput:\n%s",
					cmd, side, line, want, got, side, output)
			}
		}
	}
	assertSide("old", oldSrc, oldRendered)
	assertSide("new", newSrc, newRendered)
}

// TestNestedRenameDeepStaysMatched keeps a grandchild through a parent rename.
func TestNestedRenameDeepStaysMatched(t *testing.T) {
	skipIfNoParser(t)

	old := `function outer() {
  function inner() {
    function deep() { return 1; }
    return deep;
  }
  return inner;
}
`
	new := `function outer() {
  function renamedInner() {
    function deep() { return 1; }
    return deep;
  }
  return inner;
}
`

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	// deep stays a matched, unchanged pair at 100%.
	if !strings.Contains(out, "deep  100% similarity") {
		t.Errorf("deep must remain a matched unchanged block:\n%s", out)
	}
	// deep is never an added/removed block.
	for _, marker := range []string{"deep  [added]", "deep  [removed]"} {
		if strings.Contains(out, marker) {
			t.Errorf("deep must not be %s:\n%s", marker, out)
		}
	}
	// The rename is visible as a matched rename pair.
	if !strings.Contains(out, "inner → renamedInner") {
		t.Errorf("inner → renamedInner must be a matched rename:\n%s", out)
	}
}

// TestNestedJSXAfterOuterRename: an unchanged nested JSX element must stay
// matched when its outer JSX (and its component) is renamed.
func TestNestedJSXAfterOuterRename(t *testing.T) {
	skipIfNoParser(t)

	old := "const App = () => <div><span>hello</span></div>;\n"
	new := "const App2 = () => <section><span>hello</span></section>;\n"

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.tsx", old)
	newPath := writeFile(t, dir, "new.tsx", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	// The nested span is matched unchanged; the outer element and the
	// component are matched renames.
	if !strings.Contains(out, "span  100% similarity") {
		t.Errorf("nested span must remain a matched unchanged block:\n%s", out)
	}
	for _, marker := range []string{"span  [added]", "span  [removed]"} {
		if strings.Contains(out, marker) {
			t.Errorf("nested span must not be %s:\n%s", marker, out)
		}
	}
	if !strings.Contains(out, "App → App2") {
		t.Errorf("component rename must be visible:\n%s", out)
	}
}

// TestNestedMultipleLevelsAddRemove checks deep additions and removals.
func TestNestedMultipleLevelsAddRemove(t *testing.T) {
	skipIfNoParser(t)

	old := `function outer() {
  function a() {
    function keep() { return 1; }
    function gone() { return 1; }
  }
}
`
	new := `function outer() {
  function a2() {
    function keep() { return 1; }
    function added() { for (let i = 0; i < n; i++) { collect(i); } }
  }
}
`

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	if !strings.Contains(out, "keep  100% similarity") {
		t.Errorf("keep must remain a matched unchanged block through the renamed level:\n%s", out)
	}
	if !strings.Contains(out, "gone  [removed]") {
		t.Errorf("gone must be a removed block:\n%s", out)
	}
	if !strings.Contains(out, "added  [added]") {
		t.Errorf("added must be an added block:\n%s", out)
	}
}

// TestReorderedClassesWithDuplicateMethods checks scope-aware reordering.
func TestReorderedClassesWithDuplicateMethods(t *testing.T) {
	skipIfNoParser(t)

	old := `class A {
  foo() { return 1; }
  bar() { return 10; }
}
class B {
  foo() { return 2; }
}
`
	new := `class B {
  foo() { return 2; }
}
class A {
  foo() { return 1; }
  bar() { return 11; }
}
`

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	fooHeaders := 0
	for _, line := range strings.Split(out, "\n") {
		if m := headerPct.FindStringSubmatch(line); m != nil && strings.Contains(line, "foo  ") {
			fooHeaders++
			if m[1] != "100" {
				t.Errorf("foo must pair within its own class (100%%), got %s%%: %s", m[1], line)
			}
		}
	}
	if fooHeaders != 2 {
		t.Errorf("expected exactly 2 matched foo headers (A.foo and B.foo), got %d\n%s", fooHeaders, out)
	}
	// The real change (bar 10 → 11) must still be visible below 100%.
	barHeader := false
	barAt100 := false
	for _, line := range strings.Split(out, "\n") {
		if headerPct.MatchString(line) && strings.Contains(line, "bar  ") {
			barHeader = true
			if strings.Contains(line, "bar  100% similarity") {
				barAt100 = true
			}
		}
	}
	if !barHeader || barAt100 {
		t.Errorf("A.bar changed and must display below 100%%:\n%s", out)
	}
}

// TestAddedMethodNotRewrittenE2E keeps an added method's source inside its class.
func TestAddedMethodNotRewrittenE2E(t *testing.T) {
	skipIfNoParser(t)

	old := `class A {
  foo() { return 1; }
}
`
	new := `class A {
  foo() { return 1; }
}
class B {
  foo() { return 2; }
}
`

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	// The added B.foo method renders its real source inside the added class.
	if !strings.Contains(out, "+  foo() { return 2; }") {
		t.Errorf("B.foo must render its real source inline in class B:\n%s", out)
	}
	// Unchanged A.foo folds to a reference inside matched class A.
	if !strings.Contains(out, "// [matched: method foo]") {
		t.Errorf("unchanged A.foo must fold into a reference:\n%s", out)
	}
	// No standalone added-method block (the method is part of added class B).
	if strings.Contains(out, "foo  [added]") {
		t.Errorf("B.foo must not be listed standalone, it shows inside class B:\n%s", out)
	}
}

// TestRemovedMethodKeepsSourceE2E keeps a removed method's source inside its class.
func TestRemovedMethodKeepsSourceE2E(t *testing.T) {
	skipIfNoParser(t)

	old := `class A {
  foo() { return 1; }
}
class B {
  foo() { return 2; }
}
`
	new := `class A {
  foo() { return 1; }
}
`

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	// The removed B.foo method renders its real source inside the removed class.
	if !strings.Contains(out, "-  foo() { return 2; }") {
		t.Errorf("B.foo must render its real source inline in removed class B:\n%s", out)
	}
	// Unchanged A.foo folds to a reference inside matched class A.
	if !strings.Contains(out, "// [matched: method foo]") {
		t.Errorf("unchanged A.foo must fold into a reference:\n%s", out)
	}
	// No standalone removed-method block (the method is part of removed class B).
	if strings.Contains(out, "foo  [removed]") {
		t.Errorf("B.foo must not be listed standalone, it shows inside class B:\n%s", out)
	}
}

// TestNamespaceNotDiffedE2E keeps namespace members as separate components.
func TestNamespaceNotDiffedE2E(t *testing.T) {
	skipIfNoParser(t)

	old := `namespace app {
class Widget {
 public:
  int value() const { return 0; }
};
void greet() {}
}
`
	new := `namespace app {
class Widget {
 public:
  int value() const { return 0; }
};
void greet() {}
void delay() {
  auto p = [](int x) { return x; };
  consume(p);
}
}
`

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.cc", old)
	newPath := writeFile(t, dir, "new.cc", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	if strings.Contains(out, "\nNamespaces\n") {
		t.Errorf("namespaces must not be rendered as a component:\n%s", out)
	}
	// The added function is annotated with its namespace.
	if !strings.Contains(out, "delay  [in app]  [added]") {
		t.Errorf("added function must carry its namespace annotation, namespace after the name:\n%s", out)
	}
	// The function-local lambda stays inline; there is no Lambdas section.
	if strings.Contains(out, "\nLambdas\n") {
		t.Errorf("function-local lambda must not be a standalone block:\n%s", out)
	}
	if !strings.Contains(out, "[](int x) { return x; }") {
		t.Errorf("the lambda must render inline inside delay():\n%s", out)
	}
}

// TestWordDiffEndToEnd checks word markers in semantic and simple diffs.
func TestWordDiffEndToEnd(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", "const x = 1;\n")
	newPath := writeFile(t, dir, "new.ts", "const y = 2;\n")

	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{name: "semantic"},
		{name: "simple-diff", extra: []string{"--simple-diff"}},
	} {
		args := append(tc.extra, "--no-color", "--word-diff", oldPath, newPath)
		out := runTool(t, args...)
		if !strings.Contains(out, "~x~") {
			t.Errorf("%s --word-diff: removed word 'x' must be marked (got no ~x~):\n%s", tc.name, out)
		}
		if !strings.Contains(out, "~1~") {
			t.Errorf("%s --word-diff: removed word '1' must be marked (got no ~1~):\n%s", tc.name, out)
		}
		if !strings.Contains(out, "y") || !strings.Contains(out, "2") {
			t.Errorf("%s --word-diff: added words must be present:\n%s", tc.name, out)
		}
	}
}

// TestGoRawStringClassificationEndToEnd treats Go raw-string content as semantic.
func TestGoRawStringClassificationEndToEnd(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.go", "package p\nvar s = `a ${ x } b`\n")
	newPath := writeFile(t, dir, "new.go", "package p\nvar s = `a ${  x  } b`\n")
	out := runTool(t, "--no-color", oldPath, newPath)
	if !strings.Contains(out, "Changed:   1") || strings.Contains(out, "Whitespace:") {
		t.Errorf("Go raw-string content changes must be semantic, not whitespace-only:\n%s", out)
	}
}

// TestCppRawStringClassificationEndToEnd treats raw-string content as semantic.
func TestCppRawStringClassificationEndToEnd(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.cpp", "auto s = R\"(a b)\";\n")
	newPath := writeFile(t, dir, "new.cpp", "auto s = R\"(ab)\";\n")
	out := runTool(t, "--no-color", oldPath, newPath)
	if !strings.Contains(out, "Changed:   1") || strings.Contains(out, "Whitespace:") {
		t.Errorf("C++ raw-string content changes must be semantic, not whitespace-only:\n%s", out)
	}
}

// TestCIncludeTreatedAsImport: a #include is an import block and renders
// under the Imports section (not Other), with its line preserved.
func TestCIncludeTreatedAsImport(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.c", "#include <stdio.h>\nint main(void) { return 0; }\n")
	newPath := writeFile(t, dir, "new.c", "#include <stdio.h>\nint main(void) { return 1; }\n")
	out := runTool(t, "--no-color", oldPath, newPath)
	if !strings.Contains(out, "Imports") {
		t.Errorf("#include must render under the Imports section:\n%s", out)
	}
	if !strings.Contains(out, "#include <stdio.h>") {
		t.Errorf("the #include line must appear in the report:\n%s", out)
	}
	if !strings.Contains(out, "@@ -1,1 +1,1 @@  <stdio.h>") {
		t.Errorf("a one-line include must have a one-line hunk range:\n%s", out)
	}
}

func TestHeaderChoosesCOrCppGrammarByParseQuality(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	pureCOld := writeFile(t, dir, "old.h", "typeof(int) value;\nint c_function(int);\n")
	pureCNew := writeFile(t, dir, "new.h", "typeof(int) value;\nint c_function(int);\n")
	out := runTool(t, "--no-color", pureCOld, pureCNew)
	if strings.Contains(out, "Parse Errors:") {
		t.Errorf("pure-C .h input should select the C grammar:\n%s", out)
	}
	if !strings.Contains(out, "Variables") || !strings.Contains(out, "c_function") {
		t.Errorf("pure-C declarations should be extracted after selecting C:\n%s", out)
	}

	cppOld := writeFile(t, dir, "cpp-old.h", "class Widget { public: void render(); };\n")
	cppNew := writeFile(t, dir, "cpp-new.h", "class Widget { public: void render(); };\n")
	out = runTool(t, "--no-color", cppOld, cppNew)
	if strings.Contains(out, "Parse Errors:") {
		t.Errorf("C++ .h input should keep the C++ grammar:\n%s", out)
	}
	if !strings.Contains(out, "Methods") || !strings.Contains(out, "render") {
		t.Errorf("C++ method declarations should remain semantic methods:\n%s", out)
	}
}

// TestIgnoreAllSpaceEndToEnd preserves both raw forms of whitespace-only lines.
func TestIgnoreAllSpaceEndToEnd(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", "val = 1\nfoo = 2\n")
	newPath := writeFile(t, dir, "new.ts", "val=1\nfoo = 3\n")

	// --simple-diff: both raw forms of the whitespace-only line must appear.
	out := runTool(t, "--simple-diff", "--no-color", "--ignore-all-space", oldPath, newPath)
	for _, want := range []string{"val = 1", "val=1", "foo = 2", "foo = 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff --ignore-all-space: %q must be represented in the output:\n%s", want, out)
		}
	}

	// semantic mode: same requirement, plus the semantic pair renders both
	// raw forms.
	out = runTool(t, "--no-color", "--ignore-all-space", oldPath, newPath)
	for _, want := range []string{"val = 1", "val=1", "foo = 2", "foo = 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("review --ignore-all-space: %q must be represented in the output:\n%s", want, out)
		}
	}
}

// TestSkipUnchangedSameLineStatement: with --skip-unchanged, a changed
// unextracted statement that shares a physical line with an unchanged
// declaration must still be shown; the unchanged block must not suppress
// the whole line.
func TestSkipUnchangedSameLineStatement(t *testing.T) {
	skipIfNoParser(t)

	old := "const a = 1; changed(1);\n"
	new := "const a = 1; changed(2);\n"

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", "--skip-unchanged", oldPath, newPath)
	t.Logf("output:\n%s", out)

	if !strings.Contains(out, "changed(1)") || !strings.Contains(out, "changed(2)") {
		t.Errorf("the changed statement sharing a line with an unchanged declaration must still be shown:\n%s", out)
	}
}

// TestSkipUnchangedSameLineComment: a comment adjacent to an unchanged
// declaration on the same line must not suppress a real change either.
func TestSkipUnchangedSameLineComment(t *testing.T) {
	skipIfNoParser(t)

	old := "const a = 1; // note\n"
	new := "const a = 1; // note changed\n"

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", "--skip-unchanged", oldPath, newPath)
	t.Logf("output:\n%s", out)

	if !strings.Contains(out, "// note changed") {
		t.Errorf("the changed comment must still be shown:\n%s", out)
	}
}

// TestChangedCommentInsideMatchedContainer keeps absorbed comment changes visible.
func TestChangedCommentInsideMatchedContainer(t *testing.T) {
	skipIfNoParser(t)

	old := "class Widget {\n  // Handles pointerdown\n  onDown() { return 1; }\n}\n"
	new := "class Widget {\n  // Handles pointerdown event\n  onDown() { return 1; }\n}\n"

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	// The reworded comment renders as a matched comment pair with its own
	// before/after diff in the Comments section.
	if !strings.Contains(out, "-// Handles pointerdown") || !strings.Contains(out, "+// Handles pointerdown event") {
		t.Errorf("the changed comment must render as a matched comment diff:\n%s", out)
	}
	// It is never paired against the method (no comment/code assignment).
	for _, marker := range []string{"// Handles pointerdown → onDown", "onDown → // Handles pointerdown", "comment  [added]", "comment  [removed]"} {
		if strings.Contains(out, marker) {
			t.Errorf("comment must not be paired against code: %q\n%s", marker, out)
		}
	}
	// The summary counts the rewording as a changed block.
	if !strings.Contains(out, "Changed:   1") {
		t.Errorf("the comment rewording must count as Changed:\n%s", out)
	}
	// The raw supplement must not duplicate the comment lines (they differ
	// from the semantic rendering only by indentation).
	if strings.Contains(out, "-   // Handles pointerdown") || strings.Contains(out, "+   // Handles pointerdown event") {
		t.Errorf("the supplement must not re-show the absorbed comment lines:\n%s", out)
	}
}

// TestRemovedPrefixCommentSurvivesCollapse keeps removed prefix comments visible.
func TestRemovedPrefixCommentSurvivesCollapse(t *testing.T) {
	skipIfNoParser(t)

	old := "class A {\n  // removed\n  foo() {}\n}\n"
	new := "class A {\n  foo() {}\n}\n"
	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	if !strings.Contains(out, "comment  [removed]") || !strings.Contains(out, "Removed:   1") {
		t.Errorf("a removed prefix comment must remain a semantic removal after collapse:\n%s", out)
	}
}

// TestCommentMovedOutOfContainerE2E checks moved-comment rendering and coverage.
func TestCommentMovedOutOfContainerE2E(t *testing.T) {
	skipIfNoParser(t)

	old := "class A {\n  // note\n  foo() {}\n}\n"
	new := "// note\nclass A {\n  foo() {}\n}\n"

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	out := runTool(t, "--no-color", oldPath, newPath)
	t.Logf("output:\n%s", out)

	if !strings.Contains(out, "[moved]") {
		t.Errorf("the moved comment must render [moved]:\n%s", out)
	}
	if !strings.Contains(out, "Moved:     1") {
		t.Errorf("the summary must count the moved comment:\n%s", out)
	}
	// No raw removed line for the old indented comment in the supplement.
	if strings.Contains(out, "-   // note") {
		t.Errorf("the supplement must not duplicate the moved comment's old line:\n%s", out)
	}
	// The comment stays a matched pair (not removed + added).
	for _, marker := range []string{"+ // note  [added]", "- // note  [removed]"} {
		if strings.Contains(out, marker) {
			t.Errorf("the moved comment must not be removed/added: %q\n%s", marker, out)
		}
	}
}

// TestFallbackNoParse checks that unknown languages use the whole-file path.
func TestFallbackNoParse(t *testing.T) {
	skipIfNoParser(t)

	// Binary-ish content that is definitely not TypeScript.
	garbage := "\x00\x01\x02 not typescript \xff\xfe\nsecond line = {\n"
	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.xyz", garbage)
	newPath := writeFile(t, dir, "new.xyz", garbage+"added line\n")

	out := runTool(t, "--no-color", oldPath, newPath)
	if strings.Contains(out, "Parse Errors:") {
		t.Errorf("unsupported input must not surface spurious parse errors:\n%q", out)
	}
	// The whole-file diff must be present (Other section with the raw
	// lines) — the fallback path, not an empty semantic report.
	for _, want := range []string{"Other", "second line = {", "added line"} {
		if !strings.Contains(out, want) {
			t.Errorf("fallback whole-file diff must contain %q:\n%s", want, out)
		}
	}
}

// TestMixedSupportedUnsupportedFallback checks mixed-language fallback.
func TestMixedSupportedUnsupportedFallback(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", tsOld)
	newPath := writeFile(t, dir, "new.xyz", xyzNew)

	out := runTool(t, "--no-color", oldPath, newPath)
	assertCoverage(t, "semantic", out, tsOld, xyzNew)
	if strings.Contains(out, "Parse Errors:") {
		t.Errorf("mixed supported/unsupported must not surface parse errors:\n%s", out)
	}
}

// TestStdinPaths exercises the stdin content paths (- for one side) and
// the -B/-A content redirection used by GIT_EXTERNAL_DIFF wrappers.
func TestStdinPaths(t *testing.T) {
	skipIfNoParser(t)

	dir := t.TempDir()
	newPath := writeFile(t, dir, "new.ts", "const a = 2;\n")

	// Old content on stdin (display name "-" is unsupported, so this also
	// exercises the stdin fallback path).
	var out, errBuf bytes.Buffer
	if err := run([]string{"--no-color", "-", newPath}, strings.NewReader("const a = 1;\n"), &out, &errBuf); err != nil {
		t.Fatalf("stdin review failed: %v\n%s", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "const a = 1;") || !strings.Contains(out.String(), "const a = 2;") {
		t.Errorf("stdin content must appear in the report:\n%s", out.String())
	}

	// -B/-A: positional args are display names, content comes from paths.
	oldPath := writeFile(t, dir, "old-contents.ts", "const a = 1;\n")
	out.Reset()
	if err := run([]string{"--no-color", "-B", oldPath, "-A", newPath, "display-old.ts", "display-new.ts"},
		strings.NewReader(""), &out, &errBuf); err != nil {
		t.Fatalf("-B/-A review failed: %v\n%s", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "diff --cmdiff display-old.ts display-new.ts") {
		t.Errorf("-B/-A display names must appear in the header:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "const a = 2;") {
		t.Errorf("-A content must appear in the report:\n%s", out.String())
	}
}

// TestNoInputSizeLimit checks that large inputs are accepted and diffed.
func TestNoInputSizeLimit(t *testing.T) {
	skipIfNoParser(t)

	big := strings.Repeat("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", 150000) // ~9.9 MiB
	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.xyz", big+"\n")
	newPath := writeFile(t, dir, "new.xyz", big+"tail\n")

	out := runTool(t, "--no-color", oldPath, newPath)
	if !strings.Contains(out, "tail") {
		t.Errorf("large input must be accepted and diffed; expected 'tail' in output (got %d bytes)", len(out))
	}
}

// TestCLIFlagCombinations exercises the supported CLI flag combinations.
func TestCLIFlagCombinations(t *testing.T) {
	skipIfNoParser(t)

	old := "const a = 1;\nfunction f() { return 1; }\n"
	new := "const a = 2;\nfunction f() { return 2; }\n"
	dir := t.TempDir()
	oldPath := writeFile(t, dir, "old.ts", old)
	newPath := writeFile(t, dir, "new.ts", new)

	// Simple diff covers default, word-diff, ignore-space, and their combination.
	for _, combo := range [][]string{
		nil,
		{"--word-diff"},
		{"--ignore-all-space"},
		{"--word-diff", "--ignore-all-space"},
	} {
		args := append([]string{"--simple-diff", "--no-color"}, combo...)
		args = append(args, oldPath, newPath)
		out := runTool(t, args...)
		if containsFlag(combo, "--word-diff") {
			// Word-diff renders replacements as combined word lines: the
			// removed and added words must be present.
			for _, want := range []string{"~1~", "2"} {
				if !strings.Contains(out, want) {
					t.Errorf("diff %v: expected word marker %q in output\n%s", combo, want, out)
				}
			}
		} else {
			assertCoverage(t, "simple-diff", out, old, new)
			for _, want := range []string{"const a = 1;", "const a = 2;", "return 1;", "return 2;"} {
				if !strings.Contains(out, want) {
					t.Errorf("diff %v: expected %q in output\n%s", combo, want, out)
				}
			}
		}
	}

	// review: every flag combination must run and keep coverage (except
	// word-diff's combined replacement lines).
	combos := [][]string{
		nil,
		{"--word-diff"},
		{"--ignore-all-space"},
		{"--word-diff", "--ignore-all-space"},
		{"--skip-unchanged"},
		{"--summary"},
		{"--skip-unchanged", "--summary"},
	}
	for _, combo := range combos {
		args := append([]string{"--no-color"}, combo...)
		args = append(args, oldPath, newPath)
		out := runTool(t, args...)
		if !containsFlag(combo, "--word-diff") && !containsFlag(combo, "--summary") {
			assertCoverage(t, "semantic", out, old, new)
		}
		if !strings.Contains(out, "Summary") {
			t.Errorf("review %v: expected a Summary\n%s", combo, out)
		}
	}
}

func containsFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// assertDiffNoDuplication checks exact-once rendering for unique input lines.
func assertDiffNoDuplication(t *testing.T, output, oldSrc, newSrc string) {
	t.Helper()
	counts := map[string]int{}
	for _, cl := range extractContentLines(output) {
		counts[cl.content]++
	}
	inputCounts := map[string]int{}
	for _, src := range []string{oldSrc, newSrc} {
		for _, line := range nonEmptyLines(src) {
			inputCounts[line]++
		}
	}
	for line, in := range inputCounts {
		if in != 1 {
			continue // repeated content; exact-once does not apply
		}
		if n := counts[line]; n != 1 {
			t.Errorf("diff: unique input line %q appears %d times, want exactly 1\noutput:\n%s",
				line, n, output)
		}
	}
}

// assertReviewDedupMinimized checks supplement de-duplication and output size.
func assertReviewDedupMinimized(t *testing.T, output, oldSrc, newSrc string) {
	t.Helper()
	sections := splitSections(output)
	if len(sections) == 0 {
		return // summary-only or empty body
	}

	// Property 1: the Other section must only fill lines absent from semantic sections.
	otherIdx := -1
	for i, sec := range sections {
		if sec.header == "Other" {
			otherIdx = i
		}
	}
	if otherIdx < 0 || len(sections[otherIdx].lines) == 0 {
		return
	}
	earlier := map[string]bool{}
	for _, sec := range sections[:otherIdx] {
		for _, cl := range sec.lines {
			earlier[cl.typ+"\x00"+cl.content] = true
		}
	}
	for _, cl := range sections[otherIdx].lines {
		if earlier[cl.typ+"\x00"+cl.content] {
			t.Errorf("review: coverage supplement repeats line %q (type %s) already shown by a semantic section\noutput:\n%s",
				cl.content, cl.typ, output)
		}
	}

	// Property 2: allow changed lines twice, but reject unexpected duplication.
	inputLines := len(nonEmptyLines(oldSrc)) + len(nonEmptyLines(newSrc))
	rendered := len(extractContentLines(output))
	if rendered > 2*inputLines+8 {
		t.Errorf("review: rendered %d content lines for %d input lines (limit %d); unexpected duplication\noutput:\n%s",
			rendered, inputLines, 2*inputLines+8, output)
	}
}
