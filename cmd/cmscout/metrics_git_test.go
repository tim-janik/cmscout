package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cmscout/pkg/metrics"
)

func git_test(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSuffix(string(output), "\n")
}

func git_repository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git input tests require git")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "Metrics Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "metrics@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Metrics Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "metrics@example.invalid")
	directory := t.TempDir()
	git_test(t, directory, "init", "--quiet", "--initial-branch=trunk", "--template=")
	return directory
}

func change_set_output(t *testing.T, arguments ...string) (*metrics.ChangeSet, []byte, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := run(arguments, nil, &stdout, io.Discard)
	var result metrics.ChangeSet
	if stdout.Len() > 0 {
		if decode_error := json.Unmarshal(stdout.Bytes(), &result); decode_error != nil {
			t.Fatalf("invalid change-set JSON: %v\n%s", decode_error, stdout.String())
		}
	}
	return &result, stdout.Bytes(), err
}

func set_file(t *testing.T, result *metrics.ChangeSet, name string) *metrics.Comparison {
	t.Helper()
	for _, file := range result.Files {
		for _, snapshot := range []*metrics.Snapshot{file.Comparison.Before, file.Comparison.After} {
			if snapshot != nil && snapshot.Path == name {
				return file.Comparison
			}
		}
	}
	t.Fatalf("no metrics for %q in %+v", name, result)
	return nil
}

func TestMetrics_staged_snapshots_and_file_changes(t *testing.T) {
	root := git_repository(t)
	subdir := filepath.Join(root, "src")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	before := "function f(x) { return x; }\n"
	staged := "function f(x) { return x ? 1 : 0; }\n"
	working := "function f(x) { return x ? (x && 1) : 0; }\n"
	writeFile(t, subdir, "a.js", before)
	writeFile(t, root, "gone.go", "package p\nfunc gone() {}\n")
	writeFile(t, root, "old name\n.js", "function renamed_file(x) { return x || 9; }\n")
	git_test(t, root, "add", "--all")
	git_test(t, root, "commit", "--quiet", "-m", "initial")
	writeFile(t, subdir, "a.js", staged)
	writeFile(t, root, "added.cc", "int added() { return 42; }\n")
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	git_test(t, root, "mv", "--", "old name\n.js", "new name\n.js")
	git_test(t, root, "add", "--all")
	writeFile(t, subdir, "a.js", working)
	writeFile(t, subdir, "go.mod", "unstaged metadata")
	git_test(t, root, "config", "diff.external", "touch external-ran")
	index_before, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--metrics", "--staged", "--format=json", "--root", root}
	result, first_json, err := change_set_output(t, args...)
	if err != nil || result.Kind != "change_set" || result.Status != "complete" || result.Population != "changed_files" || len(result.Files) != 4 {
		t.Fatalf("staged report failed: %+v %v", result, err)
	}
	modified := set_file(t, result, "src/a.js")
	for i, content := range []string{before, staged} {
		single, _, err := metrics_output(t, content, "--metrics", "--format=json", "--root", root, "--stdin-name=src/a.js", "-")
		snapshot := []*metrics.Snapshot{modified.Before, modified.After}[i]
		if err != nil || !reflect.DeepEqual(single, snapshot) {
			t.Fatalf("staged snapshot differs from original blob: %v", err)
		}
	}
	added, removed := set_file(t, result, "added.cc"), set_file(t, result, "gone.go")
	if added.Before != nil || removed.After != nil || len(added.Changes) != 2 || len(removed.Changes) != 2 {
		t.Fatal("whole-file callable additions or deletions missing")
	}
	renamed := set_file(t, result, "new name\n.js")
	if renamed.Before.Path != "old name\n.js" || len(renamed.Changes) != 2 {
		t.Fatalf("rename was not preserved: %+v", renamed)
	}
	for _, change := range renamed.Changes {
		if !change.Moved || !change.NameChanged || change.Match.Status != "matched" {
			t.Fatalf("rename lacks touched names: %+v", change)
		}
	}
	writeFile(t, subdir, "a.js", "broken unstaged source")
	writeFile(t, subdir, "go.mod", "different unstaged metadata")
	_, second_json, err := change_set_output(t, args...)
	if err != nil || !bytes.Equal(first_json, second_json) {
		t.Fatalf("unstaged content changed staged output: %v", err)
	}
	index_after, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil || !bytes.Equal(index_before, index_after) {
		t.Fatal("analysis modified the index")
	}
	if _, err := os.Stat(filepath.Join(root, "external-ran")); !os.IsNotExist(err) {
		t.Fatal("analysis ran an external diff command")
	}
	filtered, _, err := change_set_output(t, "--metrics", "--staged", "--format=json", "--root", root, "--include=src/**")
	if err != nil || len(filtered.Files) != 1 || len(filtered.Skipped) != 3 {
		t.Fatalf("Git path filter failed: %+v %v", filtered, err)
	}
}

