package stats

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"cmscout/pkg/extract"
	"cmscout/pkg/ir"
	"cmscout/pkg/lang"
	"cmscout/pkg/parser"
)

func TestComplexity_decisions(t *testing.T) {
	type complexity_case struct {
		name       string
		language   string
		source     string
		complexity int
	}
	tests := []complexity_case{
		{"c_empty", "c", "int f(void) { return 0; }", 1},
		{"c_decisions", "c", "int f(int a, int b) { if (a && b) return a; else if (a || b) return b; return a ? b : 0; }", 6},
		{"c_loops", "c", "void f(int n) { for (int i=0; i<n; ++i) {} while (n) {} do {} while (n); }", 4},
		{"c_switch", "c", "void f(int n) { switch(n) { case 1: case 2: break; default: break; } }", 3},
		{"c_default", "c", "void f(int n) { switch(n) { default: break; } }", 1},
		{"c_preprocessor", "c", "void f(int n) {\n#if A && B\n if (n) {}\n#else\n while (n) {}\n#endif\n}", 3},
		{"c_literals", "c", "int f(void) { /* if && || ? */ const char *s = \"if && || ?\"; return 1 & 2 | 3; }", 1},
		{"cpp_range", "cpp", "void f() { for (auto x : xs) { if (x) {} } }", 3},
		{"cpp_alternative_operators", "cpp", "bool f(bool a, bool b) { return a and b or a; }", 3},
		{"cpp_catch", "cpp", "void f() { try { g(); } catch (int e) {} catch (...) {} }", 3},
		{"cpp_unevaluated", "cpp", "void f(int a, int b) { sizeof(a && b); noexcept(a || b); static_assert(true && true); }", 1},
		{"cpp_template", "cpp", "template<class T> int f(T t) { return t ? 1 : 0; }", 2},
		{"cpp_constructor", "cpp", "class C { public: C(int a) : x(a ? 1 : 0) { if (x) {} } int x; };", 3},
		{"cpp_function_try", "cpp", "void f() try { g(); } catch (...) {}", 2},
		{"go_empty", "go", "package p\nfunc f() {}", 1},
		{"go_boolean", "go", "package p\nfunc f(a, b bool) { if a && b || a {} else if b {} }", 5},
		{"go_loops", "go", "package p\nfunc f(xs []int) { for i:=0; i<3; i++ {} ; for _, x := range xs { _ = x }; for {} }", 4},
		{"go_switch", "go", "package p\nfunc f(n int) { switch n { case 1,2,3: return; default: return } }", 4},
		{"go_case_comments", "go", "package p\nfunc f(n int) { switch n { case 1, /* note */ 2: return; default: return } }", 3},
		{"go_type_switch", "go", "package p\nfunc f(v any) { switch v.(type) { " +
			"case int, string: return; case nil: return; default: return } }", 4},
		{"go_select", "go", "package p\nfunc f(ch chan int) { select { case <-ch: return; case ch <- 1: return; default: return } }", 3},
		{"go_select_blocking", "go", "package p\nfunc f(ch chan int) { select { case <-ch: return; case ch <- 1: return } }", 2},
		{"go_select_single", "go", "package p\nfunc f(ch chan int) { select { case <-ch: return } }", 1},
		{"go_select_empty", "go", "package p\nfunc f() { select {} }", 1},
		{"bash_empty", "bash", "f() { :; }", 1},
		{"bash_if", "bash", "f() { if true; then :; elif false; then :; else :; fi; }", 3},
		{"bash_loops", "bash", "f() { for x in a b; do :; done; while true; do :; done; " +
			"until false; do :; done; for ((i=0;i<3;i++)); do :; done; select x in a b; do :; done; }", 6},
		{"bash_lists", "bash", "f() { true && true || false; [[ a && b || c ]]; ((a && b || c)); }", 7},
		{"bash_arithmetic", "bash", "f() { echo $((a ? b : c)); }", 2},
		{"bash_test", "bash", "f() { [ a -a b -o c ]; }", 3},
		{"c_unevaluated", "c", "void f(int a, int b) { sizeof(a && b); }", 1},
		{"bash_case", "bash", "f() { case $1 in a|b) :;; c) :;; *) :;; esac; }", 4},
		{"bash_quoted_star", "bash", "f() { case $1 in '*') :;; *) :;; esac; }", 2},
		{"bash_expansions", "bash", "f() { echo ${a:-b} ${a:+b} ${a:=b} ${a:?b} ${a-b} ${a+b} ${a=b} ${a?b}; }", 9},
		{"bash_substitutions", "bash", "f() { echo \"$(if true; then :; fi)\"; (true && false); }", 3},
		{"bash_body", "bash", "f() if true; then :; fi", 2},
	}
	for _, language := range []string{"js", "jsx", "ts", "tsx"} {
		for _, js := range []struct {
			name       string
			source     string
			complexity int
		}{
			{"empty", "export function f() {}", 1},
			{"decisions", "function f(a,b) { if (a && b) {} else if (a || b) {} else {} return a ? b : 0; }", 6},
			{"loops", "function f(xs) { for (;;) {} for (let x in xs) {} for (let x of xs) {} while (xs) {} do {} while (xs); }", 6},
			{"switch", "function f(x) { switch (x) { case 1: case 2: break; default: break; } }", 3},
			{"default", "function f(x) { switch (x) { default: break; } }", 1},
			{"catch", "function f() { try { g(); } catch (e) {} finally {} }", 2},
			{"short_circuit", "function f(a,b) { a &&= b; a ||= b; a ??= b; return a ?? b; }", 5},
			{"optional_chain", "function f(a) { return a?.b?.[0]?.(); }", 4},
			{"defaults", "function f(a = 0, {b = 1} = {}) { const {c = 2} = a; let [d = 3] = b; }", 6},
			{"generator", "function* f(a) { if (a) yield 1; }", 2},
			{"arrow", "export const f = x => x ? 1 : 0;", 2},
			{"expression", "const f = function(x) { return x && 1; };", 2},
			{"method", "const o = { f(x) { return x || 1; } };", 2},
			{"lifecycle", "class C { connectedCallback() { if (x) {} } }", 2},
			{"literals", "function f() { /* if && ? */ return /if|while/.test('&& || ??'); }", 1},
		} {
			tests = append(tests, complexity_case{language + "_" + js.name, language, js.source, js.complexity})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := analyzeSrc(t, tt.language, "", "demo", tt.source, Options{})
			if r.ParseErrors != 0 {
				t.Fatalf("fixture has %d parse errors", r.ParseErrors)
			}
			var found []int
			for _, b := range r.Blocks {
				if b.Complexity > 0 {
					found = append(found, b.Complexity)
					if b.Branches != b.Complexity-1 {
						t.Errorf("branches=%d complexity=%d", b.Branches, b.Complexity)
					}
				}
			}
			if !reflect.DeepEqual(found, []int{tt.complexity}) {
				t.Errorf("complexities = %v, want [%d]; blocks: %+v", found, tt.complexity, r.Blocks)
			}
		})
	}
}

