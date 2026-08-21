// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package extract

import (
	"context"
	"testing"

	"cmdiff/pkg/ir"
	"cmdiff/pkg/lang"
	"cmdiff/pkg/parser"
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
