// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package parser

import (
	"context"
	"testing"

	"cmscout/pkg/lang"
)

func parseSrc(t *testing.T, name, src string) *AST {
	t.Helper()
	l := lang.Language{Name: name, Ext: "." + name}
	p, err := New(l)
	if err != nil {
		// CGO / grammar unavailable in this environment; can't test.
		t.Skipf("parser not available for %q: %v", name, err)
	}
	defer p.Close()
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("Parse returned error for %q: %v", name, err)
	}
	t.Cleanup(ast.Close)
	return ast
}

// TestAST_CleanSource reports no errors for grammatically valid input across
// every supported language.
func TestAST_CleanSource(t *testing.T) {
	cases := []struct{ name, src string }{
		{"ts", `function greet(name: string): string { return "hi"; }`},
		{"tsx", "const App = () => <div>hi</div>;"},
		{"js", "function f(x) { return x + 1; }"},
		{"go", "package main\nfunc ok() int { return 1 }\n"},
		{"bash", "function f() { echo hi; }\n"},
		// C: preprocessor, typedef, enum, function.
		{"c", "#include <stdio.h>\n#define MAX 100\ntypedef struct Point { int x; int y; } Point;\nenum Color { RED, GREEN, BLUE };\nint add(int a, int b) { return a + b; }\n"},
		// C++: namespace, template, concept, class with ctor/dtor/method, lambda.
		{"cpp", "namespace app {\ntemplate <typename T>\nconcept Addable = requires(T a, T b) { a + b; };\nclass Widget {\n public:\n  Widget() {}\n  ~Widget() {}\n  int value() const { return 0; }\n};\nauto f = [](int x) { return x + 1; };\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast := parseSrc(t, tc.name, tc.src)
			if n := ast.ErrorCount(); n != 0 {
				t.Errorf("clean %s source: ErrorCount=%d, want 0", tc.name, n)
			}
		})
	}
}

// TestAST_ErrorCount_Malformed verifies that genuinely un-parseable input
// produces tree-sitter ERROR nodes that ErrorCount can enumerate.
//
// IMPORTANT: tree-sitter is an error-recovering parser. Many malformed inputs
// (e.g. an unbalanced brace) parse *without* emitting any ERROR/MISSING node:
// the recovery eats the bad region and the root node's HasError() stays true
// while ErrorCount returns 0. Such inputs are deliberately NOT used here. The
// cases below are inputs known to defeat recovery for each vendored grammar,
// so ErrorCount reliably returns > 0. This is the trigger that the earlier
// manual attempts using "unclosed function" failed to hit.
//
// A non-zero count is the "don't fully trust this" signal surfaced in the
// report's "Parse Errors" row (Issue D). A zero count is *not* a proof of
// validity — recovery can silently swallow errors.
//
// Thresholds are minimums (>=), not exacts: ERROR nodes can nest, so a single
// bad region may count as 2. Bump these only when upgrading a grammar in
// go.mod if the new grammar recovers an input it previously rejected.
func TestAST_ErrorCount_Malformed(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int // ErrorCount must be >= want
	}{
		// TypeScript: stray '@' (no valid production starts with it) → 1 ERROR.
		{"ts", "@", 1},
		// TypeScript: ')' after '=' with trailing '(' → 2 nested ERRORs.
		{"ts", "const x = ) (", 2},
		// Go: invalid tokens inside a function body → 2 nested ERRORs.
		// Requires the 'package' prefix or recovery eats the whole file.
		{"go", "package main\nfunc main() { @#$ }\n", 2},
		// Bash: 'if' with no 'then'/'(' → 1 ERROR.
		// (Bash treats lone '@' as a valid command name, hence the malformed
		// control-flow construct instead.)
		{"bash", "if then fi\n", 1},
		// Bash: raw closing parens at top level → 1 ERROR.
		{"bash", "))) ((( ", 1},
		// C: stray '@' (no valid production starts with it) → 2 nested ERRORs.
		{"c", "@", 1},
		// C: malformed function-like macro → 1 ERROR.
		{"c", "#define )(", 1},
		// C++: stray '@' → 2 nested ERRORs.
		{"cpp", "@", 1},
		// C++: malformed concept definition → 1 ERROR.
		{"cpp", "concept = ;", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast := parseSrc(t, tc.name, tc.src)
			if n := ast.ErrorCount(); n < tc.want {
				t.Errorf("malformed %s source %q: ErrorCount=%d, want >= %d",
					tc.name, tc.src, n, tc.want)
			} else {
				t.Logf("%q → ErrorCount=%d", tc.src, n)
			}
		})
	}
}

// TestAST_ErrorCount_ShortCircuitOnClean documents that ErrorCount does not
// walk the tree when RootNode().HasError() is false — the fast path returns 0
// without traversing children.
func TestAST_ErrorCount_ShortCircuitOnClean(t *testing.T) {
	ast := parseSrc(t, "ts", `const x = 1;`)
	if ast.ErrorCount() != 0 {
		t.Errorf("clean source ErrorCount=%d, want 0", ast.ErrorCount())
	}
}
