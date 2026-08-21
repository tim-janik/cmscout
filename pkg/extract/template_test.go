// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package extract

// Tests for Lit html-template element extraction (pkg/extract/template.go):
// top-level elements of an `html`-tagged template become KindTemplate blocks
// with the element's raw text and exact source span, while `css`, untagged,
// and non-HTML templates stay untouched.

import (
	"context"
	"strings"
	"testing"

	"cmdiff/pkg/ir"
	"cmdiff/pkg/lang"
	"cmdiff/pkg/parser"
)

func extractBlocks(t *testing.T, src string) []ir.SemanticBlock {
	t.Helper()
	p, err := parser.New(lang.Language{Name: "tsx", Ext: ".tsx"})
	if err != nil {
		t.Skipf("parser not available: %v", err)
	}
	defer p.Close()
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	defer ast.Close()
	doc, err := New("test.tsx").Extract(ast)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return doc.Blocks
}

func templateBlocks(t *testing.T, src string) []ir.SemanticBlock {
	t.Helper()
	var out []ir.SemanticBlock
	for _, b := range extractBlocks(t, src) {
		if b.Kind == ir.KindTemplate {
			out = append(out, b)
		}
	}
	return out
}

func TestTemplateExtraction_LitHtmlTemplate(t *testing.T) {
	src := "const HTML = (t,d) => html`\n  <div id=\"sprite\" ?bidir=${d.bidir}\n    @wheel=${{handleEvent: e => t.wheel_event (e), passive: false }}\n    @pointerdown=\"${t.pointerdown}\"\n    @dblclick=\"${Util.prevent_event}\">\n  </div>\n`;\n"
	blocks := templateBlocks(t, src)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 template block, got %d (%v)", len(blocks), blocks)
	}
	b := blocks[0]
	if b.Name != "div" {
		t.Errorf("template block name = %q, want div", b.Name)
	}
	if b.Parent != "" {
		t.Errorf("template block must be parentless, got parent %q", b.Parent)
	}
	wantSrc := "<div id=\"sprite\" ?bidir=${d.bidir}\n    @wheel=${{handleEvent: e => t.wheel_event (e), passive: false }}\n    @pointerdown=\"${t.pointerdown}\"\n    @dblclick=\"${Util.prevent_event}\">\n  </div>"
	if b.Source != wantSrc {
		t.Errorf("template block source mismatch:\n got %q\nwant %q", b.Source, wantSrc)
	}
	// Byte span must point into the file: the template starts after
	// "const HTML = (t,d) => html`" (26 bytes); the element starts after
	// the backtick plus the leading "\n  " (4 bytes).
	if b.Span.StartByte != 30 {
		t.Errorf("template block StartByte = %d, want 30", b.Span.StartByte)
	}
	if int(b.Span.EndByte) != 30+len(wantSrc) {
		t.Errorf("template block EndByte = %d, want %d", b.Span.EndByte, 30+len(wantSrc))
	}
	// Positional span: element starts on line 1 (0-based), col 2 and ends
	// on the `  </div>` line at col 8.
	if b.Span.StartLine != 1 || b.Span.StartCol != 2 {
		t.Errorf("template block start position = %d:%d, want 1:2", b.Span.StartLine, b.Span.StartCol)
	}
	if b.Span.EndLine != 5 || b.Span.EndCol != 8 {
		t.Errorf("template block end position = %d:%d, want 5:8", b.Span.EndLine, b.Span.EndCol)
	}
}

func TestTemplateExtraction_OnlyTopLevelElements(t *testing.T) {
	src := "html`<div><span>x</span></div><br/><section></section>`;"
	blocks := templateBlocks(t, src)
	var names []string
	for _, b := range blocks {
		names = append(names, b.Name)
	}
	got := strings.Join(names, ",")
	if got != "div,br,section" {
		t.Errorf("template blocks = %q, want div,br,section (nested span not extracted)", got)
	}
}

func TestTemplateExtraction_NoElements(t *testing.T) {
	// Text-only template, self-closing-less markup, unterminated markup.
	for _, src := range []string{
		"html`just some text`;",
		"html`<div id=\"x\"`;", // unterminated element: skipped
		"html`<div><span>x`;",  // mismatched nesting: skipped
	} {
		if blocks := templateBlocks(t, src); len(blocks) != 0 {
			t.Errorf("source %q: expected no template blocks, got %v", src, blocks)
		}
	}
}

func TestTemplateExtraction_NonHtmlTemplatesIgnored(t *testing.T) {
	src := "const styles = css`\n  .knob { width: 100px; }\n`;\n" +
		"const raw = `\n  <div>not a template</div>\n`;\n" +
		"const other = foo`<div>not html tag</div>`;\n"
	if blocks := templateBlocks(t, src); len(blocks) != 0 {
		t.Errorf("css/untagged/non-html templates must not produce template blocks, got %v", blocks)
	}
}

func TestTemplateExtraction_MultipleTemplatesAndSubstitutions(t *testing.T) {
	src := "const a = html`<i>${x}</i>`;\nconst b = html`<b></b>`;\n"
	blocks := templateBlocks(t, src)
	var names []string
	for _, b := range blocks {
		names = append(names, b.Name)
	}
	if got := strings.Join(names, ","); got != "i,b" {
		t.Errorf("template blocks = %q, want i,b", got)
	}
}

func TestTemplateExtraction_InterpolationWithGt(t *testing.T) {
	// '>' inside an interpolation (=> arrow, comparisons) must not confuse
	// the attribute or element scanner.
	src := "html`<div ?show=${x > 1 && y => z}>hi</div>`;"
	blocks := templateBlocks(t, src)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 template block, got %v", blocks)
	}
	if blocks[0].Name != "div" {
		t.Errorf("template block name = %q, want div", blocks[0].Name)
	}
}
