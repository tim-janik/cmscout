// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStatsMode(t *testing.T) {
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

func TestRunStatsModeStdinAndMaxCommentLines(t *testing.T) {
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

func TestRunStatsModeBeforeContents(t *testing.T) {
	dir := t.TempDir()
	contentPath := filepath.Join(dir, "content.ts")
	if err := os.WriteFile(contentPath, []byte("function bar() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{"--stats", "-B", contentPath, "display.ts"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run --stats -B: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "# cmscout stats: display.ts") ||
		!strings.Contains(out, "block function bar  at display.ts:1-1") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestRunStatsModeErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// Unsupported language (real file, unknown extension).
	dir := t.TempDir()
	unknown := filepath.Join(dir, "demo.xyz")
	if err := os.WriteFile(unknown, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"--stats", unknown}, strings.NewReader("x"), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("unsupported language error = %v", err)
	}
	// Wrong positional count.
	err = run([]string{"--stats", "a.ts", "b.ts"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "usage: cmscout --stats") {
		t.Errorf("arg count error = %v", err)
	}
	// Missing file.
	err = run([]string{"--stats", "/nonexistent/demo.ts"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Error("missing file must error")
	}
}