func TestMetrics_unborn_worktree_and_revision(t *testing.T) {
	root := git_repository(t)
	writeFile(t, root, "a.js", "function f(x) { return x; }\n")
	git_test(t, root, "add", "--all")
	staged, _, err := change_set_output(t, "--metrics", "--staged", "--format=json", "--root", root)
	if err != nil || staged.Input.Before != "empty" || len(staged.Files) != 1 || staged.Files[0].Comparison.Before != nil {
		t.Fatalf("unborn index failed: %+v %v", staged, err)
	}
	writeFile(t, root, "a.js", "function f(x) { return x ? 1 : 0; }\n")
	writeFile(t, root, "untracked.js", "function untracked() {}\n")
	working, _, err := change_set_output(t, "--metrics", "--worktree", "--format=json", "--root", root)
	if err != nil || working.Input.Before != "index" || working.Input.After != "worktree" || len(working.Files) != 1 {
		t.Fatalf("working-tree report failed: %+v %v", working, err)
	}
	found := false
	for _, change := range working.Files[0].Comparison.Changes {
		if change.Delta.Cyclomatic != nil && *change.Delta.Cyclomatic == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("index-to-worktree complexity increase missing")
	}
	git_test(t, root, "commit", "--quiet", "-m", "initial")
	revision, _, err := change_set_output(t, "--metrics", "--revision=HEAD", "--format=json", "--root", root)
	if err != nil || revision.Input.Before != "empty" || !reflect.DeepEqual(revision.Files, staged.Files) {
		t.Fatalf("root revision differs from staged source: %+v %v", revision, err)
	}
	git_test(t, root, "add", "a.js")
	git_test(t, root, "commit", "--quiet", "-m", "change")
	revision, _, err = change_set_output(t, "--metrics", "--revision=HEAD", "--format=json", "--root", root)
	if err != nil || !reflect.DeepEqual(revision.Files, working.Files) {
		t.Fatalf("revision differs from working-tree pair: %+v %v", revision, err)
	}
	empty, _, err := change_set_output(t, "--metrics", "--revision=HEAD", "--base=HEAD", "--format=json", "--root", root)
	if err != nil || empty.Status != "complete" || len(empty.Files) != 0 {
		t.Fatalf("empty revision comparison failed: %+v %v", empty, err)
	}
}

func TestMetrics_merge_parent_choice(t *testing.T) {
	root := git_repository(t)
	writeFile(t, root, "a.js", "function f() {}\n")
	git_test(t, root, "add", "--all")
	git_test(t, root, "commit", "--quiet", "-m", "initial")
	base := git_test(t, root, "rev-parse", "HEAD")
	git_test(t, root, "branch", "side")
	writeFile(t, root, "a.js", "function f(x) { if (x) {} }\n")
	git_test(t, root, "commit", "--quiet", "-am", "trunk change")
	git_test(t, root, "checkout", "--quiet", "side")
	writeFile(t, root, "b.go", "package p\nfunc b() {}\n")
	git_test(t, root, "add", "--all")
	git_test(t, root, "commit", "--quiet", "-m", "side change")
	git_test(t, root, "checkout", "--quiet", "trunk")
	git_test(t, root, "merge", "--quiet", "--no-ff", "side", "-m", "merge")
	_, output, err := change_set_output(t, "--metrics", "--revision=HEAD", "--format=json", "--root", root)
	if exit_code(err) != 2 || len(output) != 0 || !strings.Contains(err.Error(), "choose --parent") {
		t.Fatalf("merge parent was silently chosen: %v %s", err, output)
	}
	for i, name := range []string{"b.go", "a.js"} {
		parent := []string{"--parent=1", "--parent=2"}[i]
		result, _, err := change_set_output(t, "--metrics", "--revision=HEAD", parent, "--format=json", "--root", root)
		if err != nil || len(result.Files) != 1 {
			t.Fatalf("explicit parent failed: %+v %v", result, err)
		}
		set_file(t, result, name)
	}
	result, _, err := change_set_output(t, "--metrics", "--revision=HEAD", "--base="+base, "--format=json", "--root", root)
	if err != nil || len(result.Files) != 2 {
		t.Fatalf("explicit base failed: %+v %v", result, err)
	}
}

