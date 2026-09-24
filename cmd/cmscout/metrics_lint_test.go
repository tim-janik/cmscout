package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetrics_lint_example(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("the optional lint example needs Python 3")
	}
	directory := t.TempDir()
	before := writeFile(t, directory, "before.js", "function f(x) { return x; }")
	after := writeFile(t, directory, "after.js", "// documentation\nfunction f(x) { return x ? 1 : 0; }")
	comparison, err := comparison_output(t, "", "--metrics", "--format=json", before, after)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(comparison)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		arguments []string
		code      int
		message   string
	}{
		{[]string{"--max-complexity=2"}, 0, ""},
		{[]string{"--max-complexity=1"}, 1, "complexity 2 exceeds 1"},
		{[]string{"--max-increase=0"}, 1, "complexity increased by 1"},
		{[]string{"--max-comment-chars=3"}, 1, "prefix comment has"},
	} {
		t.Run(strings.Join(test.arguments, " "), func(t *testing.T) {
			args := append([]string{filepath.Join("..", "..", "examples", "lint-metrics.py")}, test.arguments...)
			command := exec.Command(python, args...)
			command.Stdin = strings.NewReader(string(data))
			output, _ := command.CombinedOutput()
			if command.ProcessState.ExitCode() != test.code || !strings.Contains(string(output), test.message) {
				t.Fatalf("lint exit=%d want=%d, output=%s", command.ProcessState.ExitCode(), test.code, output)
			}
		})
	}
	for _, damaged := range []string{
		strings.Replace(string(data), `"status":"complete"`, `"status":"partial"`, 1),
		strings.Replace(string(data), `"after_name":"`, `"after_name":"missing-`, 1),
		strings.ReplaceAll(string(data), `"cyclomatic":{`, `"missing_metric":{`),
	} {
		command := exec.Command(python, filepath.Join("..", "..", "examples", "lint-metrics.py"), "--max-complexity=10")
		command.Stdin = strings.NewReader(damaged)
		output, _ := command.CombinedOutput()
		if command.ProcessState.ExitCode() != 2 {
			t.Fatalf("missing facts passed lint: exit=%d output=%s", command.ProcessState.ExitCode(), output)
		}
	}
}

func TestMetrics_lint_staged_change_set(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("the optional lint example needs Python 3")
	}
	root := git_repository(t)
	writeFile(t, root, "a.js", "function f(x) { return x; }\n")
	writeFile(t, root, "gone.js", "function gone() {}\n")
	git_test(t, root, "add", "--all")
	git_test(t, root, "commit", "--quiet", "-m", "initial")
	writeFile(t, root, "a.js", "function f(x) { return x ? 1 : 0; }\n")
	writeFile(t, root, "b.js", "// documentation\nconst b = x => x || 1;\n")
	if err := os.Remove(filepath.Join(root, "gone.js")); err != nil {
		t.Fatal(err)
	}
	git_test(t, root, "add", "--all")
	_, data, err := change_set_output(t, "--metrics", "--staged", "--format=json", "--root", root)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		argument string
		code     int
		messages []string
	}{
		{"--max-complexity=2", 0, nil},
		{"--max-complexity=1", 1, []string{"a.js:1:", "b.js:2:", "complexity 2 exceeds 1"}},
		{"--max-increase=0", 1, []string{"a.js:1:", "complexity increased by 1"}},
		{"--max-comment-chars=3", 1, []string{"b.js:1:", "prefix comment has"}},
	} {
		command := exec.Command(python, filepath.Join("..", "..", "examples", "lint-metrics.py"), test.argument)
		command.Stdin = strings.NewReader(string(data))
		output, _ := command.CombinedOutput()
		if command.ProcessState.ExitCode() != test.code {
			t.Fatalf("lint exit=%d want=%d, output=%s", command.ProcessState.ExitCode(), test.code, output)
		}
		for _, message := range test.messages {
			if !strings.Contains(string(output), message) {
				t.Fatalf("lint feedback missing %q: %s", message, output)
			}
		}
	}
	for _, damage := range []func(map[string]any){
		func(report map[string]any) { report["status"] = "partial" },
		func(report map[string]any) { delete(report, "files") },
		func(report map[string]any) {
			pair := report["files"].([]any)[0].(map[string]any)["comparison"].(map[string]any)
			pair["after"].(map[string]any)["status"] = "partial"
		},
		func(report map[string]any) {
			pair := report["files"].([]any)[0].(map[string]any)["comparison"].(map[string]any)
			pair["after"] = nil
		},
	} {
		var report map[string]any
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		damage(report)
		damaged, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(python, filepath.Join("..", "..", "examples", "lint-metrics.py"), "--max-complexity=10")
		command.Stdin = strings.NewReader(string(damaged))
		output, _ := command.CombinedOutput()
		if command.ProcessState.ExitCode() != 2 {
			t.Fatalf("incomplete change set passed lint: exit=%d output=%s", command.ProcessState.ExitCode(), output)
		}
	}
}
