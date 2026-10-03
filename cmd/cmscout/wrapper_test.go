// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"os/exec"
	"path/filepath"
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
