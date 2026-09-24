// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package stats

import (
	"context"
	"strings"
	"testing"

	"cmscout/pkg/extract"
	"cmscout/pkg/lang"
	"cmscout/pkg/parser"
)

// analyzeSrc parses, extracts, and analyzes a source string in one step.
func analyzeSrc(t *testing.T, langName, ext, filePath, src string, opts Options) *Report {
	t.Helper()
	langCode := lang.Language{Name: langName, Ext: ext}
	p, err := parser.New(langCode)
	if err != nil {
		t.Fatalf("parser not available: %v", err)
	}
	defer p.Close()
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	defer ast.Close()
	ex := extract.NewWithOptions(filePath, extract.Options{})
	doc, err := ex.Extract(ast)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	r, err := Analyze(doc, ast, opts)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}
	return r
}

// findBlock returns the block with the given qualified name, or nil.
func findBlock(r *Report, qualified string) *BlockStats {
	for i := range r.Blocks {
		if r.Blocks[i].Qualified == qualified {
			return &r.Blocks[i]
		}
	}
	return nil
}

func TestAnalyze_TSFunctionPrefixInlineAndBranches(t *testing.T) {
	src := `
/// Updates the delta.
/// Second doc line.
export function updateDelta(delta: number): number {
  // Inline note.
  if (delta > 0) {
    delta *= 2;
  } else if (delta < 0) {
    delta /= 2;
  } else {
    delta = 0;
  }
  const result = delta > 100 ? 100 : delta;
  return result && delta ? result : 0;
}
`
	r := analyzeSrc(t, "ts", ".ts", "demo.ts", src, Options{MaxCommentLines: 2})
	fn := findBlock(r, "updateDelta")
	if fn == nil {
		t.Fatalf("missing function block, got: %+v", r.Blocks)
	}
	if fn.Kind != "function" {
		t.Errorf("kind = %q, want function", fn.Kind)
	}
	if fn.StartLine != 4 || fn.EndLine != 15 {
		t.Errorf("span = %d-%d, want 4-15", fn.StartLine, fn.EndLine)
	}
	if fn.Lines != 12 {
		t.Errorf("lines = %d, want 12", fn.Lines)
	}
	if fn.PrefixLines != 2 {
		t.Errorf("prefix_lines = %d, want 2", fn.PrefixLines)
	}
	if len(fn.PrefixComments) != 1 {
		t.Fatalf("prefix comments = %d, want 1", len(fn.PrefixComments))
	}
	pc := fn.PrefixComments[0]
	if pc.PrefixOf != "updateDelta" || pc.Owner != "" {
		t.Errorf("prefix comment ownership = prefix_of %q owner %q", pc.PrefixOf, pc.Owner)
	}
	if pc.StartLine != 2 || pc.Lines != 2 {
		t.Errorf("prefix comment span = line %d lines %d, want 2/2", pc.StartLine, pc.Lines)
	}
	// Branches: if + else-if + ternary + && + ternary = 5.
	if fn.Branches != 5 {
		t.Errorf("branches = %d, want 5", fn.Branches)
	}
	// Inline comments: one single-line note.
	if len(fn.Comments) != 1 {
		t.Fatalf("inline comments = %d, want 1: %+v", len(fn.Comments), fn.Comments)
	}
	c := fn.Comments[0]
	if c.Owner != "updateDelta" || c.Lines != 1 {
		t.Errorf("inline comment = owner %q lines %d, want updateDelta/1", c.Owner, c.Lines)
	}
}

func TestAnalyze_TSClassMethodsAndQualifiedNames(t *testing.T) {
	src := `
export class Knob {
  /** Long doc comment for the lifecycle
      hook, spanning several lines,
      really quite long. */
  connectedCallback() {
    super.connectedCallback();
  }
  render() {
    return null;
  }
}
`
	r := analyzeSrc(t, "ts", ".ts", "demo.ts", src, Options{MaxCommentLines: 2})
	cls := findBlock(r, "Knob")
	if cls == nil {
		t.Fatalf("missing class block: %+v", r.Blocks)
	}
	if cls.Methods != 2 {
		t.Errorf("methods = %d, want 2", cls.Methods)
	}
	// The lifecycle doc comment is a prefix of the method, not a class comment.
	if len(cls.Comments) != 0 {
		t.Errorf("class inline comments = %d, want 0: %+v", len(cls.Comments), cls.Comments)
	}
	lc := findBlock(r, "Knob.connectedCallback")
	if lc == nil {
		t.Fatalf("missing lifecycle block: %+v", r.Blocks)
	}
	if lc.Kind != "lifecycle" {
		t.Errorf("kind = %q, want lifecycle", lc.Kind)
	}
	if len(lc.PrefixComments) != 1 || lc.PrefixLines != 3 {
		t.Errorf("prefix = %d comments / %d lines, want 1/3", len(lc.PrefixComments), lc.PrefixLines)
	}
	if !lc.PrefixComments[0].ExceedsLimit {
		t.Error("3-line prefix with max 2 must be flagged exceeds")
	}
	if lc.PrefixComments[0].PrefixOf != "Knob.connectedCallback" {
		t.Errorf("prefix_of = %q, want Knob.connectedCallback", lc.PrefixComments[0].PrefixOf)
	}
	if render := findBlock(r, "Knob.render"); render == nil || render.Methods != 0 {
		t.Errorf("render block missing or wrong: %+v", render)
	}
}

