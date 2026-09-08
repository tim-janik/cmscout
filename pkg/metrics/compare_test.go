package metrics

import (
	"encoding/json"
	"strings"
	"testing"
)

func compare_sources(t *testing.T, path, before, after string) *Comparison {
	t.Helper()
	result, err := Compare(measurement(t, path, before), measurement(t, path, after))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func change_named(t *testing.T, result *Comparison, name string) Change {
	t.Helper()
	for _, snapshot := range []*Snapshot{result.After, result.Before} {
		for _, component := range snapshot.Components {
			if component.Name != name {
				continue
			}
			for _, change := range result.Changes {
				if change.AfterName != nil && *change.AfterName == component.QualifiedName ||
					change.BeforeName != nil && *change.BeforeName == component.QualifiedName {
					return change
				}
			}
		}
	}
	t.Fatalf("no change named %q: %+v", name, result.Changes)
	return Change{}
}

func TestCompare_unchanged_and_snapshot_integrity(t *testing.T) {
	source := "// doc\nfunction f() { call(() => 1, () => 1); }\n"
	before, after := measurement(t, "before.js", source), measurement(t, "after.js", source)
	old_json, _ := json.Marshal(before)
	new_json, _ := json.Marshal(after)
	first, err := Compare(before, after)
	if err != nil || first.Status != "complete" || len(first.Changes) != 0 {
		t.Fatalf("unchanged inputs: %+v %v", first, err)
	}
	second, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("comparison is not deterministic")
	}
	a, _ = json.Marshal(before)
	b, _ = json.Marshal(after)
	if string(a) != string(old_json) || string(b) != string(new_json) {
		t.Fatal("comparison mutated a snapshot")
	}
}

func TestCompare_nested_changes_and_deltas(t *testing.T) {
	result := compare_sources(t, "a.js", "function outer(x) { const inner = () => x; }",
		"function outer(x) { const inner = () => x ? 1 : 0; }")
	inner, outer := change_named(t, result, "inner"), change_named(t, result, "outer")
	if result.Status != "complete" || inner.Delta.Cyclomatic == nil || *inner.Delta.Cyclomatic != 1 ||
		!inner.DirectChanged || !inner.CodeChanged || inner.SignatureChanged {
		t.Fatalf("wrong inner change: %+v", inner)
	}
	if outer.DirectChanged || !outer.DescendantChanged || outer.Delta.Cyclomatic == nil || *outer.Delta.Cyclomatic != 0 {
		t.Fatalf("inner edit charged to outer: %+v", outer)
	}
	if len(inner.BeforeRanges) == 0 || len(inner.AfterRanges) == 0 {
		t.Fatal("missing source ranges")
	}
}

func TestCompare_comments_and_formatting(t *testing.T) {
	for _, test := range []struct {
		before, after                    string
		prefix, inline, formatting, code bool
	}{
		{"// old\nfunction f() {}", "// new\nfunction f() {}", true, false, false, false},
		{"function f() { /* old */ }", "function f() { /* new */ }", false, true, false, false},
		{"function f(x){return x+1;}", "function f(x) { return x + 1; }", false, false, true, false},
		{`function f(){return "a  b";}`, `function f(){return "a b";}`, false, false, false, true},
	} {
		t.Run(test.after, func(t *testing.T) {
			result := compare_sources(t, "a.js", test.before, test.after)
			change := change_named(t, result, "f")
			if change.PrefixChanged != test.prefix || change.InlineChanged != test.inline ||
				change.FormattingChanged != test.formatting || change.CodeChanged != test.code ||
				!change.DirectChanged || change.Delta.Cyclomatic == nil || *change.Delta.Cyclomatic != 0 {
				t.Fatalf("wrong classification: %+v", change)
			}
		})
	}
}

func TestCompare_directives_are_not_inline_metrics(t *testing.T) {
	result := compare_sources(t, "a.js", "function f(){\n// @ts-ignore\nreturn 1;\n}",
		"function f(){\n// @ts-expect-error\nreturn 1;\n}")
	change := change_named(t, result, "f")
	if change.InlineChanged || change.FormattingChanged || !change.DirectChanged ||
		change.Delta.InlineChars == nil || *change.Delta.InlineChars != 0 {
		t.Fatalf("directive change was treated as inline documentation: %+v", change)
	}
}

