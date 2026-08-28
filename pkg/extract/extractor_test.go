// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package extract

import (
	"context"
	"strings"
	"testing"

	"cmscout/pkg/ir"
	"cmscout/pkg/lang"
	"cmscout/pkg/parser"
)

func extractSrc(t *testing.T, src string) []testBlock {
	t.Helper()

	langCode := lang.Language{Name: "tsx", Ext: ".tsx"}
	p, err := parser.New(langCode)
	if err != nil {
		t.Skipf("parser not available: %v", err)
	}
	defer p.Close()

	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	defer ast.Close()

	if ast.ErrorCount() != 0 {
		t.Logf("parse had errors (may be expected for partial code)")
	}

	ex := New("test.tsx")
	doc, err := ex.Extract(ast)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}

	var blocks []testBlock
	for _, b := range doc.Blocks {
		blocks = append(blocks, testBlock{Kind: string(b.Kind), Name: b.Name})
	}
	return blocks
}

type testBlock struct {
	Kind string
	Name string
}

func TestExtract_FunctionDeclaration(t *testing.T) {
	src := "function foo() { return 42; }"
	blocks := extractSrc(t, src)

	if len(blocks) == 0 {
		t.Fatal("expected at least 1 block")
	}

	found := false
	for _, b := range blocks {
		if b.Name == "foo" {
			found = true
			if b.Kind != "function" {
				t.Errorf("expected kind 'function', got '%s'", b.Kind)
			}
		}
	}
	if !found {
		t.Error("expected to find function 'foo'")
	}
	t.Logf("blocks: %+v", blocks)
}

func TestExtract_ImportStatement(t *testing.T) {
	src := "import { foo, bar } from 'mod';"
	blocks := extractSrc(t, src)

	found := false
	for _, b := range blocks {
		if b.Kind == "import" {
			found = true
			t.Logf("import block: %+v", b)
		}
	}
	if !found {
		t.Error("expected to find an import block")
	}
}

func TestExtract_ConstDeclaration(t *testing.T) {
	src := "const VERSION = '1.0.0';"
	blocks := extractSrc(t, src)

	found := false
	for _, b := range blocks {
		if b.Name == "VERSION" {
			found = true
			t.Logf("const block: %+v", b)
		}
	}
	if !found {
		t.Error("expected to find constant 'VERSION'")
	}
}

func TestExtract_ClassWithMethods(t *testing.T) {
	src := `
class Knob {
  connectedCallback() { super.connectedCallback(); }
  render() { return null; }
}
`
	blocks := extractSrc(t, src)

	kindMap := make(map[string][]string)
	for _, b := range blocks {
		kindMap[b.Kind] = append(kindMap[b.Kind], b.Name)
	}

	if _, ok := kindMap["class"]; !ok {
		t.Error("expected to find a class block")
	}

	t.Logf("extracted blocks: %+v", blocks)
}

func TestExtract_ArrowFunction(t *testing.T) {
	src := "const handler = (x) => x + 1;"
	blocks := extractSrc(t, src)

	found := false
	for _, b := range blocks {
		if b.Name == "handler" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find arrow function 'handler'")
	}
}

func TestExtract_LifecycleHook(t *testing.T) {
	src := `
class Component {
  connectedCallback() {}
  disconnectedCallback() {}
}
`
	blocks := extractSrc(t, src)

	var lifecycle []testBlock
	for _, b := range blocks {
		if b.Kind == "lifecycle" {
			lifecycle = append(lifecycle, b)
		}
	}

	if len(lifecycle) == 0 {
		t.Error("expected at least one lifecycle hook")
	}
	t.Logf("lifecycle hooks: %+v", lifecycle)
}

func TestExtract_Comment(t *testing.T) {
	src := "// this is a comment\nfunction foo() {}"
	blocks := extractSrc(t, src)

	foundComment := false
	for _, b := range blocks {
		if b.Kind == "comment" {
			foundComment = true
		}
	}
	t.Logf("comment found: %v", foundComment)
}

func TestExtract_TSX(t *testing.T) {
	// Use string concatenation to include backticks
	jsxStr := "`"
	src := "const el = html" + jsxStr + `<div class="knob">${value}</div>` + jsxStr + ";"
	blocks := extractSrc(t, src)

	foundJSX := false
	for _, b := range blocks {
		if b.Kind == "jsx" {
			foundJSX = true
		}
	}
	t.Logf("blocks: %+v", blocks)
	if !foundJSX {
		t.Log("expected to find JSX block (may vary by parser)")
	}
}

// extractFull returns the full extracted blocks (kind, name, source, span).
func extractFull(t *testing.T, src string) []ir.SemanticBlock {
	t.Helper()
	langCode := lang.Language{Name: "tsx", Ext: ".tsx"}
	p, err := parser.New(langCode)
	if err != nil {
		t.Skipf("parser not available: %v", err)
	}
	defer p.Close()
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	defer ast.Close()
	ex := New("test.tsx")
	doc, err := ex.Extract(ast)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return doc.Blocks
}

func kinds(blocks []ir.SemanticBlock) map[string][]string {
	m := make(map[string][]string)
	for _, b := range blocks {
		m[string(b.Kind)] = append(m[string(b.Kind)], b.Name)
	}
	return m
}

// TestExtract_TopLevelVsNestedDeclarations: local variable declarations
// belong to their containing semantic block and are NOT independent blocks.
func TestExtract_TopLevelVsNestedDeclarations(t *testing.T) {
	src := "const top = 1;\nfunction f() {\n  const inner = 2;\n  let local = 3;\n}\n"
	blocks := extractFull(t, src)
	km := kinds(blocks)

	if len(km["constant"]) != 1 || km["constant"][0] != "top" {
		t.Errorf("expected exactly the top-level constant 'top', got %v", km["constant"])
	}
	if len(km["variable"]) != 0 {
		t.Errorf("inner let/var declarations must not be extracted, got %v", km["variable"])
	}
	if len(km["function"]) != 1 || km["function"][0] != "f" {
		t.Errorf("expected function f, got %v", km["function"])
	}
}

// TestExtract_ArrowFunctionScope: a const/let/var inside an arrow function
// body is a local declaration and must not be extracted.
func TestExtract_ArrowFunctionScope(t *testing.T) {
	src := "const f = () => {\n  const x = 1;\n  return x;\n};\n"
	blocks := extractFull(t, src)
	km := kinds(blocks)

	if len(km["arrow_function"]) != 1 || km["arrow_function"][0] != "f" {
		t.Errorf("expected exactly the arrow function 'f', got %v", km["arrow_function"])
	}
	if len(km["constant"]) != 0 {
		t.Errorf("inner const inside an arrow body must not be extracted, got %v", km["constant"])
	}
}