func TestMetrics_git_partial_and_invalid_modes(t *testing.T) {
	root := git_repository(t)
	writeFile(t, root, "a.js", "function f() {")
	writeFile(t, root, "binary.cc", "binary\x00source")
	writeFile(t, root, "notes.txt", "notes")
	git_test(t, root, "add", "--all")
	result, _, err := change_set_output(t, "--metrics", "--staged", "--format=json", "--root", root)
	if exit_code(err) != 2 || result.Status != "partial" || len(result.Files) != 1 || len(result.Diagnostics) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("incomplete Git source passed: %+v %v", result, err)
	}
	for _, extra := range [][]string{
		{"--staged", "--worktree"}, {"--staged", "--scan"}, {"--worktree", "--revision=HEAD"},
		{"--staged", "a.js"}, {"--staged", "--old=a.js"}, {"--worktree", "--name=a.js"},
		{"--staged", "--parent=1"}, {"--revision=HEAD", "--parent=0"}, {"--revision=HEAD", "--parent=-1"},
		{"--revision=HEAD", "--parent=1", "--base=HEAD"}, {"--revision=missing"}, {"--worktree", "--base=HEAD"},
	} {
		args := append([]string{"--metrics", "--root", root}, extra...)
		_, output, err := change_set_output(t, args...)
		if exit_code(err) != 2 || len(output) != 0 {
			t.Fatalf("invalid Git mode accepted: %v %v %s", args, err, output)
		}
	}
	for _, format := range []string{"text", "json"} {
		if err := run([]string{"--metrics", "--staged", "--format=" + format, "--root", root}, nil, failing_metrics_writer{}, io.Discard); err == nil || exit_code(err) != 2 {
			t.Fatalf("lost change-set write error: %v", err)
		}
	}
}

func TestMetrics_unmerged_index_and_source_symlink(t *testing.T) {
	root := git_repository(t)
	writeFile(t, root, "a.js", "function f() { return 0; }\n")
	git_test(t, root, "add", "--all")
	git_test(t, root, "commit", "--quiet", "-m", "initial")
	git_test(t, root, "branch", "side")
	writeFile(t, root, "a.js", "function f() { return 1; }\n")
	git_test(t, root, "commit", "--quiet", "-am", "trunk change")
	git_test(t, root, "checkout", "--quiet", "side")
	writeFile(t, root, "a.js", "function f() { return 2; }\n")
	git_test(t, root, "commit", "--quiet", "-am", "side change")
	git_test(t, root, "checkout", "--quiet", "trunk")
	command := exec.Command("git", "-C", root, "merge", "side", "-m", "conflict")
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("fixture did not conflict: %s", output)
	}
	index_before := git_test(t, root, "ls-files", "--stage", "-z")
	for _, mode := range []string{"--staged", "--worktree"} {
		result, _, err := change_set_output(t, "--metrics", mode, "--format=json", "--root", root)
		if exit_code(err) != 2 || result.Status != "partial" || len(result.Files) != 0 ||
			len(result.Diagnostics) != 1 || result.Diagnostics[0].Kind != "unmerged" {
			t.Fatalf("unmerged index passed: %+v %v", result, err)
		}
	}
	if index_before != git_test(t, root, "ls-files", "--stage", "-z") {
		t.Fatal("analysis changed conflict stages")
	}
	git_test(t, root, "merge", "--abort")
	if err := os.Symlink("a.js", filepath.Join(root, "link.js")); err != nil {
		t.Fatal(err)
	}
	git_test(t, root, "add", "link.js")
	result, _, err := change_set_output(t, "--metrics", "--staged", "--format=json", "--root", root)
	if exit_code(err) != 2 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Kind != "nonregular" {
		t.Fatalf("source symlink passed: %+v %v", result, err)
	}
}

func TestMetrics_shallow_revision_requires_history(t *testing.T) {
	root := git_repository(t)
	writeFile(t, root, "a.js", "function f() {}\n")
	git_test(t, root, "add", "--all")
	git_test(t, root, "commit", "--quiet", "-m", "initial")
	writeFile(t, root, "a.js", "function f(x) { if (x) {} }\n")
	git_test(t, root, "commit", "--quiet", "-am", "change")
	clone := filepath.Join(t.TempDir(), "shallow")
	git_test(t, root, "clone", "--quiet", "--depth=1", "--no-local", root, clone)
	_, output, err := change_set_output(t, "--metrics", "--revision=HEAD", "--format=json", "--root", clone)
	if exit_code(err) != 2 || len(output) != 0 {
		t.Fatalf("missing parent history became an empty snapshot: %v %s", err, output)
	}
}
