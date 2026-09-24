// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"math"
	"regexp"
	"strings"

	"cmdiff/pkg/ir"
)

// SimilarityThreshold (0.5): below it a pair is unrelated (deletion + addition).
const SimilarityThreshold = 0.5

// normalizeThreshold clamps any threshold to [0, 1]: <0 → 0, >1 and NaN → 1 (never fabricate matches).
func normalizeThreshold(t float64) float64 {
	if math.IsNaN(t) || t > 1 {
		return 1
	}
	if t < 0 {
		return 0
	}
	return t
}

// ineligibleSim (-1): marks cells no normalized threshold can admit (e.g. incompatible scopes).
const ineligibleSim = -1.0

// SimilarityWeights: name and body dominate; kind only nudges (conversions stay matchable).
const (
	nameWeight = 0.35
	bodyWeight = 0.50
	kindWeight = 0.15
)

// EditDistance returns the full restricted Damerau-Levenshtein distance
// between two strings (no cutoff).
func EditDistance(a, b string) int {
	return editDistance([]byte(a), []byte(b), max(len(a), len(b)))
}

// editDistance: restricted Damerau-Levenshtein, banded to maxDist (only "distance <=
// maxDist" is exact) with three-row DP and prefix/suffix trimming.
func editDistance(a, b []byte, maxDist int) int {
	// Swap so len(b) <= len(a): the DP rows have length len(b)+1.
	if len(b) > len(a) {
		a, b = b, a
	}
	// Remove the common prefix.
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	// Remove the common suffix.
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	n, m := len(a), len(b)
	if n == 0 {
		if m > maxDist {
			return maxDist + 1
		}
		return m
	}
	if m == 0 {
		if n > maxDist {
			return maxDist + 1
		}
		return n
	}
	// Strings whose lengths differ by more than maxDist need at least that
	// many insertions/deletions, so the distance exceeds maxDist.
	if n-m > maxDist {
		return maxDist + 1
	}
	inf := maxDist + 1 // sentinel: a value known to exceed maxDist

	// Three rows: row2 = D[i-2], row1 = D[i-1], row0 = D[i].
	row2 := make([]int, m+1)
	row1 := make([]int, m+1)
	row0 := make([]int, m+1)
	for j := 0; j <= m; j++ {
		row1[j] = j // D[0][j]
	}
	for i := 1; i <= n; i++ {
		// The band is [i-maxDist, i+maxDist] clipped to the grid; the length guard above covers a slide past m.
		lo := min(m+1, max(1, i-maxDist))
		hi := min(m, i+maxDist)
		if lo == 1 {
			row0[0] = i // D[i][0]
		} else {
			row0[lo-1] = inf // outside the band: cannot be <= maxDist
		}
		for j := lo; j <= hi; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			// row1[j] is only valid within the band; at the right edge the true value exceeds maxDist, so the sentinel is exact.
			d := inf
			if j <= i-1+maxDist {
				d = row1[j] + 1 // deletion
			}
			if v := row0[j-1] + 1; v < d { // insertion
				d = v
			}
			if v := row1[j-1] + cost; v < d { // substitution
				d = v
			}
			if cost == 1 && i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				if v := row2[j-2] + 1; v < d { // transposition
					d = v
				}
			}
			if d > inf {
				d = inf
			}
			row0[j] = d
		}
		row2, row1, row0 = row1, row0, row2
	}
	d := row1[m]
	if d > maxDist {
		return maxDist + 1
	}
	return d
}

// DistanceText: raw source with whitespace collapsed (normalized forms cause false matches).
func DistanceText(source string) string {
	return strings.Join(strings.Fields(source), " ")
}

// elementDistanceText: one canonical shape for JSX and Lit elements — interpolation braces,
// attribute/event prefixes, and closers normalized — so template→JSX conversions match.
func elementDistanceText(source string) string {
	s := strings.Join(strings.Fields(source), " ")
	s = handleEventRe.ReplaceAllString(s, "{$1}")
	s = strings.ReplaceAll(s, "${", "{")
	s = quotedInterpRe.ReplaceAllString(s, "{$1}")
	s = litAttrRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := litAttrRe.FindStringSubmatch(m)
		// Keep the boundary character; drop the dialect prefix (? . @ bool:)
		// and lower-case the attribute name.
		return sub[1] + strings.ToLower(sub[3])
	})
	s = jsxOnAttrRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := jsxOnAttrRe.FindStringSubmatch(m)
		// Keep the boundary character; drop the JSX "on" prefix and
		// lower-case the event name (onWheel= → wheel=).
		return sub[1] + strings.ToLower(sub[2])
	})
	s = strings.ReplaceAll(s, "/>", ">")
	s = closeTagRe.ReplaceAllString(s, ">")
	return s
}

// distanceTextFor: elements use the canonical shape; every other kind uses raw collapsed source.
func distanceTextFor(b *ir.SemanticBlock) string {
	if isElement(b.Kind) {
		return elementDistanceText(b.Source)
	}
	return DistanceText(b.Source)
}

// Element-shape normalization patterns (see elementDistanceText).
var (
	// ${{handleEvent: F, ...}} — Lit event listener object.
	handleEventRe = regexp.MustCompile(`\$\{\{\s*handleEvent\s*:\s*([^,}]+?)\s*,[^}]*\}\}`)
	// "{expr}" — a quoted Lit interpolation.
	quotedInterpRe = regexp.MustCompile(`"\{([^{}]*)\}"`)
	// ?a= / .a= / @a= / bool:a= — Lit attribute prefixes at a word boundary.
	litAttrRe = regexp.MustCompile(`([\s<])(\?|\.|@|bool:)([A-Za-z][A-Za-z0-9_:.-]*)`)
	// onEvent= — JSX event attribute prefix.
	jsxOnAttrRe = regexp.MustCompile(`([\s<])on([A-Z][A-Za-z0-9_]*)`)
	// </name> — explicit closing tags.
	closeTagRe = regexp.MustCompile(`</[A-Za-z][A-Za-z0-9_:.-]*>`)
)