// TestExtract_MethodScope: local declarations inside a method body are not
// extracted.
func TestExtract_MethodScope(t *testing.T) {
	src := "class C {\n  m() {\n    const local = 1;\n  }\n}\n"
	blocks := extractFull(t, src)
	km := kinds(blocks)

	if len(km["constant"]) != 0 {
		t.Errorf("method-local declarations must not be extracted, got %v", km["constant"])
	}
	if len(km["method"]) != 1 || km["method"][0] != "m" {
		t.Errorf("expected method m, got %v", km["method"])
	}
}

// TestExtract_FunctionExpressionNoDuplicate: `const f = function () {}`
// must produce exactly ONE block (the function-valued declaration), not a
// duplicate outer declaration plus an inner <anonymous> function block.
func TestExtract_FunctionExpressionNoDuplicate(t *testing.T) {
	src := "const f = function () { return 1; };"
	blocks := extractFull(t, src)
	km := kinds(blocks)

	if len(km["function"]) != 1 || km["function"][0] != "f" {
		t.Errorf("expected exactly one function block named f, got %v", km["function"])
	}
}

// TestExtract_ArrowFunctionNoDuplicate: `const f = () => {}` must produce
// exactly ONE block.
func TestExtract_ArrowFunctionNoDuplicate(t *testing.T) {
	src := "const f = () => 1;"
	blocks := extractFull(t, src)
	km := kinds(blocks)

	if len(km["arrow_function"]) != 1 || km["arrow_function"][0] != "f" {
		t.Errorf("expected exactly one arrow_function block named f, got %v", km["arrow_function"])
	}
}

// TestExtract_ArrowFunctionBodyLocal keeps function-local arrows inline.
func TestExtract_ArrowFunctionBodyLocal(t *testing.T) {
	src := `function spin() {
  x = [1, 2].map(a => { return a + 1; });
}
class A {
  f = () => { return 2; };
}
export default () => { return 4; };
`
	blocks := extractFull(t, src)
	km := kinds(blocks)

	// The map callback is function-local: no arrow block for it.
	if len(km["arrow_function"]) != 2 {
		t.Fatalf("expected only the class-field and module arrows, got %v", km["arrow_function"])
	}
	// One anonymous arrow (class field f) and one anonymous arrow (export default).
	for _, n := range km["arrow_function"] {
		if n != "" {
			t.Errorf("class-field and bare module arrows must be anonymous, got %q", n)
		}
	}
	if len(km["function"]) != 1 || km["function"][0] != "spin" {
		t.Errorf("expected function spin, got %v", km["function"])
	}
}

// TestExtract_DeclarationSourceAndSpan: a single-declarator top-level
// declaration covers the complete statement — declaration keyword and
// semicolon included — so the block source matches the file line instead
// of silently rendering only `name = value`.
func TestExtract_DeclarationSourceAndSpan(t *testing.T) {
	src := "const VERSION = '1.0.0';\n"
	blocks := extractFull(t, src)
	if len(blocks) != 1 {
		t.Fatalf("expected exactly 1 block, got %d: %+v", len(blocks), blocks)
	}
	b := blocks[0]
	if b.Source != "const VERSION = '1.0.0';" {
		t.Errorf("block source must be the complete declaration, got %q", b.Source)
	}
	if int(b.Span.EndByte-b.Span.StartByte) != len(b.Source) {
		t.Errorf("span must match source length: span %d-%d (%d) vs source %q (%d)",
			b.Span.StartByte, b.Span.EndByte, b.Span.EndByte-b.Span.StartByte, b.Source, len(b.Source))
	}
	if b.Span.StartLine != 0 || b.Span.EndLine != 0 {
		t.Errorf("single-line declaration should span exactly one line, got %+v", b.Span)
	}
}

// TestExtract_MultipleDeclarators: `const a = 1, b = 2;` yields one block
// per declarator with non-overlapping spans (never one misleading span
// that silently swallows the other declarator).
func TestExtract_MultipleDeclarators(t *testing.T) {
	src := "const a = 1, b = 2;\n"
	blocks := extractFull(t, src)
	km := kinds(blocks)

	if len(km["constant"]) != 2 {
		t.Fatalf("expected 2 constant blocks (a, b), got %v", km["constant"])
	}
	var a, b *ir.SemanticBlock
	for i := range blocks {
		switch blocks[i].Name {
		case "a":
			a = &blocks[i]
		case "b":
			b = &blocks[i]
		}
	}
	if a == nil || b == nil {
		t.Fatalf("missing declarator blocks: a=%v b=%v", a, b)
	}
	if a.Span.EndByte > b.Span.StartByte {
		t.Errorf("declarator spans must not overlap: a %d-%d, b %d-%d", a.Span.StartByte, a.Span.EndByte, b.Span.StartByte, b.Span.EndByte)
	}
	if int(a.Span.EndByte-a.Span.StartByte) != len(a.Source) || int(b.Span.EndByte-b.Span.StartByte) != len(b.Source) {
		t.Errorf("spans must match sources: a %q %+v, b %q %+v", a.Source, a.Span, b.Source, b.Span)
	}
}

