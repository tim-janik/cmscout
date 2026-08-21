// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package correlate

import (
	"strings"

	"cmdiff/pkg/ir"
)

// absorption: a comment folded into a matched reference (known by block ID or trimmed text).
type absorption struct {
	id   string // comment block ID ("" when only the line text is known)
	text string // trimmed comment source text
}

// newAbsorption returns the two maps backing one side's absorption
// bookkeeping: trimmed-source counts and absorbed block IDs.
func newAbsorption() (map[string]int, map[string]struct{}) {
	return make(map[string]int), make(map[string]struct{})
}

// sideAbsorbed: exact block IDs (span expansion) or per-occurrence text counts (line-based absorption).
func sideAbsorbed(b *ir.SemanticBlock, textCounts map[string]int, idSet map[string]struct{}) bool {
	if _, ok := idSet[b.ID]; ok {
		return true
	}
	return consumeAbsorbed(b.Source, textCounts)
}

// absorbPrefixComments folds comment-only lines preceding a "// [matched:" reference
// into it; reference lines themselves are never absorbed.
func absorbPrefixComments(src string) (string, []string) {
	lines := strings.Split(src, "\n")
	var result []string
	var absorbed []string

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Generated reference lines absorb any preceding comment-only lines (marker shape must match exactly).
		if isMatchedReferenceLine(line) {
			// Look backwards for comment-only lines and skip them
			for j := len(result) - 1; j >= 0; j-- {
				trimmed := strings.TrimSpace(result[j])
				if isMatchedReferenceLine(trimmed) {
					break // a reference line is not a prefix comment
				}
				if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
					absorbed = append(absorbed, trimmed)
					result = result[:j]
				} else {
					break
				}
			}
			result = append(result, line)
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n"), absorbed
}

func isMatchedReferenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	marker := "// [matched: "
	idx := strings.Index(trimmed, marker)
	if idx < 0 || !strings.HasSuffix(trimmed, "]") {
		return false
	}
	// Generated references start at the first non-space byte (code with a trailing marker is not one).
	return strings.TrimSpace(trimmed[:idx]) == ""
}

// consumeAbsorbed: one occurrence per folded comment (duplicate texts must not both vanish).
func consumeAbsorbed(source string, absorbed map[string]int) bool {
	trimmed := strings.TrimSpace(source)
	if absorbed[trimmed] == 0 {
		return false
	}
	absorbed[trimmed]--
	return true
}
