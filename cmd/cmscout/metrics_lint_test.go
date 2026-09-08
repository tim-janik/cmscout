package main

import (
	"encoding/json"
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
