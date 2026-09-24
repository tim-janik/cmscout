package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cmscout/pkg/metrics"
)

func scan_output(t *testing.T, arguments ...string) (*metrics.ScanReport, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := run(arguments, nil, &stdout, io.Discard)
	var result metrics.ScanReport
	if stdout.Len() > 0 {
		if decode_error := json.Unmarshal(stdout.Bytes(), &result); decode_error != nil {
			t.Fatalf("invalid scan JSON: %v\n%s", decode_error, stdout.String())
		}
	}
	return &result, err
}

func TestMetrics_scan_roots_and_snapshot_equivalence(t *testing.T) {
	directory := t.TempDir()
	subdir := filepath.Join(directory, "src")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	first := writeFile(t, directory, "a.js", "function f(x) { return x ? 1 : 0; }")
	second := writeFile(t, subdir, "b.go", "package p\nfunc f() {}")
	writeFile(t, directory, "readme.txt", "text")
	args := []string{"--metrics", "--format=json", "--explain", "--scan", directory, second, subdir, first}
	result, err := scan_output(t, args...)
	if err != nil || result.Status != "complete" || result.Kind != "scan" || result.Population != "selected_files" {
		t.Fatalf("scan failed: %+v %v", result, err)
	}
	if result.Root != directory || len(result.Files) != 2 || len(result.Skipped) != 1 {
		t.Fatalf("wrong scan population: %+v", result)
	}
	names := map[string]bool{}
	for i, filename := range []string{first, second} {
		single, _, err := metrics_output(t, "", "--metrics", "--format=json", "--explain", "--root", directory, filename)
		if err != nil || !reflect.DeepEqual(single, result.Files[i]) {
			t.Fatalf("scan differs from independent file %s: %v", filename, err)
		}
		for _, component := range result.Files[i].Components {
			if names[component.QualifiedName] {
				t.Fatalf("duplicate component name %s", component.QualifiedName)
			}
			names[component.QualifiedName] = true
		}
	}
	auto, err := scan_output(t, "--metrics", "--format=json", directory)
	if err != nil || len(auto.Files) != 2 {
		t.Fatalf("single directory failed: %+v %v", auto, err)
	}
	t.Chdir(directory)
	current, err := scan_output(t, "--metrics", "--scan", "--format=json")
	if err != nil || !reflect.DeepEqual(current, auto) {
		t.Fatalf("implicit current directory differs: %+v %v", current, err)
	}
	pair, err := comparison_output(t, "", "--metrics", "--format=json", first, first)
	if err != nil || pair.Before == nil || pair.After == nil {
		t.Fatalf("two-file comparison changed: %v", err)
	}
}

func TestMetrics_scan_filters_and_partial_files(t *testing.T) {
	directory := t.TempDir()
	writeFile(t, directory, "a.js", "function f() {}")
	writeFile(t, directory, "broken.js", "function f() {")
	writeFile(t, directory, "binary.go", "package p\x00")
	if err := os.Symlink(directory, filepath.Join(directory, "loop")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(directory, ".git"), "hidden.js", "function f() {")
	partial, err := scan_output(t, "--metrics", "--scan", "--format=json", directory)
	if exit_code(err) != 2 || partial.Status != "partial" || len(partial.Files) != 2 || len(partial.Diagnostics) != 1 {
		t.Fatalf("bad source did not give a partial report: %+v %v", partial, err)
	}
	if len(partial.Skipped) != 2 || partial.Skipped[0].Kind != "metadata" || partial.Skipped[1].Kind != "nonregular" {
		t.Fatalf("metadata or symlink traversal: %+v", partial.Skipped)
	}
	metadata, err := scan_output(t, "--metrics", "--scan", "--format=json", filepath.Join(directory, ".git", "hidden.js"))
	if err != nil || len(metadata.Files) != 0 || len(metadata.Skipped) != 1 || metadata.Skipped[0].Kind != "metadata" {
		t.Fatalf("explicit input bypassed Git metadata exclusion: %+v %v", metadata, err)
	}
	filtered, err := scan_output(t, "--metrics", "--scan", "--format=json", "--include=**/*.js", "--exclude=broken.js", directory)
	if err != nil || filtered.Status != "complete" || len(filtered.Files) != 1 || filtered.Files[0].Path != "a.js" {
		t.Fatalf("filter failed: %+v %v", filtered, err)
	}
}

func TestMetrics_scan_invalid_modes_and_writer(t *testing.T) {
	directory := t.TempDir()
	for _, arguments := range [][]string{
		{"--metrics", "--scan", "--old=a.js", directory},
		{"--metrics", "--scan", "--name=a.js", directory},
		{"--metrics", "--scan", "--include=[", directory},
		{"--metrics", "--scan", "--root", filepath.Join(directory, "missing"), directory},
		{"--metrics", "--scan", "--root", directory, filepath.Dir(directory)},
		{"--metrics", "--scan", "-"},
		{"--scan", directory},
	} {
		var stdout bytes.Buffer
		if err := run(arguments, nil, &stdout, io.Discard); err == nil || stdout.Len() != 0 {
			t.Fatalf("invalid arguments accepted: %v, %v, %s", arguments, err, stdout.String())
		}
	}
	for _, format := range []string{"text", "json"} {
		if err := run([]string{"--metrics", "--scan", "--format=" + format, directory}, nil, failing_metrics_writer{}, io.Discard); err == nil || exit_code(err) != 2 {
			t.Fatalf("lost scan write error: %v", err)
		}
	}
}