func TestComplexity_function_boundaries(t *testing.T) {
	tests := []struct {
		language string
		source   string
		want     []int
	}{
		{"js", "function outer(a) { if (a) { function inner(b) { if (b && a) {} } } " +
			"const arrow = x => x ? 1 : 0; const f = function() { while (a) {} }; }", []int{2, 3, 2, 2}},
		{"ts", "function outer(a = () => x ? 1 : 0): void { " +
			"type T = typeof fn<x extends true ? 1 : 2>; class C { x = a || b; m() { if (a) {} } } }", []int{2, 2, 2}},
		{"tsx", "function outer() { return <div>{x ? <span/> : null}{xs.map(x => x && 1)}</div>; }", []int{2, 2}},
		{"go", "package p\nfunc outer() { if true { f := func() { if a && b {} }; _ = f } }", []int{2, 3}},
		{"cpp", "void outer() { if (x) { auto f = [] { if (a && b) {} }; } struct C { void m() { while(x) {} } }; }", []int{2, 3, 2}},
		{"cpp", "void outer() { auto f = [x = a ? b : c] { if (x) {} }; }", []int{2, 2}},
		{"js", "function outer() { const o = { [x ? a : b]() { if (y) {} } }; }", []int{2, 2}},
		{"ts", "function outer() { class C { [x ? a : b]() { if (y) {} } } }", []int{2, 2}},
		{"cpp", "auto f = [] (int x) { return x ? 1 : 0; };", []int{2}},
		{"bash", "outer() { if true; then inner() { true && false; }; fi; }", []int{2, 2}},
		{"js", "const f = a => b => b ? a : 0, g = x => x || 1;", []int{1, 2, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			r := analyzeSrc(t, tt.language, "", "demo", tt.source, Options{})
			if r.ParseErrors != 0 {
				t.Fatalf("fixture has %d parse errors", r.ParseErrors)
			}
			var got []int
			for _, b := range r.Blocks {
				if b.Complexity > 0 {
					got = append(got, b.Complexity)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("complexities = %v, want %v; blocks: %+v", got, tt.want, r.Blocks)
			}
		})
	}
}

func TestComplexity_unknown(t *testing.T) {
	for _, source := range []string{
		"int f(void);",
		"#define F(x) ((x) ? 1 : 0)\n",
		"int f(void) { return @; }",
	} {
		r := analyzeSrc(t, "c", ".c", "demo.c", source, Options{})
		var out strings.Builder
		if err := Render(&out, r); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "complexity=unknown") || strings.Contains(out.String(), "branches=") {
			t.Errorf("unexpected output: %s", out.String())
		}
	}
}

func TestComplexity_preserves_document(t *testing.T) {
	p, err := parser.New(lang.Language{Name: "go"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ast, err := p.Parse(context.Background(), []byte("package p\nfunc f() { g := func() { if x {} }; _ = g }"))
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()
	doc, err := extract.NewWithOptions("demo.go", extract.Options{}).Extract(ast)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]ir.SemanticBlock(nil), doc.Blocks...)
	a, err := Analyze(doc, ast, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Analyze(doc, ast, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, doc.Blocks) {
		t.Error("Analyze changed the input document")
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("repeated analysis differs")
	}
}