// TestExtract_ExportClauseSingleBlock: `export { a, b }` must produce ONE
// export block covering the whole statement — never the statement plus one
// block per specifier as accidental duplicate semantic blocks.
func TestExtract_ExportClauseSingleBlock(t *testing.T) {
	src := "export { a, b };\n"
	blocks := extractFull(t, src)

	if len(blocks) != 1 {
		t.Fatalf("expected exactly 1 block, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Kind != ir.KindExport {
		t.Errorf("expected an export block, got %s", blocks[0].Kind)
	}
	if blocks[0].Source != "export { a, b };" {
		t.Errorf("export block must cover the complete statement, got %q", blocks[0].Source)
	}
}

// TestExtract_ReExportSingleBlock: `export { a } from 'mod'` is a re-export
// statement: one block, no per-specifier duplicates.
func TestExtract_ReExportSingleBlock(t *testing.T) {
	src := "export { a } from './mod';\n"
	blocks := extractFull(t, src)
	if len(blocks) != 1 {
		t.Fatalf("expected exactly 1 block, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Kind != ir.KindExport {
		t.Errorf("expected an export block, got %s", blocks[0].Kind)
	}
	if blocks[0].Name != "a" {
		t.Errorf("export block should retain the exported name, got %q", blocks[0].Name)
	}
}

func TestExtract_ExportClauseName(t *testing.T) {
	blocks := extractFull(t, "export { foo, baz as qux };\n")
	if len(blocks) != 1 {
		t.Fatalf("expected exactly one export block, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Name != "foo, baz" {
		t.Errorf("export block should list the exported names, got %q", blocks[0].Name)
	}
}

// TestExtract_GoFunctionLocalDeclarations: local var/const/short-var
// declarations inside a Go function are not extracted.
func TestExtract_GoFunctionLocalDeclarations(t *testing.T) {
	langCode := lang.Language{Name: "go", Ext: ".go"}
	p, err := parser.New(langCode)
	if err != nil {
		t.Skipf("parser not available: %v", err)
	}
	defer p.Close()
	src := "package main\n\nconst top = 1\n\nfunc f() {\n\tx := 1\n\tconst local = 2\n\tvar v = 3\n}\n"
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()
	doc, err := New("test.go").Extract(ast)
	if err != nil {
		t.Fatal(err)
	}

	km := kinds(doc.Blocks)
	if len(km["constant"]) != 1 || km["constant"][0] != "top" {
		t.Errorf("expected only the top-level constant 'top', got %v", km["constant"])
	}
	if len(km["variable"]) != 0 {
		t.Errorf("function-local variable declarations must not be extracted, got %v", km["variable"])
	}
	// The top-level constant must cover the full declaration.
	for _, b := range doc.Blocks {
		if b.Name == "top" && b.Source != "const top = 1" {
			t.Errorf("top-level Go constant source must be the full declaration, got %q", b.Source)
		}
	}
}

// TestExtract_BashFunctionLocalAssignments: variable assignments inside a
// bash function are not extracted.
func TestExtract_BashFunctionLocalAssignments(t *testing.T) {
	langCode := lang.Language{Name: "bash", Ext: ".sh"}
	p, err := parser.New(langCode)
	if err != nil {
		t.Skipf("parser not available: %v", err)
	}
	defer p.Close()
	src := "NAME=\"world\"\nfunction greet() {\n  LOCAL=\"x\"\n  echo \"hi\"\n}\n"
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()
	doc, err := New("test.sh").Extract(ast)
	if err != nil {
		t.Fatal(err)
	}

	km := kinds(doc.Blocks)
	if len(km["variable"]) != 1 || km["variable"][0] != "NAME" {
		t.Errorf("expected only the top-level variable NAME, got %v", km["variable"])
	}
}

// extractGoBlocks extracts all blocks from Go source.
func extractGoBlocks(t *testing.T, src string) []ir.SemanticBlock {
	t.Helper()
	langCode := lang.Language{Name: "go", Ext: ".go"}
	p, err := parser.New(langCode)
	if err != nil {
		t.Skipf("parser not available: %v", err)
	}
	defer p.Close()
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()
	doc, err := New("test.go").Extract(ast)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Blocks
}

// TestExtract_GoTypeSpecFullDeclaration: a sole top-level type_spec must
// cover the whole type_declaration — the "type" keyword and declaration
// terminator included — with byte and line/column spans matching the
// rendered source exactly.
func TestExtract_GoTypeSpecFullDeclaration(t *testing.T) {
	src := "package main\n\ntype Foo struct{ A int }\n"
	blocks := extractGoBlocks(t, src)
	if len(blocks) != 1 {
		t.Fatalf("expected exactly 1 block, got %d: %+v", len(blocks), blocks)
	}
	b := blocks[0]
	if b.Kind != ir.KindClass {
		t.Errorf("struct type must be a class block, got %s", b.Kind)
	}
	if b.Name != "Foo" {
		t.Errorf("expected name Foo, got %q", b.Name)
	}
	if b.Source != "type Foo struct{ A int }" {
		t.Errorf("block source must cover the whole declaration, got %q", b.Source)
	}
	if got := src[b.Span.StartByte:b.Span.EndByte]; got != b.Source {
		t.Errorf("span %d-%d must match the source %q, got %q", b.Span.StartByte, b.Span.EndByte, b.Source, got)
	}
	if b.Span.StartLine != 2 || b.Span.EndLine != 2 {
		t.Errorf("single-line declaration must span exactly line 2, got %+v", b.Span)
	}
}

// TestExtract_GoTypeAliasFullDeclaration: a type alias spec is a
// type_alias block covering the complete declaration.
func TestExtract_GoTypeAliasFullDeclaration(t *testing.T) {
	src := "package main\n\ntype Alias = string\n"
	blocks := extractGoBlocks(t, src)
	if len(blocks) != 1 {
		t.Fatalf("expected exactly 1 block, got %d: %+v", len(blocks), blocks)
	}
	b := blocks[0]
	if b.Kind != ir.KindTypeAlias {
		t.Errorf("alias must be a type_alias block, got %s", b.Kind)
	}
	if b.Source != "type Alias = string" {
		t.Errorf("block source must cover the whole declaration, got %q", b.Source)
	}
}

// TestExtract_GoGroupedTypeSpecsNonOverlapping: grouped declarations keep
// one non-overlapping block per spec; no block swallows the "type ("
// wrapper or the other spec's span.
func TestExtract_GoGroupedTypeSpecsNonOverlapping(t *testing.T) {
	src := "package main\n\ntype (\n\tA struct{ A int }\n\tB interface{ M() }\n)\n"
	blocks := extractGoBlocks(t, src)
	var a, b *ir.SemanticBlock
	for i := range blocks {
		switch blocks[i].Name {
		case "A":
			a = &blocks[i]
		case "B":
			b = &blocks[i]
		}
	}
	if a == nil || b == nil {
		t.Fatalf("expected grouped spec blocks A and B, got %+v", blocks)
	}
	if a.Kind != ir.KindClass || b.Kind != ir.KindInterface {
		t.Errorf("A must be class and B interface, got %s / %s", a.Kind, b.Kind)
	}
	if a.Source != "A struct{ A int }" || b.Source != "B interface{ M() }" {
		t.Errorf("grouped specs keep per-spec sources, got %q / %q", a.Source, b.Source)
	}
	if a.Span.EndByte > b.Span.StartByte {
		t.Errorf("grouped spec spans must not overlap: a %d-%d, b %d-%d", a.Span.StartByte, a.Span.EndByte, b.Span.StartByte, b.Span.EndByte)
	}
	for _, blk := range []*ir.SemanticBlock{a, b} {
		if got := src[blk.Span.StartByte:blk.Span.EndByte]; got != blk.Source {
			t.Errorf("span must match source: %q vs %q", got, blk.Source)
		}
	}
}

func TestExtract_GoMixedGroupedTypeSpecs(t *testing.T) {
	src := "package main\n\ntype (\n\tS struct{ A int }\n\tAlias = string\n\tI interface{ M() }\n)\n"
	blocks := extractGoBlocks(t, src)
	want := map[string]string{
		"S":     "S struct{ A int }",
		"Alias": "Alias = string",
		"I":     "I interface{ M() }",
	}
	got := make(map[string]string)
	for _, block := range blocks {
		if _, ok := want[block.Name]; ok {
			got[block.Name] = block.Source
		}
	}
	for name, source := range want {
		if got[name] != source {
			t.Errorf("grouped type %s source = %q, want %q", name, got[name], source)
		}
	}
}

// TestExtract_GoLocalTypeOwnedByFunction: a type declared inside a
// function body belongs to the enclosing function and is not extracted as
// its own block.
func TestExtract_GoLocalTypeOwnedByFunction(t *testing.T) {
	src := "package main\n\nfunc f() {\n\ttype T struct{}\n\treturn\n}\n"
	blocks := extractGoBlocks(t, src)
	km := kinds(blocks)
	if len(km["class"]) != 0 || len(km["type_alias"]) != 0 || len(km["interface"]) != 0 {
		t.Errorf("local type declarations must not be extracted, got %v", km)
	}
	if len(km["function"]) != 1 || km["function"][0] != "f" {
		t.Errorf("expected function f, got %v", km["function"])
	}
}

// TestExtract_TerminatorOnFollowingLine: a statement terminator on the
// line after a declaration is part of the block (JS tree-sitter attaches
// the ";" to the declaration), so the block source spans both lines and
// EndLine/EndCol must stay consistent with the extended byte range.
func TestExtract_TerminatorOnFollowingLine(t *testing.T) {
	src := "const a = 1\n;\n"
	blocks := extractFull(t, src)
	if len(blocks) != 1 {
		t.Fatalf("expected exactly 1 block, got %d: %+v", len(blocks), blocks)
	}
	b := blocks[0]
	if b.Source != "const a = 1\n;" {
		t.Errorf("block source must include the following-line terminator, got %q", b.Source)
	}
	if got := src[b.Span.StartByte:b.Span.EndByte]; got != b.Source {
		t.Errorf("byte span must match the source: %q vs %q", got, b.Source)
	}
	if b.Span.EndLine != 1 || b.Span.EndCol != 1 {
		t.Errorf("EndLine/EndCol must point at the terminator on the following line, got %+v", b.Span)
	}
}

func TestExtract_SourcePreserved(t *testing.T) {
	src := "function foo() { return 42; }"

	langCode := lang.Language{Name: "tsx", Ext: ".tsx"}
	p, err := parser.New(langCode)
	if err != nil {
		t.Skipf("parser not available: %v", err)
	}
	defer p.Close()

	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()

	ex := New("test.tsx")
	doc, err := ex.Extract(ast)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Blocks) == 0 {
		t.Fatal("expected at least 1 block")
	}

	for _, b := range doc.Blocks {
		if b.Name == "foo" {
			if b.Source == "" {
				t.Error("source should be preserved")
			}
			t.Logf("block source: %q", b.Source)
		}
	}
}

// extractLangBlocks extracts all blocks from source in the given language,
// optionally with SeparateMacroFunctions set (C/C++ function-like macros).
func extractLangBlocks(t *testing.T, name, src string, separateMacros bool) []ir.SemanticBlock {
	t.Helper()
	langCode := lang.Language{Name: name, Ext: "." + name}
	p, err := parser.New(langCode)
	if err != nil {
		t.Skipf("parser not available for %q: %v", name, err)
	}
	defer p.Close()
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()
	doc, err := NewWithOptions("test."+name, Options{SeparateMacroFunctions: separateMacros}).Extract(ast)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Blocks
}

func extractCBlocks(t *testing.T, src string) []ir.SemanticBlock {
	return extractLangBlocks(t, "c", src, false)
}

func extractCppBlocks(t *testing.T, src string) []ir.SemanticBlock {
	return extractLangBlocks(t, "cpp", src, false)
}

// TestExtract_CFunction: function definitions resolve their name through
// pointer declarator chains (int *foo(void)) and plain declarators.
func TestExtract_CFunction(t *testing.T) {
	src := "int *foo(void) { return 0; }\nvoid bar(int *p) {}\n"
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["function"]) != 2 {
		t.Fatalf("expected 2 function blocks, got %v", km["function"])
	}
	for _, b := range blocks {
		if b.Kind == ir.KindFunction && b.Name == "foo" && b.Source != "int *foo(void) { return 0; }" {
			t.Errorf("foo source must be the full definition, got %q", b.Source)
		}
	}
}

// TestExtract_CStruct: a standalone struct specifier is a class block.
func TestExtract_CStruct(t *testing.T) {
	src := "struct Point { int x; int y; };\n"
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["class"]) != 1 || km["class"][0] != "Point" {
		t.Errorf("expected one class block 'Point', got %v", km["class"])
	}
}

