// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package ir

import "testing"

func TestDiffResultHasChanges(t *testing.T) {
	if (&DiffResult{}).HasChanges() {
		t.Error("empty diff must not report changes")
	}
	noChange := &DiffResult{Hunks: []DiffHunk{{Lines: []DiffLine{{Content: "x", Type: DiffLineContext}}}}}
	if noChange.HasChanges() {
		t.Error("context-only diff must not report changes")
	}
	added := &DiffResult{Hunks: []DiffHunk{{Lines: []DiffLine{{Content: "x", Type: DiffLineAdded}}}}}
	if !added.HasChanges() {
		t.Error("added line must report changes")
	}
	removed := &DiffResult{Hunks: []DiffHunk{{Lines: []DiffLine{{Content: "x", Type: DiffLineRemoved}}}}}
	if !removed.HasChanges() {
		t.Error("removed line must report changes")
	}
}

func TestCorrelatedPairPredicates(t *testing.T) {
	b := &SemanticBlock{}
	cases := []struct {
		name    string
		p       CorrelatedPair
		added   bool
		removed bool
		matched bool
	}{
		{"added", CorrelatedPair{New: b}, true, false, false},
		{"removed", CorrelatedPair{Old: b}, false, true, false},
		{"matched", CorrelatedPair{Old: b, New: b}, false, false, true},
	}
	for _, tc := range cases {
		if got := tc.p.IsAdded(); got != tc.added {
			t.Errorf("%s: IsAdded() = %v, want %v", tc.name, got, tc.added)
		}
		if got := tc.p.IsRemoved(); got != tc.removed {
			t.Errorf("%s: IsRemoved() = %v, want %v", tc.name, got, tc.removed)
		}
		if got := tc.p.IsMatched(); got != tc.matched {
			t.Errorf("%s: IsMatched() = %v, want %v", tc.name, got, tc.matched)
		}
	}
}