func TestCompare_added_removed_and_moves(t *testing.T) {
	added := compare_sources(t, "a.cc", "", "int f() { return 1; }")
	change := change_named(t, added, "f")
	if !change.Added || change.BeforeName != nil || change.Delta.Cyclomatic != nil || change.Delta.Status != "added" {
		t.Fatalf("addition has a fabricated old value: %+v", change)
	}
	removed := compare_sources(t, "a.cc", "int f() { return 1; }", "")
	change = change_named(t, removed, "f")
	if !change.Removed || change.AfterName != nil || change.Delta.Cyclomatic != nil {
		t.Fatalf("removal has a fabricated new value: %+v", change)
	}
	moved := compare_sources(t, "a.js", "function a(){}\nfunction b(){}\n", "function b(){}\nfunction a(){}\n")
	if !change_named(t, moved, "a").Moved || !change_named(t, moved, "b").Moved {
		t.Fatalf("reordering not detected: %+v", moved.Changes)
	}
	shifted := compare_sources(t, "a.js", "function f() {}", "\nfunction f() {}")
	for _, change := range shifted.Changes {
		if change.Moved {
			t.Fatalf("line shift called a move: %+v", change)
		}
	}
}

func TestCompare_renamed_parent_and_inserted_lambda(t *testing.T) {
	renamed := compare_sources(t, "a.js", "class Old { run(x) { return x ? 1 : 0; } }",
		"class New { run(x) { return x ? 1 : 0; } }")
	if !change_named(t, renamed, "New").Renamed {
		t.Fatalf("parent rename lost: %+v", renamed.Changes)
	}
	method := change_named(t, renamed, "run")
	if method.BeforeName == nil || method.AfterName == nil || !method.NameChanged || method.Moved || method.Delta.Cyclomatic == nil {
		t.Fatalf("child of renamed parent lost: %+v", method)
	}
	result := compare_sources(t, "a.js", "function f(){ call(() => 7, () => 9); }",
		"function f(){ call(() => 0, () => 7, () => 9); }")
	added, renumbered := 0, 0
	for _, change := range result.Changes {
		if change.AfterKind != nil && *change.AfterKind == "arrow_function" {
			if change.Added {
				added++
			} else {
				renumbered++
				if change.Delta.Cyclomatic == nil || *change.Delta.Cyclomatic != 0 || change.CodeChanged || change.Moved {
					t.Fatalf("inserted sibling changed old lambda: %+v", change)
				}
			}
		}
	}
	if result.Status != "complete" || added != 1 || renumbered != 2 {
		t.Fatalf("wrong lambda matches: %+v", result.Changes)
	}
}

func TestCompare_ambiguity_and_language_changes(t *testing.T) {
	ambiguous := compare_sources(t, "a.js", "function f(){ call(() => 1, () => 1); }",
		"function f(){ call(() => 1, () => 1, () => 1); }")
	if ambiguous.Status != "partial" || len(ambiguous.Diagnostics) == 0 {
		t.Fatalf("ambiguous match reported complete: %+v", ambiguous)
	}
	for _, change := range ambiguous.Changes {
		if change.Match.Status == "ambiguous" && change.Delta.Cyclomatic != nil {
			t.Fatalf("ambiguous match got a trusted delta: %+v", change)
		}
	}
	before := measurement(t, "a.js", "function f(x) { return x; }")
	after := measurement(t, "a.ts", "function f(x: number) { return x; }")
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	change := change_named(t, result, "f")
	if change.Delta.Status != "incomparable" || change.Delta.Cyclomatic != nil {
		t.Fatalf("language change got a comparable delta: %+v", change)
	}
}

func TestCompare_file_changes_and_comment_arrays(t *testing.T) {
	result := compare_sources(t, "a.js", "const answer = 1;", "const answer = 2;")
	file := change_named(t, result, "a.js")
	if !file.CodeChanged || len(file.BeforeRanges) == 0 || len(file.AfterRanges) == 0 {
		t.Fatalf("unmeasured source change disappeared: %+v", file)
	}
	result = compare_sources(t, "a.js", "function f(){}", "function f(){}\n")
	if len(result.Changes) == 0 || len(result.AfterRanges) == 0 {
		t.Fatal("final newline change disappeared")
	}
	result = compare_sources(t, "a.js", "function f(){\n /* one */\n\n /* two */\n}", "function f(){\n /* added */\n\n /* one */\n\n /* two */\n}")
	change := change_named(t, result, "f")
	if change.Delta.InlineCommentCount == nil || *change.Delta.InlineCommentCount != 1 ||
		change.Delta.InlineChars == nil || *change.Delta.InlineChars != len("/* added */") {
		t.Fatalf("comment arrays were subtracted by position: %+v", change)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "\"BeforeName\"") || !json.Valid(data) {
		t.Fatal("invalid comparison schema")
	}
}