// TestExtract_CEnum: an enum specifier is an enum block.
func TestExtract_CEnum(t *testing.T) {
	src := "enum Color { RED, RED2 };\n"
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["enum"]) != 1 || km["enum"][0] != "Color" {
		t.Errorf("expected one enum block 'Color', got %v", km["enum"])
	}
}

// TestExtract_CTypedef: a typedef name is the terminal of the declarator
// chain, including function-pointer typedefs (typedef int (*handler_t)(int);).
func TestExtract_CTypedef(t *testing.T) {
	src := "typedef int (*handler_t)(int);\ntypedef int int32_t;\n"
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["type_alias"]) != 2 {
		t.Fatalf("expected 2 type_alias blocks, got %v", km["type_alias"])
	}
	want := map[string]bool{"handler_t": true, "int32_t": true}
	for _, n := range km["type_alias"] {
		if !want[n] {
			t.Errorf("unexpected typedef name %q", n)
		}
	}
	// A typedef of a struct must NOT also emit a class block (no double extraction).
	src2 := "typedef struct Point { int x; int y; } Point;\n"
	if km2 := kinds(extractCBlocks(t, src2)); len(km2["class"]) != 0 {
		t.Errorf("typedef-of-struct must not double-extract a class block, got %v", km2["class"])
	}
}

// TestExtract_CInclude: preprocessor includes are import blocks named by the path.
func TestExtract_CInclude(t *testing.T) {
	src := "#include <stdio.h>\n#include \"foo.h\"\n"
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["import"]) != 2 {
		t.Fatalf("expected 2 import blocks, got %v", km["import"])
	}
}

