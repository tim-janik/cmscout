package metrics

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"cmscout/pkg/analysis"
)

func measurement(t *testing.T, path, source string) *Snapshot {
	t.Helper()
	result, err := Measure(source_tree(t, path, source), Options{Namespace: "test", Path: path, Explain: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func functions_by_name(snapshot *Snapshot) map[string]Component {
	functions := map[string]Component{}
	for _, component := range snapshot.Components {
		if component.FunctionMetrics != nil {
			functions[component.Name] = component
		}
	}
	return functions
}

func TestComments_all_languages(t *testing.T) {
	for _, test := range []struct{ path, prefix, body string }{
		{"a.js", "// π\n// docs", "function f() {\n  // inner\n  // more\n  return '/* not a comment */';\n}"},
		{"a.jsx", "// π\n// docs", "const f = () => {\n  // inner\n  // more\n  return <div>/* not a comment */</div>;\n};"},
		{"a.ts", "// π\n// docs", "export function f() {\n  // inner\n  // more\n  return 0;\n}"},
		{"a.tsx", "// π\n// docs", "const f = () => {\n  // inner\n  // more\n  return <div>text</div>;\n};"},
		{"a.go", "// π\n// docs", "func f() {\n  // inner\n  // more\n  _ = `/* not a comment */`\n}"},
		{"a.go", "// π\n// docs", "var f = func() {\n  // inner\n  // more\n  _ = 1\n}"},
		{"a.c", "// π\n// docs", "int f(void) {\n  // inner\n  // more\n  return 0;\n}"},
		{"a.cc", "// π\n// docs", "auto f = [] {\n  // inner\n  // more\n  return R\"(// not a comment)\";\n};"},
		{"a.sh", "# π\n# docs", "f() {\n  # inner\n  # more\n  echo '# not a comment';\n}"},
	} {
		t.Run(test.path, func(t *testing.T) {
			source := test.prefix + "\n" + test.body
			if test.path == "a.go" {
				source = "package p\n\n" + source
			}
			snapshot := measurement(t, test.path, source)
			function := functions_by_name(snapshot)["f"]
			if snapshot.Status != "complete" || function.FunctionMetrics == nil {
				t.Fatalf("incomplete: %+v", snapshot)
			}
			prefix := function.PrefixComment
			if prefix == nil || prefix.Chars == nil || *prefix.Chars != utf8.RuneCountInString(test.prefix) || prefix.Lines != 2 {
				t.Fatalf("wrong prefix: %+v", prefix)
			}
			if len(function.InlineComments) != 1 || function.InlineComments[0].Lines != 2 {
				t.Fatalf("wrong inline comments: %+v", function.InlineComments)
			}
			if len(snapshot.Comments) != 2 {
				t.Fatalf("literal text counted as comment: %+v", snapshot.Comments)
			}
		})
	}
}

func TestComments_object_literals_keep_callable_owner(t *testing.T) {
	for _, test := range []struct{ path, source string }{
		{"a.js", "function f() { const o = {\n // own\n value: 1,\n // callback\n run: () => 1\n}; }"},
		{"a.go", "package p\nfunc f() { _ = T{\n // own\n Value: 1,\n // callback\n Run: func(){},\n} }"},
	} {
		snapshot := measurement(t, test.path, test.source)
		function := functions_by_name(snapshot)["f"]
		if len(function.InlineComments) != 1 || *function.InlineComments[0].Chars != len("// own") {
			t.Fatalf("object comment lost its callable owner: %+v", function.InlineComments)
		}
	}
}

func TestComments_nested_ownership(t *testing.T) {
	source := "// outer docs\nfunction outer() {\n  // own\n\n  // inner docs\n  const inner = () => {\n    /* inside */\n    return 1; // trailing\n  };\n  // last\n}\n"
	snapshot := measurement(t, "a.js", source)
	functions := functions_by_name(snapshot)
	outer, inner := functions["outer"], functions["inner"]
	if len(outer.InlineComments) != 2 || len(inner.InlineComments) != 2 {
		t.Fatalf("wrong ownership: outer=%+v inner=%+v", outer.InlineComments, inner.InlineComments)
	}
	if inner.PrefixComment == nil || *inner.PrefixComment.Chars != len("// inner docs") ||
		outer.Size.Bytes <= inner.Size.Bytes || !reflect.DeepEqual(outer.InnerFunctions, []string{inner.QualifiedName}) {
		t.Fatalf("wrong nested record: %+v", inner)
	}
	for i, comment := range snapshot.Comments {
		if i > 0 && snapshot.Comments[i-1].Span.EndByte > comment.Span.StartByte {
			t.Fatal("comment counted for more than one owner")
		}
	}
}

func TestComments_boundaries(t *testing.T) {
	for _, test := range []struct {
		source string
		prefix int
		inline []int
	}{
		{"// detached\n\nfunction f() {}", 0, []int{}},
		{"let x = 1; // trailing\nfunction f() {}", 0, []int{}},
		{"/* same line */ function f() {}", 0, []int{}},
		{"// docs\r\n// π\r\nfunction f() {\r\n  // one\r\n  // two\r\n}", 2, []int{2}},
		{"function f() {\n  /* one\n     two */\n  // three\n  // four\n  return 1; // five\n  // six\n}", 0, []int{2, 2, 1, 1}},
		{"// docs\nconst f = function named() {};", 1, []int{}},
		{"// docs\nexport default function() {}", 1, []int{}},
	} {
		t.Run(test.source, func(t *testing.T) {
			snapshot := measurement(t, "a.js", test.source)
			var function Component
			for _, component := range snapshot.Components {
				if component.FunctionMetrics != nil {
					function = component
					break
				}
			}
			if function.PrefixComment == nil || function.PrefixComment.Lines != test.prefix {
				t.Fatalf("prefix=%+v want lines=%d", function.PrefixComment, test.prefix)
			}
			lines := []int{}
			for _, comment := range function.InlineComments {
				lines = append(lines, comment.Lines)
			}
			if !reflect.DeepEqual(lines, test.inline) {
				t.Fatalf("inline lines=%v want=%v", lines, test.inline)
			}
		})
	}
}

func TestComments_ambiguous_and_directives(t *testing.T) {
	snapshot := measurement(t, "a.js", "// which one?\nconst a = () => 1, b = () => 2;\n")
	if snapshot.Status != "partial" || len(snapshot.Comments) != 1 || snapshot.Comments[0].Ownership != "ambiguous" {
		t.Fatalf("ambiguous prefix was silently assigned: %+v", snapshot)
	}
	for _, function := range functions_by_name(snapshot) {
		if function.PrefixComment != nil || function.CommentStatus != "ambiguous" || function.Cyclomatic.Value == nil {
			t.Fatalf("wrong ambiguous record: %+v", function)
		}
	}
	for _, test := range []struct{ path, source string }{
		{"a.sh", "#!/bin/bash\nf() { cat <<'EOF'\n# not a comment\nEOF\n}\n"},
		{"a.go", "package p\n//go:noinline\nfunc f() {}\n"},
		{"a.js", "#!/usr/bin/env node\nfunction f() {}\n"},
	} {
		snapshot := measurement(t, test.path, test.source)
		function := functions_by_name(snapshot)["f"]
		if function.PrefixComment.Lines != 0 || len(snapshot.Comments) != 1 || snapshot.Comments[0].Role != "directive" {
			t.Fatalf("directive counted as documentation: %+v", snapshot.Comments)
		}
	}
}

func TestMeasure_damaged_source(t *testing.T) {
	for _, source := range []string{"function f() {", "function f() { return '\xff'; }"} {
		ast, err := analysis.Parse(context.Background(), []byte(source), "a.js")
		if err != nil {
			t.Fatal(err)
		}
		defer ast.Close()
		snapshot, err := Measure(ast, Options{Namespace: "test", Path: "a.js"})
		if err != nil || snapshot.Status != "partial" || len(snapshot.Diagnostics) == 0 {
			t.Fatalf("damaged source reported complete: %+v error=%v", snapshot, err)
		}
		for _, function := range functions_by_name(snapshot) {
			if function.Cyclomatic.Value != nil || function.PrefixComment != nil || function.InlineComments != nil {
				t.Fatalf("damaged source got known metrics: %+v", function)
			}
		}
	}
}

func TestMeasure_determinism_and_lifetime(t *testing.T) {
	source := "  \n// doc\nfunction f(x) { return x ? 1 : 0; }\n\n"
	ast := source_tree(t, "a.js", source)
	options := Options{Namespace: "test", Path: "a.js", Explain: true}
	first, err := Measure(ast, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Measure(ast, options)
	if err != nil {
		t.Fatal(err)
	}
	ast.Close()
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) || !json.Valid(a) || strings.Contains(string(a), "StartByte") {
		t.Fatalf("unstable JSON: %s\n%s", a, b)
	}
	if first.Components[0].Size.Bytes != uint(len(source)) || first.Components[0].Span.StartByte != 0 {
		t.Fatalf("file excludes surrounding whitespace: %+v", first.Components[0])
	}
	if _, err := Measure(ast, options); err == nil {
		t.Fatal("closed tree accepted")
	}
}
