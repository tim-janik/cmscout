// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"math/rand"
	"testing"
)

// naiveEditDistance is a straightforward O(n·m) restricted Damerau-Levenshtein
// (optimal string alignment) used only as a reference in tests.
func naiveEditDistance(a, b []byte) int {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
		dp[i][0] = i
	}
	for j := 0; j <= m; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d := min(dp[i-1][j]+1, dp[i][j-1]+1)
			d = min(d, dp[i-1][j-1]+cost)
			if cost == 1 && i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d = min(d, dp[i-2][j-2]+1)
			}
			dp[i][j] = d
		}
	}
	return dp[n][m]
}

func TestEditDistanceMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	alphabet := []byte("abAB12 _-")
	check := func(a, b string, maxDist int) {
		want := naiveEditDistance([]byte(a), []byte(b))
		got := editDistance([]byte(a), []byte(b), maxDist)
		if want <= maxDist && got != want {
			t.Errorf("editDistance(%q, %q, %d) = %d, want %d", a, b, maxDist, got, want)
		}
		if want > maxDist && got != maxDist+1 {
			t.Errorf("editDistance(%q, %q, %d) = %d, want %d (capped)", a, b, maxDist, got, maxDist+1)
		}
	}

	// Deterministic cases.
	cases := []struct{ a, b string }{
		{"", ""}, {"A", "A"}, {"", "1"}, {"1", ""}, {"A", "B"},
		{"AB", "BA"}, {"ABC", "CA"}, {"kitten", "sitting"}, {"123", "abc"},
		{"b-knob", "Knob"}, {"prop_changed", "propChanged"},
		{"class BKnob extends LitComponent", "function Knob(props)"},
		{"this.prop?.get_text()", "props.prop?.get_text()"},
		{"function relabel() { let tip = 'x'; }", "async function relabel() { let tip = 'y'; }"},
	}
	for _, c := range cases {
		full := naiveEditDistance([]byte(c.a), []byte(c.b))
		for _, maxDist := range []int{0, 1, 2, 3, 5, 10, full} {
			check(c.a, c.b, maxDist)
		}
	}

	// Randomized cases, including very different lengths.
	for iter := 0; iter < 20000; iter++ {
		na := rng.Intn(18)
		nb := rng.Intn(18)
		ba := make([]byte, na)
		bb := make([]byte, nb)
		for i := range ba {
			ba[i] = alphabet[rng.Intn(len(alphabet))]
		}
		for i := range bb {
			bb[i] = alphabet[rng.Intn(len(alphabet))]
		}
		a, b := string(ba), string(bb)
		full := naiveEditDistance(ba, bb)
		for _, maxDist := range []int{0, 1, 2, 3, full / 2, full} {
			check(a, b, maxDist)
		}
	}
}

func TestEditDistanceLarge(t *testing.T) {
	// A longer pair with heavy overlap: the banded DP must stay exact.
	a := "class BKnob extends LitComponent { constructor() { this.last_ = 0; } async relabel() { let tip = 'x'; } }"
	b := "function Knob(props) { let last_ = 0; async function relabel() { let tip = 'y'; } }"
	want := naiveEditDistance([]byte(a), []byte(b))
	got := editDistance([]byte(a), []byte(b), 1000)
	if got != want {
		t.Errorf("editDistance(%q, %q) = %d, want %d", a, b, got, want)
	}
}

func TestElementDistanceText(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain jsx unchanged", "<div class=\"x\">hi</div>", "<div class=\"x\">hi>"},
		{"lit interpolation braces", "<div>${x}</div>", "<div>{x}>"},
		{"lit boolean attr", "<div ?bidir=${d.bidir}>", "<div bidir={d.bidir}>"},
		{"lit property attr", "<div .prop=${x}>", "<div prop={x}>"},
		{"lit event attr", "<div @wheel=${f}>", "<div wheel={f}>"},
		{"jsx on-event attr", "<div onWheel={f}>", "<div wheel={f}>"},
		{"solid bool: attr", "<div bool:bidir={bidir()}>", "<div bidir={bidir()}>"},
		{"lit listener object", "<div @wheel=${{handleEvent: e => f (e), passive: false }}>",
			"<div wheel={e => f (e)}>"},
		{"quoted interpolation", "<div @dblclick=\"${Util.prevent_event}\">",
			"<div dblclick={Util.prevent_event}>"},
		{"self closing", "<br/>", "<br>"},
		{"closing tag", "<section></section>", "<section>>"},
		{"comparison gt preserved", "<div>{a > b}</div>", "<div>{a > b}>"},
	}
	for _, tc := range cases {
		if got := elementDistanceText(tc.in); got != tc.want {
			t.Errorf("%s: elementDistanceText(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestElementDistanceText_TemplateMatchesJSX(t *testing.T) {
	// The anklang sprite case: a Lit template element and the JSX element it
	// was converted to must normalize to the same shape (modulo the real
	// attribute changes), so the pair scores well above the threshold.
	lit := `<div id="sprite" ?bidir=${d.bidir}
    @wheel=${{handleEvent: e => t.wheel_event (e), passive: false }}
    @pointerdown="${t.pointerdown}"
    @dblclick="${Util.prevent_event}">
  </div>`
	jsx := `<div id="sprite" bool:bidir={bidir()} ref={sprite_el}
        onWheel={wheel_event}
        onPointerDown={pointerdown}
        onDblClick={Util.prevent_event}
      >
      </div>`
	a := elementDistanceText(lit)
	b := elementDistanceText(jsx)
	sim := bodySimilarity(a, b)
	if sim < 0.7 {
		t.Errorf("normalized Lit/JSX sprite similarity = %.3f, want >= 0.7\nlit=%q\njsx=%q", sim, a, b)
	}
	// The unrelated outer wrapper must stay clearly below the sprite pair.
	outer := `<div class="b-knob" aria-disabled={props.disabled || undefined} ref={root_el}>
      <div id="sprite" bool:bidir={bidir()} ref={sprite_el}
        onWheel={wheel_event}
        onPointerDown={pointerdown}
        onDblClick={Util.prevent_event}
      >
      </div>
    </div>`
	outerSim := bodySimilarity(a, elementDistanceText(outer))
	if outerSim >= sim {
		t.Errorf("outer wrapper similarity %.3f must stay below sprite similarity %.3f", outerSim, sim)
	}
}