func TestExtract_CPreprocessorSpanExcludesLineEnding(t *testing.T) {
	src := "#include <stdio.h>\n#define VALUE 1\n#define ADD(x) (x)\n"
	blocks := extractCBlocks(t, src)
	if len(blocks) != 3 {
		t.Fatalf("expected three preprocessor blocks, got %+v", blocks)
	}
	for _, block := range blocks {
		if strings.HasSuffix(block.Source, "\n") || strings.HasSuffix(block.Source, "\r") {
			t.Errorf("%s source must not include the directive line ending: %q", block.Name, block.Source)
		}
		if got := src[block.Span.StartByte:block.Span.EndByte]; got != block.Source {
			t.Errorf("%s span/source mismatch: %q vs %q", block.Name, got, block.Source)
		}
		if block.Span.StartLine != block.Span.EndLine {
			t.Errorf("one-line directive %s has an overlong line span: %+v", block.Name, block.Span)
		}
	}
}

// TestExtract_CFileScopeVariable checks declaration spans and grouping.
func TestExtract_CFileScopeVariable(t *testing.T) {
	src := "const int G = 5;\nint counter = 0, total = 0;\n"
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["constant"]) != 1 || km["constant"][0] != "G" {
		t.Fatalf("expected one constant 'G', got %v", km["constant"])
	}
	if len(km["variable"]) != 2 {
		t.Fatalf("expected 2 variables (counter, total), got %v", km["variable"])
	}
	var g, counter, total *ir.SemanticBlock
	for i := range blocks {
		switch blocks[i].Name {
		case "G":
			g = &blocks[i]
		case "counter":
			counter = &blocks[i]
		case "total":
			total = &blocks[i]
		}
	}
	if g == nil || counter == nil || total == nil {
		t.Fatalf("missing blocks: g=%v counter=%v total=%v", g, counter, total)
	}
	// Sole declarator: full declaration including the ';'.
	if g.Source != "const int G = 5;" {
		t.Errorf("constant G must cover the full declaration, got %q", g.Source)
	}
	if got := src[g.Span.StartByte:g.Span.EndByte]; got != g.Source {
		t.Errorf("G span must match source: %q vs %q", got, g.Source)
	}
	// Grouped declarators: non-overlapping, per-declarator spans.
	if counter.Span.EndByte > total.Span.StartByte {
		t.Errorf("grouped declarator spans must not overlap: counter %d-%d, total %d-%d",
			counter.Span.StartByte, counter.Span.EndByte, total.Span.StartByte, total.Span.EndByte)
	}
	for _, b := range []*ir.SemanticBlock{counter, total} {
		if got := src[b.Span.StartByte:b.Span.EndByte]; got != b.Source {
			t.Errorf("span must match source for %q: %q vs %q", b.Name, got, b.Source)
		}
	}
}

// TestExtract_CFunctionLocalDeclarations: init_declarators inside a function
// body are locals and must not be extracted (scope guard).
func TestExtract_CFunctionLocalDeclarations(t *testing.T) {
	src := "int top = 1;\nint main(void) {\n  int local = 2;\n  const int c = 3;\n  return local;\n}\n"
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["variable"]) != 1 || km["variable"][0] != "top" {
		t.Errorf("expected only the file-scope variable 'top', got %v", km["variable"])
	}
	if len(km["constant"]) != 0 {
		t.Errorf("function-local const must not be extracted, got %v", km["constant"])
	}
	if len(km["function"]) != 1 || km["function"][0] != "main" {
		t.Errorf("expected function 'main', got %v", km["function"])
	}
}

// TestExtract_CMacroFunction: a function-like macro emits KindFunction by
// default and KindMacroFunction when SeparateMacroFunctions is set.
func TestExtract_CMacroFunction(t *testing.T) {
	src := "#define ADD(a, b) ((a) + (b))\n"
	if km := kinds(extractLangBlocks(t, "c", src, false)); len(km["function"]) != 1 || km["function"][0] != "ADD" {
		t.Errorf("separate=false: expected function 'ADD', got %v", km["function"])
	}
	if km := kinds(extractLangBlocks(t, "c", src, true)); len(km["macro_function"]) != 1 || km["macro_function"][0] != "ADD" {
		t.Errorf("separate=true: expected macro_function 'ADD', got %v", km["macro_function"])
	}
}

// TestExtract_CppClassAndMethods: a class is a class block; ctor, dtor, and
// methods defined inside it are method blocks.
func TestExtract_CppClassAndMethods(t *testing.T) {
	src := "class Widget {\n public:\n  Widget() {}\n  ~Widget() {}\n  int value() const { return 0; }\n};\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["class"]) != 1 || km["class"][0] != "Widget" {
		t.Errorf("expected one class 'Widget', got %v", km["class"])
	}
	if len(km["method"]) != 3 {
		t.Fatalf("expected 3 methods (ctor, dtor, value), got %v", km["method"])
	}
	want := map[string]bool{"Widget": true, "~Widget": true, "value": true}
	for _, n := range km["method"] {
		if !want[n] {
			t.Errorf("unexpected method %q", n)
		}
	}
}

// TestExtract_CppNamespace: named and anonymous namespaces are namespace blocks.
func TestExtract_CppNamespace(t *testing.T) {
	src := "namespace app { void f() {} }\nnamespace { void anon() {} }\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["namespace"]) != 2 {
		t.Fatalf("expected 2 namespace blocks, got %v", km["namespace"])
	}
	// One named 'app', one anonymous ("").
	names := map[string]int{}
	for _, n := range km["namespace"] {
		names[n]++
	}
	if names["app"] != 1 || names[""] != 1 {
		t.Errorf("expected namespaces 'app' and '', got %v", names)
	}
}

// TestExtract_CppTemplateFunction: a template_declaration wraps the function
// definition; the function is extracted once with its real name (no duplicate).
func TestExtract_CppTemplateFunction(t *testing.T) {
	src := "template <typename T>\nT add(T a, T b) { return a + b; }\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["function"]) != 1 || km["function"][0] != "add" {
		t.Errorf("expected one function 'add', got %v", km["function"])
	}
	if len(blocks) != 1 || blocks[0].Source != "template <typename T>\nT add(T a, T b) { return a + b; }" {
		t.Errorf("template header must belong to the function block, got %+v", blocks)
	}
}

