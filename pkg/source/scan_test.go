package source

import "testing"

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
	for _, pattern := range []string{"", "/absolute", "["} {
		if err := (Filter{Include: []string{pattern}}).Validate(); err == nil {
			t.Errorf("accepted invalid pattern %q", pattern)
		}
	}
}
