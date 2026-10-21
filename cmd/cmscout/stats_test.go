// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStats_single_file(t *testing.T) {
	var stdout, stderr bytes.Buffer
	src := "/// doc for foo\nfunction foo() {\n  // note\n  if (a) { b(); }\n}\n"
	err := run([]string{"--stats", "--old", "demo.ts", "-"}, strings.NewReader(src), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run --stats: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"# cmscout stats: demo.ts  (ts, 0 parse errors)",
		"comment prefix_of=foo  at demo.ts:1  lines=1 chars=15",
		"block function foo  at demo.ts:2-5  lines=4 chars=46  prefix_lines=1 prefix_chars=15  branches=1 complexity=2",
		"comment inside=foo  at demo.ts:3  lines=1 chars=7",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestStats_stdin_and_max_comment_lines(t *testing.T) {
	var stdout, stderr bytes.Buffer
	src := "/* long comment\nspanning three\nlines */\nfunction foo() {}\n"
	err := run([]string{"--stats", "--max-comment-lines", "2", "--old", "demo.ts", "-"},
		strings.NewReader(src), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run --stats -: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "comment prefix_of=foo  at demo.ts:1-3  lines=3 chars=39  exceeds=3") {
		t.Errorf("expected exceeds flag on 3-line prefix, got:\n%s", out)
	}
}

func TestStats_before_contents(t *testing.T) {
	dir := t.TempDir()
	content_path := filepath.Join(dir, "content.ts")
	if err := os.WriteFile(content_path, []byte("function bar() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{"--stats", "-B", content_path, "display.ts"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run --stats -B: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "# cmscout stats: display.ts") ||
		!strings.Contains(out, "block function bar  at display.ts:1-1") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestStats_incomplete(t *testing.T) {
	for _, src := range []string{
		"int good(void) { return 1; }\nint bad(void) { return @; }",
		"int f(int x) { if(x) return 1;",
		"int f(void) { return 1; }\n//\xff",
	} {
		var out, stderr bytes.Buffer
		err := run([]string{"--stats", "--old", "demo.c", "-"}, strings.NewReader(src), &out, &stderr)
		if err == nil || exit_code(err) != 2 || !strings.Contains(out.String(), "# status: partial") ||
			!strings.Contains(out.String(), "# diagnostic") || strings.Contains(out.String(), "branches=") {
			t.Errorf("source=%q error=%v output=%s", src, err, &out)
		}
	}
}

func TestStats_rejects_conflicting_flags(t *testing.T) {
	for _, args := range [][]string{
		{"--stats", "--metrics"}, {"--stats", "--simple-diff"}, {"--stats", "--max-comment-lines", "-1"},
		{"--stats", "--scan", "--old", "demo.c"}, {"--metrics", "--max-comment-lines", "5", "demo.c"},
		{"--stats", "--scan", "--metrics"}, {"--stats", "--scan", "--staged"}, {"--stats", "--unknown"},
	} {
		var out, stderr bytes.Buffer
		if err := run(args, strings.NewReader(""), &out, &stderr); exit_code(err) != 2 {
			t.Errorf("%v: error=%v code=%d", args, err, exit_code(err))
		}
	}
	if err := run([]string{"--max-comment-lines", "5"}, strings.NewReader(""), io.Discard, io.Discard); exit_code(err) != 1 {
		t.Errorf("--max-comment-lines without --stats: %v", err)
	}
}

func TestStats_errors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// Unsupported language (real file, unknown extension).
	dir := t.TempDir()
	unknown := filepath.Join(dir, "demo.xyz")
	if err := os.WriteFile(unknown, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"--stats", unknown}, strings.NewReader("x"), &stdout, &stderr)
	if exit_code(err) != 2 || !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("unsupported language error = %v", err)
	}
	// Wrong positional count.
	err = run([]string{"--stats", "a.ts", "b.ts"}, strings.NewReader(""), &stdout, &stderr)
	if exit_code(err) != 2 || !strings.Contains(err.Error(), "usage: cmscout --stats") {
		t.Errorf("arg count error = %v", err)
	}
	// Missing file.
	err = run([]string{"--stats", "/nonexistent/demo.ts"}, strings.NewReader(""), &stdout, &stderr)
	if exit_code(err) != 2 {
		t.Error("missing file must error")
	}
}