func TestExtract_CppTemplateMethodsAndDeclarations(t *testing.T) {
	src := `class C {
 public:
  void declared();
  virtual int pure() = 0;
  template<class T> void templated(T) {}
};
template<class T> void C::out(T) {}
void C::declared() {}
`
	blocks := extractCppBlocks(t, src)
	var names []string
	for _, block := range blocks {
		if block.Kind == ir.KindMethod {
			names = append(names, block.Name)
			if block.Name == "templated" && !strings.HasPrefix(block.Source, "template<class T>") {
				t.Errorf("templated method must include its template header, got %q", block.Source)
			}
			if block.Name == "out" && block.Parent == "" {
				t.Errorf("out-of-class method must have a class parent: %+v", block)
			}
		}
	}
	want := map[string]int{"declared": 2, "pure": 1, "templated": 1, "out": 1}
	got := map[string]int{}
	for _, name := range names {
		got[name]++
	}
	if len(got) != len(want) {
		t.Fatalf("method names: got %+v, want %+v (all blocks=%+v)", got, want, blocks)
	}
	for name, count := range want {
		if got[name] != count {
			t.Errorf("method %q: got %d, want %d (all=%+v)", name, got[name], count, got)
		}
	}
}

// TestExtract_CppTemplateMethodDeclaration keeps the template header in the block.
func TestExtract_CppTemplateMethodDeclaration(t *testing.T) {
	src := `class Loop {
 public:
  template<IsLoopCallback Func>
  void add(Func&& func);
};
`
	blocks := extractCppBlocks(t, src)
	found := false
	for _, block := range blocks {
		if block.Kind == ir.KindMethod && block.Name == "add" {
			found = true
			if !strings.HasPrefix(block.Source, "template<") {
				t.Errorf("template method declaration must include its template header, got %q", block.Source)
			}
			if start := strings.Index(block.Source, "void add"); start <= 0 {
				t.Errorf("template header must precede the declaration, got %q", block.Source)
			}
		}
	}
	if !found {
		t.Fatalf("expected method add, blocks=%+v", blocks)
	}
}

func TestExtract_CppOperatorAndSpecializationNames(t *testing.T) {
	src := `class C {
 public:
  C& operator=(const C&);
  operator bool() const;
  void operator()();
};
C::operator bool() const { return true; }
template<> void f<int>() {}
`
	blocks := extractCppBlocks(t, src)
	got := map[string]ir.BlockKind{}
	for _, block := range blocks {
		got[block.Name] = block.Kind
	}
	want := map[string]ir.BlockKind{
		"operator=":     ir.KindMethod,
		"operator bool": ir.KindMethod,
		"operator()":    ir.KindMethod,
		"f<int>":        ir.KindFunction,
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%q: got %q, want %q (all=%+v)", name, got[name], kind, got)
		}
	}
}

func TestExtract_CppFriendFunctionsStayFree(t *testing.T) {
	src := `class C {
 public:
  friend void f();
};
void f() {}
`
	blocks := extractCppBlocks(t, src)
	for _, block := range blocks {
		if block.Kind == ir.KindFunction && block.Name == "f" {
			if block.Scope != "" || block.Parent != "" {
				t.Errorf("friend function must not inherit class scope: %+v", block)
			}
			return
		}
	}
	t.Fatalf("missing friend function block: %+v", blocks)
}

func TestExtract_CppQualifiedMethodsUseFullScope(t *testing.T) {
	src := `namespace A { class C { void f(); }; }
namespace B { class C { void f(); }; }
namespace A { void C::f() {} }
void B::C::f() {}
`
	blocks := extractCppBlocks(t, src)
	var methods []ir.SemanticBlock
	for _, block := range blocks {
		if block.Kind == ir.KindMethod && block.Name == "f" {
			methods = append(methods, block)
		}
	}
	if len(methods) != 4 {
		t.Fatalf("expected four C::f method blocks, got %+v", methods)
	}
	seen := map[string]int{}
	for _, method := range methods {
		seen[method.Scope]++
		if method.Parent == "" {
			t.Errorf("qualified method must have a class parent: %+v", method)
		}
	}
	if seen["A::C"] != 2 || seen["B::C"] != 2 {
		t.Errorf("qualified methods must resolve to their namespace-specific classes, got %v", seen)
	}
}

func TestExtract_CppLocalTypesNotExtracted(t *testing.T) {
	src := `void f() {
  class Local { void m() {} };
  struct S { int x; };
  enum E { A };
}
`
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["function"]) != 1 || km["function"][0] != "f" {
		t.Errorf("expected only enclosing function, got functions=%v", km["function"])
	}
	for _, kind := range []string{"class", "method", "enum", "namespace"} {
		if len(km[kind]) != 0 {
			t.Errorf("local %s declarations must not escape the enclosing function: %v", kind, km[kind])
		}
	}
}

func TestExtract_CLocalTypesNotExtracted(t *testing.T) {
	src := `void f(void) {
  struct Local { int x; };
  enum E { A };
  union U { int y; };
}
`
	blocks := extractCBlocks(t, src)
	km := kinds(blocks)
	if len(km["function"]) != 1 || km["function"][0] != "f" {
		t.Errorf("expected only enclosing function, got functions=%v", km["function"])
	}
	for _, kind := range []string{"class", "enum"} {
		if len(km[kind]) != 0 {
			t.Errorf("local %s declarations must not escape the enclosing function: %v", kind, km[kind])
		}
	}
}

// TestExtract_CppConcept: a concept definition (under a template_declaration)
// is a concept block.
func TestExtract_CppConcept(t *testing.T) {
	src := "template <typename T>\nconcept Addable = requires(T a, T b) { a + b; };\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["concept"]) != 1 || km["concept"][0] != "Addable" {
		t.Errorf("expected one concept 'Addable', got %v", km["concept"])
	}
	if len(blocks) != 1 || !strings.HasPrefix(blocks[0].Source, "template <typename T>") {
		t.Errorf("concept block must include its template header, got %+v", blocks)
	}
}