func TestAnalyze_TSCommentBoundaries(t *testing.T) {
	src := `
// Header comment, blank line below.
// Not a prefix (blank line separates).

const x = 1; // trailing comment: not a prefix

function foo() {}
`
	r := analyzeSrc(t, "ts", ".ts", "demo.ts", src, Options{})
	// File-level comments: header run and trailing comment.
	if len(r.Comments) != 2 {
		t.Errorf("file-level comments = %d, want 2: %+v", len(r.Comments), r.Comments)
	}
	foo := findBlock(r, "foo")
	if foo == nil {
		t.Fatal("missing function foo")
	}
	if foo.PrefixLines != 0 || len(foo.PrefixComments) != 0 {
		t.Errorf("blank-line-separated comment must not attach: %+v", foo.PrefixComments)
	}
}

func TestAnalyze_CPPScopeBranchesAndMethods(t *testing.T) {
	src := `
namespace app {
class Widget {
public:
  Widget();
  ~Widget();
  int value() const;
};
int Widget::value() const {
  int v = 0;
  if (v > 0) { v++; } else if (v < 0) { v--; }
  switch (v) {
  case 0: return 1;
  case 1: return 2;
  default: return v;
  }
}
}
`
	r := analyzeSrc(t, "cpp", ".cpp", "demo.cpp", src, Options{})
	cls := findBlock(r, "app::Widget")
	if cls == nil {
		t.Fatalf("missing class block: %+v", r.Blocks)
	}
	if cls.Methods != 3 {
		t.Errorf("methods = %d, want 3 (ctor, dtor, value)", cls.Methods)
	}
	// Two blocks share the name: the in-class prototype and the out-of-line
	// definition; the definition carries the branches.
	var value *BlockStats
	for i := range r.Blocks {
		if r.Blocks[i].Qualified == "app::Widget::value" && r.Blocks[i].Lines > 1 {
			value = &r.Blocks[i]
		}
	}
	if value == nil {
		t.Fatalf("missing method definition block: %+v", r.Blocks)
	}
	if value.Branches != 4 {
		t.Errorf("branches = %d, want 4", value.Branches)
	}
}

func TestAnalyze_GoBranches(t *testing.T) {
	src := `
package main
func compute(a, b int) int {
	if a > b {
		return a
	} else if b > a {
		return b
	} else {
		return 0
	}
	for i := 0; i < 3; i++ {
		a += i
	}
	switch a {
	case 0:
		return 1
	case 1:
		return 2
	default:
		return a
	}
}
`
	r := analyzeSrc(t, "go", ".go", "demo.go", src, Options{})
	fn := findBlock(r, "compute")
	if fn == nil {
		t.Fatalf("missing function: %+v", r.Blocks)
	}
	if fn.Branches != 5 {
		t.Errorf("branches = %d, want 5", fn.Branches)
	}
}

func TestAnalyze_BashBranches(t *testing.T) {
	src := `
deploy() {
  if [ -z "$1" ]; then
    echo missing
  elif [ "$1" = prod ]; then
    echo prod
  else
    echo other
  fi
  for i in 1 2 3; do
    echo $i
  done
  while false; do
    break
  done
  until true; do
    break
  done
  case "$1" in
    prod) echo careful ;;
    *) echo ok ;;
  esac
  [ -n "$1" ] && echo has || echo none
}
`
	r := analyzeSrc(t, "bash", ".sh", "demo.sh", src, Options{})
	fn := findBlock(r, "deploy")
	if fn == nil {
		t.Fatalf("missing function: %+v", r.Blocks)
	}
	if fn.Branches != 8 {
		t.Errorf("branches = %d, want 8", fn.Branches)
	}
}

func TestAnalyze_CCommentInsideFunction(t *testing.T) {
	src := `
void foo(void) {
  /* multi-line comment
     inside the function */
  int x = 1;
}
`
	r := analyzeSrc(t, "c", ".c", "demo.c", src, Options{MaxCommentLines: 1})
	fn := findBlock(r, "foo")
	if fn == nil {
		t.Fatalf("missing function: %+v", r.Blocks)
	}
	if len(fn.Comments) != 1 {
		t.Fatalf("inline comments = %d, want 1: %+v", len(fn.Comments), fn.Comments)
	}
	c := fn.Comments[0]
	if c.StartLine != 3 || c.Lines != 2 || c.Owner != "foo" {
		t.Errorf("comment = line %d lines %d owner %q, want 3/2/foo", c.StartLine, c.Lines, c.Owner)
	}
	if !c.ExceedsLimit {
		t.Error("2-line comment with max 1 must be flagged exceeds")
	}
}

func TestAnalyze_ReportOrder(t *testing.T) {
	src := `
/// doc for a
function a() {}

// standalone

function b() {}
`
	r := analyzeSrc(t, "ts", ".ts", "demo.ts", src, Options{})
	if len(r.Blocks) != 2 || len(r.Comments) != 1 {
		t.Fatalf("blocks = %d comments = %d, want 2/1", len(r.Blocks), len(r.Comments))
	}
	if r.Blocks[0].Qualified != "a" || r.Blocks[1].Qualified != "b" {
		t.Errorf("block order = %q, %q", r.Blocks[0].Qualified, r.Blocks[1].Qualified)
	}
	if r.Comments[0].StartLine != 5 {
		t.Errorf("standalone comment line = %d, want 5", r.Comments[0].StartLine)
	}
}

func TestRender_Format(t *testing.T) {
	src := `
/// doc
function foo() {}
`
	r := analyzeSrc(t, "ts", ".ts", "demo.ts", src, Options{MaxCommentLines: 3})
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("render error: %v", err)
	}
	want := "# cmscout stats: demo.ts  (ts, 0 parse errors)\n" +
		"  comment prefix_of=foo  at demo.ts:2  lines=1 chars=7\n" +
		"block function foo  at demo.ts:3-3  lines=1 chars=17  prefix_lines=1 prefix_chars=7  branches=0 complexity=1\n"
	if sb.String() != want {
		t.Errorf("render output:\n%s\nwant:\n%s", sb.String(), want)
	}
}