// nameSimilarity: edit distance on lowercased alphanumeric-only names.
func nameSimilarity(a, b string) float64 {
	na := foldName(a)
	nb := foldName(b)
	if na == "" && nb == "" {
		return 0.5 // both unnamed: neutral, no evidence either way
	}
	if na == "" || nb == "" {
		return 0.0 // named vs unnamed: no name evidence
	}
	if na == nb {
		return 1.0
	}
	maxLen := max(len(na), len(nb))
	d := editDistance([]byte(na), []byte(nb), maxLen)
	if d > maxLen {
		d = maxLen
	}
	return 1.0 - float64(d)/float64(maxLen)
}

// foldName lowercases a name and keeps only alphanumeric characters.
func foldName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

// bodySimilarity: full edit distance on distance texts, normalized by the longer text;
// empty bodies contribute 0, and the uncapped distance keeps custom thresholds exact.
func bodySimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0.0
	}
	if a == b {
		return 1.0
	}
	maxLen := max(len(a), len(b))
	if maxLen == 0 {
		return 0.0
	}
	d := EditDistance(a, b)
	if d > maxLen {
		d = maxLen
	}
	return 1.0 - float64(d)/float64(maxLen)
}

// kindSimilarity scores same-kind, compatible, and other eligible pairs.
func kindSimilarity(a, b ir.BlockKind) float64 {
	if a == b {
		return 1.0
	}
	if Compatible(a, b) {
		return 0.6
	}
	return 0.3
}

// BlockSimilarity computes the [0, 1] similarity between two blocks used by
// every matcher: a weighted combination of name, body text, and kind.
func BlockSimilarity(a, b *ir.SemanticBlock, aText, bText string) float64 {
	return weightedSimilarity(nameSimilarity, a, b, aText, bText)
}

// commentSimilarity: no name term (the 0.5 prior would over-match); source text alone decides.
func commentSimilarity(a, b *ir.SemanticBlock, aText, bText string) float64 {
	return weightedSimilarity(func(string, string) float64 { return 0 }, a, b, aText, bText)
}

// weightedSimilarity: name + body + kind with a pluggable name term (matching vs display).
func weightedSimilarity(nameSim func(a, b string) float64, a, b *ir.SemanticBlock, aText, bText string) float64 {
	return nameWeight*nameSim(a.Name, b.Name) +
		bodyWeight*bodySimilarity(aText, bText) +
		kindWeight*kindSimilarity(a.Kind, b.Kind)
}

// IsAnonymousOrdinal: per-side ordinal names (kind.NN) track position on one side only — never identity.
func IsAnonymousOrdinal(kind ir.BlockKind, name string) bool {
	if name == "" {
		return false
	}
	prefix := string(kind) + "."
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	for _, r := range name[len(prefix):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// PairSimilarity: displayed percentage from the pair's CURRENT sources (same text as InnerDiff), never stored confidence.
func PairSimilarity(p *ir.CorrelatedPair) float64 {
	if p.Old == nil || p.New == nil {
		return 0
	}
	// Anonymous ordinals on both sides: name term neutral, so identical text displays 100%.
	nameSim := displayNameSimilarity
	if p.Old.Kind == p.New.Kind && IsAnonymousOrdinal(p.Old.Kind, p.Old.Name) && IsAnonymousOrdinal(p.New.Kind, p.New.Name) {
		nameSim = func(string, string) float64 { return 1.0 }
	}
	// Display: no 0.5 prior for unnamed pairs (the matcher's prior must not leak into the percentage).
	return weightedSimilarity(nameSim, p.Old, p.New, p.Old.Source, p.New.Source)
}

// displayNameSimilarity: identical names (incl. two empty) get full credit; no 0.5 prior.
func displayNameSimilarity(a, b string) float64 {
	if foldName(a) == "" && foldName(b) == "" {
		return 1.0
	}
	return nameSimilarity(a, b)
}

// kindPairEligible isolates concepts and namespaces from other kinds.
func kindPairEligible(a, b ir.BlockKind) bool {
	if a == b {
		return true
	}
	return a != ir.KindNamespace && b != ir.KindNamespace &&
		a != ir.KindConcept && b != ir.KindConcept
}

// Compatible: same kind, callable↔callable, value↔value, and JSX↔Lit template elements.
func Compatible(a, b ir.BlockKind) bool {
	if a == b {
		return true
	}
	if isCallable(a) && isCallable(b) {
		return true
	}
	if isValue(a) && isValue(b) {
		return true
	}
	return isElement(a) && isElement(b)
}

// isElement: JSX or Lit template element (conversions stay one matched pair).
func isElement(kind ir.BlockKind) bool {
	return kind == ir.KindJSX || kind == ir.KindTemplate
}

func isCallable(kind ir.BlockKind) bool {
	switch kind {
	case ir.KindFunction, ir.KindArrowFunc, ir.KindMethod, ir.KindObjectMethod,
		ir.KindLifecycle, ir.KindMacroFunction, ir.KindLambda:
		return true
	default:
		return false
	}
}

func isValue(kind ir.BlockKind) bool {
	return kind == ir.KindConstant || kind == ir.KindVariable
}
