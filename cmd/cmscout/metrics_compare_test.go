package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"cmscout/pkg/metrics"
)

func comparison_output(t *testing.T, input string, args ...string) (*metrics.Comparison, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, strings.NewReader(input), &stdout, &stderr)
	if stdout.Len() == 0 {
		return nil, err
	}
	var comparison metrics.Comparison
	decoder := json.NewDecoder(&stdout)
	if decode_error := decoder.Decode(&comparison); decode_error != nil {
		t.Fatalf("invalid comparison JSON: %v\n%s", decode_error, stdout.String())
	}
	var extra any
	if decode_error := decoder.Decode(&extra); decode_error != io.EOF {
		t.Fatalf("output after comparison JSON: %v", decode_error)
	}
	return &comparison, err
}

func TestMetrics_comparison_all_languages(t *testing.T) {
	directory := t.TempDir()
	for _, test := range []struct{ extension, before, after string }{
		{"js", "function f(x){ return x; }", "function f(x){ if(x){} return x; }"},
		{"jsx", "const f = x => <div>{x}</div>;", "const f = x => <div>{x && 1}</div>;"},
		{"ts", "function f(x: boolean){ return x; }", "function f(x: boolean){ if(x){} return x; }"},
		{"tsx", "const f = (x: boolean) => <div>{x}</div>;", "const f = (x: boolean) => <div>{x && 1}</div>;"},
		{"go", "package p\nfunc f(x bool) { }", "package p\nfunc f(x bool) { if x {} }"},
		{"sh", "f() { :; }", "f() { if true; then :; fi; }"},
		{"c", "int f(int x){ return x; }", "int f(int x){ if(x){} return x; }"},
		{"cc", "auto f = [](int x){ return x; };", "auto f = [](int x){ if(x){} return x; };"},
	} {
		t.Run(test.extension, func(t *testing.T) {
			old_path := writeFile(t, directory, "before."+test.extension, test.before)
			new_path := writeFile(t, directory, "after."+test.extension, test.after)
			comparison, err := comparison_output(t, "", "--metrics", "--format=json", "--explain", old_path, new_path)
			if err != nil || comparison == nil || comparison.Status != "complete" {
				t.Fatalf("comparison failed: %+v %v", comparison, err)
			}
			if comparison.Diff == nil || comparison.Diff.Format != "cmscout-text-v1" ||
				!strings.Contains(comparison.Diff.Text, "diff --cmscout") || strings.Contains(comparison.Diff.Text, "\x1b") {
				t.Fatalf("missing or colored diff: %+v", comparison.Diff)
			}
			found := false
			for _, change := range comparison.Changes {
				if change.Delta.Cyclomatic != nil && *change.Delta.Cyclomatic == 1 {
					found = true
				}
			}
			if !found {
				t.Fatalf("no complexity delta: %+v", comparison.Changes)
			}
		})
	}
}

func TestMetrics_comparison_matches_single_file_records(t *testing.T) {
	directory := t.TempDir()
	before := "// first\nfunction outer(x){\n // inner\n const f = () => x;\n}"
	after := "// later\nfunction outer(x){\n // inner\n const f = () => x ? 1 : 0;\n}"
	old_path := writeFile(t, directory, "old-content", before)
	new_path := writeFile(t, directory, "new-content", after)
	comparison, err := comparison_output(t, "", "--metrics", "--format=json", "--explain", "--root", directory,
		"--old", "src/a.js", "--new", "src/a.js", "-B", old_path, "-A", new_path)
	if err != nil {
		t.Fatal(err)
	}
	for _, side := range []struct {
		path     string
		snapshot *metrics.Snapshot
	}{{old_path, comparison.Before}, {new_path, comparison.After}} {
		single, _, err := metrics_output(t, "", "--metrics", "--format=json", "--explain", "--root", directory,
			"--name", "src/a.js", side.path)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(single)
		got, _ := json.Marshal(side.snapshot)
		if string(want) != string(got) {
			t.Fatalf("diff rendering changed measurements:\n%s\n%s", want, got)
		}
	}
	stdin, err := comparison_output(t, before, "--metrics", "--format=json", "--explain", "--root", directory,
		"--old", "src/a.js", "--new", "src/a.js", "-B", "-", "-A", new_path)
	if err != nil || stdin.Before.SnapshotID != comparison.Before.SnapshotID {
		t.Fatalf("stdin changed comparison: %+v %v", stdin, err)
	}
}

func TestMetrics_comparison_failures(t *testing.T) {
	directory := t.TempDir()
	old_path := writeFile(t, directory, "before.js", "function f(){ call(() => 1, () => 1); }")
	new_path := writeFile(t, directory, "after.js", "function f(){ call(() => 1, () => 1, () => 1); }")
	comparison, err := comparison_output(t, "", "--metrics", "--format=json", old_path, new_path)
	if exit_code(err) != 2 || comparison == nil || comparison.Status != "partial" || comparison.Diff == nil {
		t.Fatalf("ambiguous comparison should emit a partial report: %+v %v", comparison, err)
	}
	damaged := writeFile(t, directory, "damaged.js", "function f() {")
	comparison, err = comparison_output(t, "", "--metrics", "--format=json", old_path, damaged)
	if exit_code(err) != 2 || comparison == nil || comparison.After.Status != "partial" || comparison.Diff == nil {
		t.Fatalf("damaged input should emit a partial report: %+v %v", comparison, err)
	}
	for _, change := range comparison.Changes {
		if change.Delta.Cyclomatic != nil || change.Added || change.Removed {
			t.Fatalf("incomplete source got a trusted change: %+v", change)
		}
	}
	for _, args := range [][]string{
		{"--metrics", "--format=json", "--name=a.js", old_path, new_path},
		{"--metrics", "--format=json", "--stdin-name=a.js", old_path, new_path},
		{"--metrics", "--format=json", "--old=a.js", "--new=a.js", "-B=-", "-A=-"},
		{"--metrics", "--format=json", directory, new_path},
	} {
		comparison, err := comparison_output(t, "", args...)
		if exit_code(err) != 2 || comparison != nil {
			t.Fatalf("invalid input accepted: %+v %v", comparison, err)
		}
	}
}

func TestMetrics_comparison_text_and_render_flags(t *testing.T) {
	directory := t.TempDir()
	old_path := writeFile(t, directory, "before.js", "function f(x){return x;}")
	new_path := writeFile(t, directory, "after.js", "function f(x) { return x; }")
	comparison, err := comparison_output(t, "", "--metrics", "--format=json", "--ignore-all-space", old_path, new_path)
	if err != nil || len(comparison.Changes) == 0 {
		t.Fatalf("rendering flag hid metric changes: %+v %v", comparison, err)
	}
	var stdout, stderr bytes.Buffer
	err = run([]string{"--metrics", old_path, new_path}, nil, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Before", "After", "Touched components", "formatting", "delta cyclomatic=0", "diff --cmscout"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("text lacks %q:\n%s", want, stdout.String())
		}
	}
	for _, format := range []string{"text", "json"} {
		err := run([]string{"--metrics", "--format=" + format, old_path, new_path}, nil, failing_metrics_writer{}, io.Discard)
		if exit_code(err) != 2 {
			t.Fatalf("lost comparison write error: %v", err)
		}
	}
}
