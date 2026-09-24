package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmscout/pkg/metrics"
)

func metrics_output(t *testing.T, input string, arguments ...string) (*metrics.Snapshot, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(arguments, strings.NewReader(input), &stdout, &stderr)
	var snapshot metrics.Snapshot
	if stdout.Len() > 0 {
		decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		if decode_error := decoder.Decode(&snapshot); decode_error != nil {
			t.Fatalf("not a JSON snapshot: %v\n%s", decode_error, stdout.String())
		}
		var extra any
		if decode_error := decoder.Decode(&extra); decode_error != io.EOF {
			t.Fatalf("extra output after JSON: %v", decode_error)
		}
	}
	return &snapshot, stdout.String(), err
}

func TestMetrics_single_file_all_languages(t *testing.T) {
	directory := t.TempDir()
	for _, test := range []struct{ path, source string }{
		{"a.js", "// doc\nfunction f(x) { if(x) {} }"},
		{"a.jsx", "// doc\nconst f = x => <div>{x && 1}</div>;"},
		{"a.ts", "// doc\nfunction f(x: boolean) { if(x) {} }"},
		{"a.tsx", "// doc\nconst f = (x: boolean) => <div>{x && 1}</div>;"},
		{"a.go", "package p\n// doc\nfunc f(x bool) { if x {} }"},
		{"a.sh", "# doc\nf() { if true; then :; fi; }"},
		{"a.c", "// doc\nint f(int x) { if(x) {} return 0; }"},
		{"a.cc", "// doc\nauto f = [](int x) { if(x) {} };"},
	} {
		t.Run(test.path, func(t *testing.T) {
			path := writeFile(t, directory, test.path, test.source)
			snapshot, output, err := metrics_output(t, "", "--metrics", "--format=json", "--root", directory, "--explain", path)
			if err != nil || snapshot.Status != "complete" || snapshot.Path != test.path || strings.Contains(output, "\x1b") {
				t.Fatalf("metrics failed: %v\n%s", err, output)
			}
			count := 0
			for _, component := range snapshot.Components {
				if component.FunctionMetrics == nil {
					continue
				}
				count++
				if *component.Cyclomatic.Value != 2 || len(component.Cyclomatic.Decisions) != 1 || component.PrefixComment.Lines != 1 {
					t.Fatalf("wrong function metrics: %+v", component)
				}
			}
			if count != 1 {
				t.Fatalf("expected one function, got %d", count)
			}
		})
	}
}

func TestMetrics_stdin_and_logical_name(t *testing.T) {
	directory := t.TempDir()
	source := "function f(x) { return x ? 1 : 0; }"
	path := writeFile(t, directory, "saved-content", source)
	file, file_json, err := metrics_output(t, "", "--metrics", "--format=json", "--root", directory, "--name", "src/a.js", path)
	if err != nil {
		t.Fatal(err)
	}
	stdin, stdin_json, err := metrics_output(t, source, "--metrics", "--format=json", "--root", directory, "--stdin-name", "src/a.js", "-")
	if err != nil || file_json != stdin_json || file.Path != "src/a.js" || stdin.SnapshotID != file.SnapshotID {
		t.Fatalf("input transport changed metrics: %v\n%s\n%s", err, file_json, stdin_json)
	}
}

func TestMetrics_errors_and_partial_output(t *testing.T) {
	directory := t.TempDir()
	path := writeFile(t, directory, "a.js", "function f() {}")
	for _, arguments := range [][]string{
		{"--metrics"}, {"--metrics", path, path, path},
		{"--metrics", "--format=yaml", path},
		{"--metrics", "--unknown", path}, {"--metrics", "-"},
		{"--unknown", "--metrics", path},
		{"--metrics", "--stdin-name=a.js", path}, {"--metrics", "--summary", path},
		{"--metrics", "--simple-diff", path}, {"--metrics", "--root", path, path},
		{"--metrics", filepath.Join(directory, "missing.js")},
		{"--metrics", "--stdin-name=a.txt", "-"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(arguments, strings.NewReader(""), &stdout, &stderr)
			if exit_code(err) != 2 || stdout.Len() != 0 {
				t.Fatalf("error=%v code=%d output=%s", err, exit_code(err), stdout.String())
			}
		})
	}
	snapshot, output, err := metrics_output(t, "function f() {", "--metrics", "--format=json", "--stdin-name=a.js", "-")
	if exit_code(err) != 2 || snapshot.Status != "partial" || !json.Valid([]byte(output)) {
		t.Fatalf("partial input did not emit JSON and status 2: %v\n%s", err, output)
	}
	for _, component := range snapshot.Components {
		if component.FunctionMetrics != nil && component.Cyclomatic.Value != nil {
			t.Fatal("parse error produced a trusted score")
		}
	}
}

func TestMetrics_empty_header_and_text(t *testing.T) {
	directory := t.TempDir()
	empty := writeFile(t, directory, "empty.go", "")
	snapshot, _, err := metrics_output(t, "", "--metrics", "--format=json", empty)
	if err != nil || snapshot.Status != "complete" || len(snapshot.Components) != 1 {
		t.Fatalf("empty source failed: %+v %v", snapshot, err)
	}
	header := writeFile(t, directory, "api.h", "typeof(int) value;\nint f(int x) { return x ? 1 : 0; }\n")
	snapshot, _, err = metrics_output(t, "", "--metrics", "--format=json", header)
	if err != nil || snapshot.Language != "c" {
		t.Fatalf("header selection failed: %+v %v", snapshot, err)
	}
	var stdout, stderr bytes.Buffer
	err = run([]string{"--metrics", "--explain", header}, nil, &stdout, &stderr)
	for _, want := range []string{"cyclomatic=2", "prefix_comment:", "inline_comments: []", "+1 ternary", "1 callable definitions"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("text lacks %q: %s", want, stdout.String())
		}
	}
	if err != nil || strings.Contains(stdout.String(), "diff --cmscout") {
		t.Fatalf("single file used diff mode: %v\n%s", err, stdout.String())
	}
}

type failing_metrics_writer struct{}

func (failing_metrics_writer) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestMetrics_write_failure(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		err := run([]string{"--metrics", "--format=" + format, "--stdin-name=a.js", "-"},
			strings.NewReader("function f() {}"), failing_metrics_writer{}, io.Discard)
		if !errors.Is(err, io.ErrClosedPipe) || exit_code(err) != 2 {
			t.Fatalf("lost write error: %v", err)
		}
	}
}

func TestMetric_context_discovery(t *testing.T) {
	directory := t.TempDir()
	for _, marker := range []string{"go.mod", "go.work", "compile_commands.json", ".git"} {
		root := filepath.Join(directory, marker+"-project")
		child := filepath.Join(root, "src")
		if err := os.MkdirAll(child, 0755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, marker, "")
		inferred, err := metric_context(filepath.Join(child, "a.go"), "", false)
		if err != nil {
			t.Fatal(err)
		}
		explicit, err := metric_context("src/a.go", root, true)
		if err != nil || inferred != explicit || inferred.Path != "src/a.go" {
			t.Fatalf("root inference differs: %+v %+v %v", inferred, explicit, err)
		}
	}
	if _, err := metric_context("../outside.go", directory, true); err == nil {
		t.Fatal("accepted logical path outside root")
	}
}
