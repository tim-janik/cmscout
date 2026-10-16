// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func wrapperScript() string {
	return filepath.Join("..", "..", "git-diff-wrapper.sh")
}

// TestGitDiffWrapper_UnmergedPath: during merge conflicts git passes a single
// path; the wrapper must announce it and succeed so `git diff` keeps working.
func TestGitDiffWrapper_UnmergedPath(t *testing.T) {
	out, err := exec.Command("bash", wrapperScript(), "src/file.c").CombinedOutput()
	if err != nil {
		t.Fatalf("unmerged path must exit 0: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "* Unmerged path src/file.c"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestGitDiffWrapper_RejectsOtherArgCounts(t *testing.T) {
	if err := exec.Command("bash", wrapperScript(), "a", "b").Run(); err == nil {
		t.Error("2 arguments must exit non-zero")
	}
}

func wrapper_command(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	source, err := os.ReadFile(wrapperScript())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "git-diff-wrapper.sh")
	if err := os.WriteFile(script, source, 0755); err != nil {
		t.Fatal(err)
	}
	binary := "#!/bin/bash\nprintf '%s\\0' \"$@\"\nexit \"${CMSCOUT_TEST_STATUS:-0}\"\n"
	if err := os.WriteFile(filepath.Join(dir, "cmscout"), []byte(binary), 0755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name != "NO_COLOR" && !strings.HasPrefix(name, "CMSCOUT_") {
			command.Env = append(command.Env, entry)
		}
	}
	return command
}

func TestGitDiffWrapper_ForwardsArguments(t *testing.T) {
	old_name := `src/old "file".go`
	old_path, new_path := "/tmp/old content", "/tmp/new content"
	for _, test := range []struct {
		name     string
		env      []string
		new_name string
		flags    []string
	}{
		{
			name:  "defaults",
			flags: []string{"--skip-unchanged", "--removed-style=red", "--ignore-all-space"},
		},
		{
			name: "environment",
			env: []string{"NO_COLOR=1", "CMSCOUT_KEEP_UNCHANGED=1", "CMSCOUT_ADDED_STYLE=green",
				"CMSCOUT_REMOVED_STYLE=white", "CMSCOUT_WORD_DIFF=1"},
			flags: []string{"--no-color", "--added-style=green", "--removed-style=white", "--word-diff", "--ignore-all-space"},
		},
		{
			name:     "rename",
			new_name: `dst/new "file".go`,
			flags:    []string{"--skip-unchanged", "--removed-style=red", "--ignore-all-space"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{old_name, old_path, "old-hash", "100644", new_path, "new-hash", "100644"}
			new_name := old_name
			if test.new_name != "" {
				new_name = test.new_name
				args = append(args, new_name)
			}
			command := wrapper_command(t, args...)
			command.Env = append(command.Env, test.env...)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("wrapper: %v\n%s", err, output)
			}
			got := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
			want := append(test.flags, "-B", old_path, "-A", new_path, "a/"+old_name, "b/"+new_name)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("arguments = %q; want %q", got, want)
			}
		})
	}
}

func TestGitDiffWrapper_PreservesCommandFailure(t *testing.T) {
	command := wrapper_command(t, "src/file.go", "old", "hash", "100644", "new", "hash", "100644")
	command.Env = append(command.Env, "CMSCOUT_TEST_STATUS=23")
	err := command.Run()
	exit_error, ok := err.(*exec.ExitError)
	if !ok || exit_error.ExitCode() != 23 {
		t.Fatalf("wrapper error = %v; want exit status 23", err)
	}
}