func TestCompare_method_and_capture_owners(t *testing.T) {
	result := compare_sources(t, "a.go", "package p\ntype A struct{}\nfunc (a A) Run(x bool) { }",
		"package p\ntype A struct{}\nfunc (a A) Run(x bool) { if x {} }")
	for _, name := range []string{"a.go", "A"} {
		change := change_named(t, result, name)
		if change.DirectChanged || !change.DescendantChanged {
			t.Fatalf("method change charged to %s: %+v", name, change)
		}
	}
	result = compare_sources(t, "a.cc", "void f(int x) { auto g = [a=x]() { return a; }; }",
		"void f(int x) { auto g = [a=x ? 1 : 0]() { return a; }; }")
	change := change_named(t, result, "f")
	if !change.DirectChanged || change.Delta.Cyclomatic == nil || *change.Delta.Cyclomatic != 1 {
		t.Fatalf("capture decision did not affect outer function: %+v", change)
	}
	result = compare_sources(t, "a.go", "package p\ntype A struct{}\nfunc (a A) Run() {}\nfunc f() {}",
		"package p\ntype A struct{}\nfunc f() {}\nfunc (a A) Run() {}")
	if !change_named(t, result, "Run").Moved || !change_named(t, result, "f").Moved {
		t.Fatalf("move across different logical owners missed: %+v", result.Changes)
	}
}

func TestCompare_ambiguous_parent_has_no_child_delta(t *testing.T) {
	before := "function f(){ call(() => () => 1, () => () => 1); }"
	after := "function f(){ call(() => () => 1, () => () => 1, () => () => 1); }"
	result := compare_sources(t, "a.js", before, after)
	for _, change := range result.Changes {
		if change.AfterKind != nil && *change.AfterKind == "arrow_function" && change.BeforeName != nil &&
			(change.Match.Status != "ambiguous" || change.Delta.Cyclomatic != nil) {
			t.Fatalf("ambiguous parent gave a trusted child delta: %+v", change)
		}
	}
}

func TestCompare_unchanged_duplicates_do_not_block_other_edits(t *testing.T) {
	before := "function f(){\n call(() => 1, () => 1);\n return 1;\n}"
	after := "function f(){\n call(() => 1, () => 1);\n return 2;\n}"
	result := compare_sources(t, "a.js", before, after)
	if result.Status != "complete" || len(result.Diagnostics) != 0 {
		t.Fatalf("unchanged duplicate callbacks blocked an unrelated edit: %+v", result.Diagnostics)
	}
}

func TestCompare_overloads_and_shadowed_bindings(t *testing.T) {
	result := compare_sources(t, "a.cc", "int f(int x) { return x; }\nint f(double x) { return x; }",
		"int f(double x) { return x; }\nint f(int renamed) { if(renamed){} return renamed; }")
	matched, increased := 0, 0
	for _, change := range result.Changes {
		if change.AfterKind != nil && *change.AfterKind == "function" {
			matched++
			if change.Added || change.Removed || change.Delta.Cyclomatic == nil {
				t.Fatalf("overload lost its match: %+v", change)
			}
			increased += *change.Delta.Cyclomatic
		}
	}
	if result.Status != "complete" || matched != 2 || increased != 1 {
		t.Fatalf("wrong overload results: %+v", result.Changes)
	}
	result = compare_sources(t, "a.js", "function outer(){ {const f=()=>1;} {const f=()=>2;} }",
		"function outer(){ {const f=()=>2;} {const f=()=>1;} }")
	for _, change := range result.Changes {
		if change.AfterKind != nil && *change.AfterKind == "arrow_function" &&
			(change.Added || change.Removed || change.CodeChanged || change.Delta.Cyclomatic == nil || *change.Delta.Cyclomatic != 0) {
			t.Fatalf("shadowed binding matched by ordinal: %+v", change)
		}
	}
}
