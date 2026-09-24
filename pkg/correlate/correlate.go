// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package correlate

import (
	"cmscout/pkg/ir"
	"cmscout/pkg/matching"
)

// Correlate matches two documents; every block appears exactly once (matched, added, or removed).
func Correlate(oldDoc, newDoc *ir.SemanticDocument) *ir.CorrelationResult {
	matched, unmatchedOld, unmatchedNew := matching.MatchBlocks(oldDoc.Blocks, newDoc.Blocks, matching.SimilarityThreshold)
	return assembleResult(oldDoc, newDoc, matched, unmatchedOld, unmatchedNew)
}

// assembleResult turns a matcher result into the correlation IR.
func assembleResult(oldDoc, newDoc *ir.SemanticDocument, matched []ir.CorrelatedPair, unmatchedOld, unmatchedNew []*ir.SemanticBlock) *ir.CorrelationResult {
	pairs := make([]ir.CorrelatedPair, 0, len(matched)+len(unmatchedOld)+len(unmatchedNew))
	pairs = append(pairs, matched...)

	for _, block := range unmatchedNew {
		pairs = append(pairs, ir.CorrelatedPair{New: block, MatchType: ir.MatchNone})
	}
	for _, block := range unmatchedOld {
		pairs = append(pairs, ir.CorrelatedPair{Old: block, MatchType: ir.MatchNone})
	}

	return &ir.CorrelationResult{
		Pairs:          pairs,
		OldParseErrors: oldDoc.ParseErrors,
		NewParseErrors: newDoc.ParseErrors,
	}
}
