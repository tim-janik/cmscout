// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package lang

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		path string
		want string // expected language name; "" = not recognized
	}{
		{"a.ts", "ts"}, {"a.tsx", "tsx"}, {"a.js", "js"}, {"a.mjs", "js"},
		{"a.cjs", "js"}, {"a.jsx", "jsx"}, {"a.go", "go"}, {"a.sh", "bash"},
		{"a.bash", "bash"},
		{"A.TS", "ts"},         // extension matching is case-insensitive
		{"dir/sub/a.go", "go"}, // full path, not just basename
		{"a.txt", ""},          // unknown extension
		{"Makefile", ""},       // no extension

		// C / C++: .c → c; the C++ extensions and `.h` → cpp.
		{"a.c", "c"},
		{"a.h", "cpp"},
		{"a.cc", "cpp"}, {"a.cpp", "cpp"}, {"a.cxx", "cpp"}, {"a.c++", "cpp"},
		{"a.hh", "cpp"}, {"a.hpp", "cpp"}, {"a.hxx", "cpp"}, {"a.h++", "cpp"},
		{"a.tcc", "cpp"},
		{"a.C", "cpp"},       // legacy capital-extension convention (raw-case special case)
		{"a.H", "cpp"},       // lowercased by ToLower → `.h` → cpp
		{"dir/x.CPP", "cpp"}, // case-insensitive beyond the raw `.C` special case
		{"a.h.in", ""},       // compound extension, not recognized
	}
	for _, tt := range tests {
		got, ok := Detect(tt.path)
		if tt.want == "" {
			if ok {
				t.Errorf("Detect(%q) = %q, want not recognized", tt.path, got.Name)
			}
			continue
		}
		if !ok || got.Name != tt.want {
			t.Errorf("Detect(%q) = %q (ok=%v), want %q", tt.path, got.Name, ok, tt.want)
		}
	}
}
