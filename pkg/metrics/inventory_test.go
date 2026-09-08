package metrics

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"cmscout/pkg/analysis"
	"cmscout/pkg/parser"
)

func source_tree(t *testing.T, path, source string) *parser.AST {
	t.Helper()
	ast, err := analysis.Parse(context.Background(), []byte(source), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ast.Close)
	if ast.ErrorCount() != 0 {
		t.Fatalf("invalid fixture: %+v\n%s", ast.Diagnostics(), ast.RootNode().ToSexp())
	}
	return ast
}

func inventory_for(t *testing.T, path, source string) *inventory {
	t.Helper()
	return discover(source_tree(t, path, source), Options{Namespace: "project", Path: path})
}

func TestInventory_all_languages(t *testing.T) {
	for _, test := range []struct {
		path, source string
		want         []string
	}{
		{"a.js", `function outer() { const f = x => x; return function() {}; }`, []string{"outer", "f", "function"}},
		{"a.jsx", `const App = () => <button onClick={() => 1}/>;`, []string{"App", "arrow_function"}},
		{"a.ts", `export function outer(): void { let f = (x: number) => x; }`, []string{"outer", "f"}},
		{"a.tsx", `const App = () => <button onClick={() => 1}/>;`, []string{"App", "arrow_function"}},
		{"a.go", "package p\nfunc outer() { f := func() { }; _ = f }\n", []string{"outer", "f"}},
		{"a.go", "package p\nvar f = func(){}\nvar g,h = func(){},func(){}\nvar (x = func(){})\nvar v = T{Run: func(){}}\n", []string{"f", "g", "h", "x", "Run"}},
		{"a.sh", "outer() { inner() { :; }; };\n", []string{"outer", "inner"}},
		{"a.c", "#define F(x) ((x) ? 1 : 0)\nvoid declaration(void);\nint outer(int x) { return x; }", []string{"outer"}},
		{"a.cc", `void outer() { auto f = [] { return [] { }; }; }`, []string{"outer", "f", "lambda"}},
	} {
		t.Run(test.path, func(t *testing.T) {
			inv := inventory_for(t, test.path, test.source)
			var names []string
			seen := map[string]bool{}
			for _, component := range inv.components {
				if component.QualifiedName == "" || seen[component.QualifiedName] {
					t.Fatalf("empty or duplicate name: %+v", component)
				}
				seen[component.QualifiedName] = true
			}
			for _, unit := range inv.units {
				component := inv.components[unit.index]
				names = append(names, component.Name)
				if !seen[component.ParentName] {
					t.Fatalf("missing parent: %+v", component)
				}
			}
			if !reflect.DeepEqual(names, test.want) {
				t.Fatalf("functions=%v want=%v", names, test.want)
			}
		})
	}
}

func TestInventory_enclosing_components(t *testing.T) {
	for _, test := range []struct{ path, source, child, parent string }{
		{"a.go", "package p\nfunc (b *Box[T]) Run() {}\ntype Box[T any] struct{}\n", "Run", "Box"},
		{"a.cc", `namespace N { int W::f(int x) { return x; } class W { public: int f(int x); }; }`, "f", "W"},
		{"a.cc", `namespace N { class W { public: W() {} }; }`, "W", "W"},
		{"a.cc", `namespace A::B { class W { public: int f(); }; } int A::B::W::f() { return 0; }`, "f", "W"},
		{"a.cc", `namespace N { class W { public: int f(); }; int ::N::W::f() { return 0; } }`, "f", "W"},
		{"a.cc", `class W { public: operator bool() const { return true; } };`, "operator bool", "W"},
		{"a.ts", `class A { run() { class Local { run() {} } } }`, "run", "Local"},
		{"a.js", `function f() { const o = { run() { return 1; } }; }`, "run", "o"},
		{"a.go", "package p\nvar v = T{Run: func(){}}", "Run", "v"},
		{"a.cc", `int External::run() { return 0; }`, "run", "External"},
	} {
		t.Run(test.source, func(t *testing.T) {
			inv := inventory_for(t, test.path, test.source)
			for _, child := range inv.components {
				if child.FunctionMetrics == nil || child.Name != test.child {
					continue
				}
				for _, parent := range inv.components {
					if parent.QualifiedName == child.ParentName && parent.Name == test.parent {
						return
					}
				}
			}
			t.Fatalf("missing %s inside %s: %+v", test.child, test.parent, inv.components)
		})
	}
}

func TestNames_stability_and_overloads(t *testing.T) {
	before := inventory_for(t, "a.cc", `void first() {} void second() { call([]{}, []{}); }`)
	after := inventory_for(t, "a.cc", `void first() { call([]{}); } void second() { call([]{}, []{}); }`)
	var old_names, new_names []string
	for _, pair := range []struct {
		inv   *inventory
		names *[]string
	}{{before, &old_names}, {after, &new_names}} {
		for _, component := range pair.inv.components {
			if strings.Contains(component.QualifiedName, "function=second#") {
				*pair.names = append(*pair.names, component.QualifiedName)
			}
		}
	}
	if len(old_names) != 3 || !reflect.DeepEqual(old_names, new_names) {
		t.Fatalf("unrelated lambda changed names: %v -> %v", old_names, new_names)
	}
	a := inventory_for(t, "a.cc", `int f(int x = 1) { return 0; } int f(double y) { return 0; }`)
	b := inventory_for(t, "a.cc", `int f(int renamed = 9) { return 0; } int f(double z) { return 0; }`)
	for i := range a.units {
		old_name := a.components[a.units[i].index].QualifiedName
		new_name := b.components[b.units[i].index].QualifiedName
		if old_name != new_name {
			t.Fatalf("parameter name or default changed identity: %s -> %s", old_name, new_name)
		}
	}
	if a.components[a.units[0].index].QualifiedName == a.components[a.units[1].index].QualifiedName {
		t.Fatal("overloads share a name")
	}
}

func TestNames_escape_and_shadowing(t *testing.T) {
	inv := inventory_for(t, "dir/a::b.js", `function f() { { let x = () => 1; } { let x = () => 2; } }`)
	if len(inv.units) != 3 || inv.components[inv.units[1].index].QualifiedName == inv.components[inv.units[2].index].QualifiedName {
		t.Fatalf("shadowed bindings collided: %+v", inv.components)
	}
	name := inv.components[0].QualifiedName
	if !strings.Contains(name, "file=dir%2Fa%3A%3Ab.js") {
		t.Fatalf("unescaped file segment: %s", name)
	}
}
