package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilter(t *testing.T) {
	for _, test := range []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.go", "a.go", true},
		{"**/*.go", "src/internal/a.go", true},
		{"*.go", "src/a.go", false},
		{"src/**/a?.[ch]", "src/a1.c", true},
		{"src/**/a?.[ch]", "src/lib/a1.h", true},
		{"src/**/a?.[ch]", "src/lib/aa.cc", false},
		{"vendor/**", "vendor/sub/a.go", true},
		{"**", "line\nbreak.js", true},
	} {
		if got := match_path(test.pattern, test.name); got != test.want {
			t.Errorf("match_path(%q, %q) = %v, want %v", test.pattern, test.name, got, test.want)
		}
	}
	filter := Filter{Include: []string{"**/*.go", "**/*.cc"}, Exclude: []string{"vendor/**"}}
	if err := filter.Validate(); err != nil {
		t.Fatal(err)
	}
	if !filter.Allows("a.go") || !filter.Allows("src/a.cc") || filter.Allows("a.js") || filter.Allows("vendor/a.go") {
		t.Fatal("include/exclude selection failed")
	}
	if (Filter{Exclude: []string{"vendor"}}).Allows("vendor/src/a.go") {
		t.Fatal("explicit file input bypassed its directory exclusion")
	}
	for _, pattern := range []string{"", "/absolute", "["} {
		if err := (Filter{Include: []string{pattern}}).Validate(); err == nil {
			t.Errorf("accepted invalid pattern %q", pattern)
		}
	}
}

func TestScan_unreadable_directory_and_exclusions(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(blocked, 0755) })
	if directory, err := os.Open(blocked); err == nil {
		directory.Close()
		t.Skip("test needs directory permission enforcement")
	}
	result, err := Scan([]string{root}, root, Filter{})
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "blocked" {
		t.Fatalf("unreadable directory was silently omitted: %+v %v", result, err)
	}
	result, err = Scan([]string{root, blocked}, root, Filter{Exclude: []string{"blocked/**"}})
	if err != nil || len(result.Diagnostics) != 0 || len(result.Skipped) != 1 || result.Skipped[0].Path != "blocked" {
		t.Fatalf("excluded directory was visited: %+v %v", result, err)
	}
}