// TestExtract_ScopeAnnotation checks namespace and class paths.
func TestExtract_ScopeAnnotation(t *testing.T) {
	src := `namespace A {
namespace B {
int free_fn() { return 1; }
}
class Widget {
 public:
  Widget() {}
  int value() const;
};
int Widget::value() const { return 0; }
}
int c_main() { return 0; }
`
	blocks := extractCppBlocks(t, src)
	scope := map[string]string{}
	for _, b := range blocks {
		scope[string(b.Kind)+":"+b.Name] = b.Scope
	}
	if scope["function:free_fn"] != "A::B" {
		t.Errorf("free_fn scope = %q, want A::B (all=%v)", scope["function:free_fn"], scope)
	}
	if scope["class:Widget"] != "A" {
		t.Errorf("Widget scope = %q, want A", scope["class:Widget"])
	}
	// In-class and out-of-class method definitions both belong to A::Widget.
	if scope["method:Widget"] != "A::Widget" {
		t.Errorf("ctor scope = %q, want A::Widget", scope["method:Widget"])
	}
	if scope["method:value"] != "A::Widget" {
		t.Errorf("out-of-class value() scope = %q, want A::Widget", scope["method:value"])
	}
	if scope["function:c_main"] != "" {
		t.Errorf("plain C function scope = %q, want empty", scope["function:c_main"])
	}
}

func TestExtract_NonCxxScopeEmpty(t *testing.T) {
	blocks := extractFull(t, "class Widget { value() { return 1; } }\n")
	for _, block := range blocks {
		if block.Scope != "" {
			t.Errorf("non-C/C++ blocks must not receive C++ scope tags: %+v", block)
		}
	}
}

// TestExtract_ScopeAnnotationNoNamespace: C functions have an empty scope;
// the reporter must not assume a namespace always exists.
func TestExtract_ScopeAnnotationNoNamespace(t *testing.T) {
	blocks := extractCBlocks(t, "int main(void) { return 0; }\n")
	if len(blocks) != 1 || blocks[0].Scope != "" {
		t.Errorf("expected one top-level C function with empty scope, got %+v", blocks)
	}
}

// TestExtract_CppBodylessSpecifiersSkipped ignores non-defining type references.
func TestExtract_CppBodylessSpecifiersSkipped(t *testing.T) {
	src := `class LoopSource;
struct Node;
class Box {
 public:
  int value;
};
static_assert (sizeof (struct pollfd) == sizeof (struct pollfd));
`
	blocks := extractCppBlocks(t, src)
	var classes []string
	for _, b := range blocks {
		if b.Kind == ir.KindClass {
			classes = append(classes, b.Name)
		}
	}
	if len(classes) != 1 || classes[0] != "Box" {
		t.Errorf("only the {…}-bodied class may extract, got %v", classes)
	}
}

func TestExtract_CppTemplateClassIncludesHeader(t *testing.T) {
	src := "template <typename T>\nclass Box { T value; };\n"
	blocks := extractCppBlocks(t, src)
	if len(blocks) != 1 || blocks[0].Kind != ir.KindClass || blocks[0].Name != "Box" {
		t.Fatalf("expected one Box class block, got %+v", blocks)
	}
	if !strings.HasPrefix(blocks[0].Source, "template <typename T>") {
		t.Errorf("class block must include its template header, got %q", blocks[0].Source)
	}
}

// TestExtract_CppAliasDeclaration: `using X = ...;` is a type_alias block.
func TestExtract_CppAliasDeclaration(t *testing.T) {
	src := "using IntVec = int;\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["type_alias"]) != 1 || km["type_alias"][0] != "IntVec" {
		t.Errorf("expected one type_alias 'IntVec', got %v", km["type_alias"])
	}
}

// TestExtract_CppLambdaAssigned: `auto f = [](int x){...};` at namespace scope
// is one lambda block named f (the init_declarator owns the lambda).
func TestExtract_CppLambdaAssigned(t *testing.T) {
	src := "auto f = [](int x) { return x + 1; };\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["lambda"]) != 1 || km["lambda"][0] != "f" {
		t.Errorf("expected one lambda 'f', got %v", km["lambda"])
	}
}

// TestExtract_CppLambdaLocal keeps function-local callbacks inline.
func TestExtract_CppLambdaLocal(t *testing.T) {
	src := "void sort_call() {\n  std::sort(v, v, [](int a, int b) { return a < b; });\n}\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["lambda"]) != 0 {
		t.Errorf("function-local bare lambda must not be extracted, got %v", km["lambda"])
	}
	if len(km["function"]) != 1 || km["function"][0] != "sort_call" {
		t.Errorf("expected function sort_call, got %v", km["function"])
	}
}

// TestExtract_CppLambdaScopes: lambdas at namespace scope and class scope are
// standalone blocks; only function-local lambdas are treated as locals.
func TestExtract_CppLambdaScopes(t *testing.T) {
	src := `namespace app {
auto pred = [](int a) { return a > 0; };
}
struct S {
  auto cb = []() { return 1; };
};
`
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["lambda"]) != 2 {
		t.Fatalf("expected the namespace-scope and class-scope lambdas, got %v", km["lambda"])
	}
	if km["lambda"][0] != "pred" {
		t.Errorf("namespace-scope lambda should keep its declarator name, got %q", km["lambda"][0])
	}
	if km["lambda"][1] != "cb" {
		t.Errorf("class-field lambda should keep its field name, got %q", km["lambda"][1])
	}
	for _, block := range blocks {
		if block.Kind == ir.KindLambda && block.Name == "cb" {
			if block.Source != "auto cb = []() { return 1; };" {
				t.Errorf("class-field lambda should own its declaration, got %q", block.Source)
			}
		}
	}
}

// TestExtract_CppOutOfClassDataMember resolves static member definitions.
func TestExtract_CppOutOfClassDataMember(t *testing.T) {
	src := `class C {
 public:
  static int value;
};
int C::value = 1;
`
	blocks := extractCppBlocks(t, src)
	for _, block := range blocks {
		if block.Kind == ir.KindVariable && block.Name == "value" {
			if block.Parent == "" || block.Scope != "C" {
				t.Errorf("out-of-class data member must resolve to C: %+v", block)
			}
			return
		}
	}
	t.Fatalf("missing out-of-class data member block: %+v", blocks)
}

func TestExtract_CppInitializedFieldsStayInClass(t *testing.T) {
	src := `class W {
 public:
  int value = 1;
  auto callback = []() { return 2; };
};
`
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["variable"]) != 0 {
		t.Errorf("initialized data members must not become variable blocks, got %v", km["variable"])
	}
	if len(km["lambda"]) != 1 || km["lambda"][0] != "callback" {
		t.Errorf("class lambda field must remain a named lambda block, got %v", km["lambda"])
	}
}

// TestExtract_CppMethodLocalDeclarations keeps method locals inside the method.
func TestExtract_CppMethodLocalDeclarations(t *testing.T) {
	src := "class W {\n public:\n  void m() {\n    int local = 1;\n    const int c = 2;\n  }\n};\n"
	blocks := extractCppBlocks(t, src)
	km := kinds(blocks)
	if len(km["variable"]) != 0 {
		t.Errorf("method-local variables must not be extracted, got %v", km["variable"])
	}
	if len(km["constant"]) != 0 {
		t.Errorf("method-local constants must not be extracted, got %v", km["constant"])
	}
	if len(km["method"]) != 1 || km["method"][0] != "m" {
		t.Errorf("expected method 'm', got %v", km["method"])
	}
}

