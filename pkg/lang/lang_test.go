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
