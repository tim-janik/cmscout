package metrics

import (
	"fmt"
	"reflect"
	"testing"
)

func TestCyclomatic_rules(t *testing.T) {
	for _, test := range []struct {
		path, source string
		want         []int
	}{
		{"a.js", `function f(a,b) { if(a && b) {} else if (b) {} else {} for (;;) { break; } while(a){} do{}while(b); return a ? b : 0; }`, []int{8}},
		{"a.js", `function f(a) { switch(a) { case 1: case 2: break; default: break; } try {} catch(e) {} finally {} }`, []int{4}},
		{"a.js", `function f({a=1}={}, x=1) { let {y=2} = {}; x &&= y; x ||= y; x ??= y; return x?.a?.(y) ?? y; }`, []int{11}},
		{"a.ts", `function f({a=1}={}, x=1) { let {y=2} = {}; x &&= y; x ||= y; x ??= y; return x?.a?.(y) ?? y; }`, []int{11}},
		{"a.jsx", `function App({x}) { return <div>{x && <button onClick={() => x ? 1 : 0}/>}</div>; }`, []int{2, 2}},
		{"a.tsx", `function App({x}) { return <div>{x && <button onClick={() => x ? 1 : 0}/>}</div>; }`, []int{2, 2}},
		{"a.go", "package p\nfunc f(a bool, ch chan int, x any) { if a {} else if !a {} ; for {} ; for range ch {} ; switch x { case 1,2: case 3: default: }; switch x.(type) { case int,string: default: }; select {case <- ch: default:} }", []int{9}},
		{"a.go", "package p\nfunc f(a,b bool) bool { return a && b || !a }", []int{3}},
		{"a.c", "int f(int a) { if(a && 1){} else if(a){} for(;;){} while(a){} do{}while(a); switch(a){case 1: case 2: break; default:break;} return a ? 1:0; }", []int{10}},
		{"a.cc", `int f(int a) { if constexpr (true) {} for(auto x : xs) {} try {} catch(...) {} return a || 1; }`, []int{5}},
		{"a.sh", "f() { if [[ a && b ]]; then :; elif false; then :; else :; fi; true && false || :; echo ${x:-fallback}; ((x ? y : z)); case $x in a|b) :;; *) :;; esac; }", []int{9}},
		{"a.sh", "f() { for x in a; do :; done; until false; do :; done; select x in a; do :; done; for ((x=0;x<3;x++)); do :; done; echo ${x:=a} ${x:+a} ${x:?error}; }", []int{8}},
		{"a.sh", "f() { case $x in '*') :;; *) :;; a) :;; esac; }", []int{4}},
		{"a.sh", "f() { :; } >\"${x:-out}\"", []int{2}},
		{"a.js", "function f() { const s = 'if && ?'; /* if (x) {} */ return `text ${x && y}`; }", []int{2}},
		{"a.c", "#define F(x) ((x) ? 1 : 0)\nint f(int x) {\n#if FOO && BAR\nif(x) {}\n#else\nwhile(x) {}\n#endif\nreturn sizeof(x && 1); }", []int{3}},
		{"a.cc", `int f(int x) { return sizeof(x && 1) + noexcept(x || 1); }`, []int{1}},
	} {
		t.Run(test.path+test.source, func(t *testing.T) {
			ast := source_tree(t, test.path, test.source)
			inv := discover(ast, Options{Namespace: "test", Path: test.path})
			inv.count_complexity(ast.RootNode())
			var got []int
			for _, unit := range inv.units {
				metric := inv.components[unit.index].Cyclomatic
				if metric.Value == nil || metric.Status != "complete" {
					t.Fatalf("unavailable metric: %+v", metric)
				}
				got = append(got, *metric.Value)
				if *metric.Value != len(metric.Decisions)+1 {
					t.Fatalf("evidence does not sum to score: %+v", metric)
				}
				seen := map[string]bool{}
				for _, event := range metric.Decisions {
					key := fmt.Sprintf("%d/%s", event.Span.StartByte, event.Rule)
					if seen[key] || event.Span.EndByte <= event.Span.StartByte {
						t.Fatalf("duplicate or empty evidence: %+v", event)
					}
					seen[key] = true
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("scores=%v want=%v, units=%+v", got, test.want, inv.components)
			}
		})
	}
}

func TestCyclomatic_ownership(t *testing.T) {
	for _, test := range []struct {
		path, source string
		want         []int
	}{
		{"a.js", `function outer(x) { if(x){} const f = (a = x ? 1 : 2) => { if(a){} if(x){} }; }`, []int{2, 5}},
		{"a.go", "package p\nfunc outer(x bool) { if x {} ; f := func() { if x {} ; if x {} }; _ = f }", []int{2, 3}},
		{"a.cc", `void outer(int x) { if(x){} auto f = [v = x ? 1 : 0]() { if(v){} if(v){} }; }`, []int{3, 3}},
		{"a.js", `function outer(x) { const o = { [x ? 'a':'b'](y) { return y || x; } }; }`, []int{2, 2}},
		{"a.js", `function outer(x) { class A { [x ? 'a':'b']() { return x && 1; } value = x ? 1 : 0; callback = () => x ? 1 : 0; static { if(x) {} } } }`, []int{2, 2, 2}},
		{"a.cc", `struct A { A(int x) : value(x ? 1 : 0) {} int value; }; int f(int x = 1 ? 2 : 3) { return x; }`, []int{2, 1}},
		{"a.cc", `int f(int x = [] { if(true) {} return 1; }()) { return x; }`, []int{1, 2}},
	} {
		t.Run(test.source, func(t *testing.T) {
			ast := source_tree(t, test.path, test.source)
			inv := discover(ast, Options{Namespace: "test", Path: test.path})
			inv.count_complexity(ast.RootNode())
			var got []int
			for _, unit := range inv.units {
				got = append(got, *inv.components[unit.index].Cyclomatic.Value)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ownership scores=%v want=%v", got, test.want)
			}
		})
	}
}

func TestCyclomatic_c_vla_is_unavailable(t *testing.T) {
	for _, source := range []string{
		"void f(int n) { int a[n]; }\n",
		"int f(int n, int a[n]) { return sizeof(a); }\n",
		"int f(int n) { return sizeof(int[n]); }\n",
	} {
		ast := source_tree(t, "a.c", source)
		inv := discover(ast, Options{Namespace: "test", Path: "a.c"})
		inv.count_complexity(ast.RootNode())
		metric := inv.components[inv.units[0].index].Cyclomatic
		if metric.Value != nil || metric.Status != "unsupported" {
			t.Fatalf("VLA counted as supported: %+v", metric)
		}
	}
}