// TestExtract_CppMacroFunctionConditional: in C++ too, a function-like macro
// emits KindFunction by default and KindMacroFunction when separate is set.
func TestExtract_CppMacroFunctionConditional(t *testing.T) {
	src := "#define ADD(a, b) ((a) + (b))\n"
	if km := kinds(extractLangBlocks(t, "cpp", src, false)); len(km["function"]) != 1 || km["function"][0] != "ADD" {
		t.Errorf("separate=false: expected function 'ADD', got %v", km["function"])
	}
	if km := kinds(extractLangBlocks(t, "cpp", src, true)); len(km["macro_function"]) != 1 || km["macro_function"][0] != "ADD" {
		t.Errorf("separate=true: expected macro_function 'ADD', got %v", km["macro_function"])
	}
}

func TestExtract_CFileScopeDeclarationWithoutInitializer(t *testing.T) {
	src := "int x;\nstatic int y;\nint a, b;\nvoid (*handler)(int);\nint prototype(int);\n"
	blocks := extractCBlocks(t, src)
	got := map[string]ir.BlockKind{}
	for _, block := range blocks {
		got[block.Name] = block.Kind
		if block.Span.StartByte < block.Span.EndByte {
			if source := src[block.Span.StartByte:block.Span.EndByte]; source != block.Source {
				t.Errorf("%s span/source mismatch: %q vs %q", block.Name, source, block.Source)
			}
		}
	}
	want := map[string]ir.BlockKind{
		"x":         ir.KindVariable,
		"y":         ir.KindVariable,
		"a":         ir.KindVariable,
		"b":         ir.KindVariable,
		"handler":   ir.KindVariable,
		"prototype": ir.KindFunction,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d declaration blocks, got %d: %+v", len(want), len(got), got)
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("declaration %q: got %q, want %q", name, got[name], kind)
		}
	}
}

func TestExtract_CppFileScopeDeclarationWithoutInitializer(t *testing.T) {
	src := "int value;\nvoid f(int);\nvoid (*handler)(int);\n"
	blocks := extractCppBlocks(t, src)
	got := map[string]ir.BlockKind{}
	for _, block := range blocks {
		got[block.Name] = block.Kind
	}
	want := map[string]ir.BlockKind{
		"value":   ir.KindVariable,
		"f":       ir.KindFunction,
		"handler": ir.KindVariable,
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s: got %q, want %q (all=%+v)", name, got[name], kind, got)
		}
	}
}

func TestExtract_CppMultipleTypedefDeclarators(t *testing.T) {
	src := "typedef int A, B;\ntypedef int (*F)(int), (*G)(int);\n"
	blocks := extractCppBlocks(t, src)
	got := map[string]bool{}
	for _, block := range blocks {
		if block.Kind != ir.KindTypeAlias {
			continue
		}
		got[block.Name] = true
		if source := src[block.Span.StartByte:block.Span.EndByte]; source != block.Source {
			t.Errorf("typedef %s span/source mismatch: %q vs %q", block.Name, source, block.Source)
		}
	}
	for _, name := range []string{"A", "B", "F", "G"} {
		if !got[name] {
			t.Errorf("missing typedef alias %q in %+v", name, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("expected exactly four typedef aliases, got %+v", got)
	}
}

func TestExtract_CConstQualifiersFollowDeclarator(t *testing.T) {
	src := "const int *p = 0;\nint * const q = 0;\nconst int (*r)() = 0;\n"
	blocks := extractCBlocks(t, src)
	got := map[string]ir.BlockKind{}
	for _, block := range blocks {
		got[block.Name] = block.Kind
	}
	want := map[string]ir.BlockKind{
		"p": ir.KindVariable, // const applies to the pointee
		"q": ir.KindConstant, // const applies to the pointer object
		"r": ir.KindVariable, // const return type does not make the pointer object const
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s: got %q, want %q (all=%+v)", name, got[name], kind, got)
		}
	}
}

func TestExtract_CppConstexprAndPointerQualifiers(t *testing.T) {
	src := "constexpr int X = 1;\nconst int *p = 0;\nint * const q = 0;\n"
	blocks := extractCppBlocks(t, src)
	got := map[string]ir.BlockKind{}
	for _, block := range blocks {
		got[block.Name] = block.Kind
	}
	want := map[string]ir.BlockKind{
		"X": ir.KindConstant,
		"p": ir.KindVariable,
		"q": ir.KindConstant,
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s: got %q, want %q (all=%+v)", name, got[name], kind, got)
		}
	}
}

func TestExtract_CInlineTypeObjectSpansDoNotOverlap(t *testing.T) {
	src := `struct S { int x; } s = {1};
enum E { A } e = A;
union U { int y; } u = {2};
`
	blocks := extractCBlocks(t, src)
	byName := make(map[string]*ir.SemanticBlock, len(blocks))
	for i := range blocks {
		byName[blocks[i].Name] = &blocks[i]
		if blocks[i].Span.StartByte >= blocks[i].Span.EndByte {
			continue
		}
		if source := src[blocks[i].Span.StartByte:blocks[i].Span.EndByte]; source != blocks[i].Source {
			t.Errorf("%s span/source mismatch: %q vs %q", blocks[i].Name, source, blocks[i].Source)
		}
	}
	for _, name := range []string{"S", "E", "U", "s", "e", "u"} {
		if byName[name] == nil {
			t.Fatalf("missing inline type/object block %q: %+v", name, blocks)
		}
	}
	for _, pair := range [][2]string{{"S", "s"}, {"E", "e"}, {"U", "u"}} {
		typeBlock, objectBlock := byName[pair[0]], byName[pair[1]]
		if typeBlock.Span.StartByte < objectBlock.Span.EndByte && objectBlock.Span.StartByte < typeBlock.Span.EndByte {
			t.Errorf("inline type/object spans overlap: type=%+v object=%+v", *typeBlock, *objectBlock)
		}
	}
	if byName["s"].Source != "s = {1};" || byName["e"].Source != "e = A;" || byName["u"].Source != "u = {2};" {
		t.Errorf("object blocks should own only their declarators: s=%q e=%q u=%q", byName["s"].Source, byName["e"].Source, byName["u"].Source)
	}
}
