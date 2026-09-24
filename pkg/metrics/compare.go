package metrics

import (
	"fmt"
	"math"
	"sort"

	"cmscout/pkg/ir"
	"cmscout/pkg/matching"
)

type component_pair struct {
	before, after *Component
	change        Change
}

func Compare(before, after *Snapshot) (*Comparison, error) {
	return compare_snapshots(before, after, false)
}

func CompareFiles(before, after *Snapshot) (*Comparison, error) {
	return compare_snapshots(before, after, true)
}

func compare_snapshots(before, after *Snapshot, file_identity bool) (*Comparison, error) {
	if before == nil && after == nil {
		return nil, fmt.Errorf("comparison requires at least one snapshot")
	}
	for _, snapshot := range []*Snapshot{before, after} {
		if snapshot != nil && (snapshot.source == nil || len(snapshot.Components) == 0) {
			return nil, fmt.Errorf("comparison requires snapshots returned by Measure")
		}
	}
	if before == nil || after == nil {
		return compare_missing(before, after), nil
	}
	if before.SchemaVersion != after.SchemaVersion || before.NamingVersion != after.NamingVersion {
		return nil, fmt.Errorf("cannot compare different metric schemas or naming versions")
	}
	result := &Comparison{
		SchemaVersion: "1", Status: "complete", RangePrecision: "original source lines",
		Before: before, After: after, Changes: []Change{}, Diagnostics: []Diagnostic{},
	}
	var unchanged_lines map[uint]uint
	result.BeforeRanges, result.AfterRanges, unchanged_lines = changed_ranges(before.source, after.source)
	old_blocks, old_components := comparison_blocks(before)
	new_blocks, new_components := comparison_blocks(after)
	matches, removed, added := matching.MatchBlocks(old_blocks, new_blocks, matching.SimilarityThreshold)
	pairs := []component_pair{{before: &before.Components[0], after: &after.Components[0],
		change: Change{Match: MatchEvidence{Status: "matched", Method: "input_pair", Similarity: 1}}}}
	for _, match := range matches {
		pairs = append(pairs, component_pair{before: old_components[match.Old.ID], after: new_components[match.New.ID],
			change: Change{Match: MatchEvidence{Status: "matched", Method: string(match.MatchType), Similarity: match.Confidence}}})
	}
	for _, block := range removed {
		pairs = append(pairs, component_pair{before: old_components[block.ID], change: Change{Match: MatchEvidence{Status: "removed"}}})
	}
	for _, block := range added {
		pairs = append(pairs, component_pair{after: new_components[block.ID], change: Change{Match: MatchEvidence{Status: "added"}}})
	}
	parent_pairs := map[string]string{}
	for _, pair := range pairs {
		if pair.before != nil && pair.after != nil {
			parent_pairs[pair.before.QualifiedName] = pair.after.QualifiedName
		}
	}
	if before.ContentDigest != after.ContentDigest {
		for i := range matches {
			match := matches[i]
			evidence := &pairs[i+1].change.Match
			evidence.AfterCandidates = tied_candidates(match.Old, match.New, new_blocks, match.MatchType)
			evidence.BeforeCandidates = tied_candidates(match.New, match.Old, old_blocks, match.MatchType)
			if unchanged_component(pairs[i+1].before, pairs[i+1].after, unchanged_lines) {
				evidence.AfterCandidates, evidence.BeforeCandidates = nil, nil
				evidence.Method = "unchanged_lines"
			}
			if len(evidence.AfterCandidates) > 1 || len(evidence.BeforeCandidates) > 1 {
				evidence.Status = "ambiguous"
				for j, id := range evidence.AfterCandidates {
					evidence.AfterCandidates[j] = new_components[id].QualifiedName
				}
				for j, id := range evidence.BeforeCandidates {
					evidence.BeforeCandidates[j] = old_components[id].QualifiedName
				}
				result.Status = "partial"
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					"ambiguous_match", "several components have equal matching evidence", pairs[i+1].after.Span,
				})
			}
		}
	}
	for i := range pairs {
		pair := &pairs[i]
		old, new, change := pair.before, pair.after, &pair.change
		if old != nil {
			change.BeforeName, change.BeforeKind = &old.QualifiedName, &old.Kind
		}
		if new != nil {
			change.AfterName, change.AfterKind = &new.QualifiedName, &new.Kind
		}
		change.BeforeRanges = ranges_for(old, result.BeforeRanges)
		change.AfterRanges = ranges_for(new, result.AfterRanges)
		change.Added, change.Removed = old == nil, new == nil
		if old != nil && new != nil {
			change.Renamed = old.Kind != "file" && old.Name != new.Name
			change.NameChanged = render_name(old.NameParts[2:]) != render_name(new.NameParts[2:])
			change.KindChanged = old.Kind != new.Kind
			change.Moved = old.ParentName != "" && parent_pairs[old.ParentName] != new.ParentName
			if file_identity && before.Path != after.Path {
				change.NameChanged, change.Moved = true, true
				if old.Kind == "file" {
					change.Renamed = true
				}
			}
			change.CodeChanged = before.component_text(old, true, false) != after.component_text(new, true, false)
			change.SignatureChanged = old.Body != nil && new.Body != nil &&
				before.component_text(old, true, true) != after.component_text(new, true, true)
			change.PrefixChanged = before.comment_text(old, true) != after.comment_text(new, true)
			change.InlineChanged = before.comment_text(old, false) != after.comment_text(new, false)
			directive_changed := before.directive_text(old) != after.directive_text(new)
			raw_changed := before.component_text(old, false, false) != after.component_text(new, false, false)
			change.FormattingChanged = raw_changed && !change.CodeChanged && !change.InlineChanged && !directive_changed
			change.DirectChanged = raw_changed || change.PrefixChanged || change.InlineChanged || change.Renamed || change.KindChanged
		} else {
			change.DirectChanged = true
		}
		if before.Status != "complete" || after.Status != "complete" {
			result.Status = "partial"
			change.Match.Status = "unavailable"
			change.Added, change.Removed = false, false
		}
		change.Delta = metric_delta(old, new, before, after, change.Match.Status)
		if change.Delta.Cyclomatic != nil && *change.Delta.Cyclomatic != 0 {
			change.DirectChanged, change.CodeChanged = true, true
		}
	}
	propagate_uncertainty(pairs)
	mark_component_moves(pairs, before, after, parent_pairs)
	propagate_changes(pairs, before, after)
	for _, pair := range pairs {
		if pair.change.touched() {
			result.Changes = append(result.Changes, pair.change)
		}
	}
	sort.SliceStable(result.Changes, func(i, j int) bool {
		a, b := result.Changes[i], result.Changes[j]
		name_a, name_b := a.BeforeName, b.BeforeName
		if a.AfterName != nil {
			name_a = a.AfterName
		}
		if b.AfterName != nil {
			name_b = b.AfterName
		}
		return *name_a < *name_b
	})
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Span.StartByte < result.Diagnostics[j].Span.StartByte
	})
	return result, nil
}

func (change Change) touched() bool {
	return change.DirectChanged || change.DescendantChanged || change.NameChanged || change.Moved || change.Added || change.Removed ||
		change.Match.Status == "ambiguous" || change.Match.Status == "unavailable"
}

func comparison_blocks(snapshot *Snapshot) ([]ir.SemanticBlock, map[string]*Component) {
	blocks := []ir.SemanticBlock{}
	by_id := map[string]*Component{}
	ids := map[string]string{snapshot.Components[0].QualifiedName: ""}
	for i := 1; i < len(snapshot.Components); i++ {
		component := &snapshot.Components[i]
		offset := uint(0)
		if component.Span != nil {
			offset = component.Span.StartByte
		}
		id := fmt.Sprintf("%s:%d", render_name(component.NameParts[2:]), offset)
		ids[component.QualifiedName], by_id[id] = id, component
	}
	for i := 1; i < len(snapshot.Components); i++ {
		component := &snapshot.Components[i]
		name := component.Name
		if component.NameOrigin == "ordinal" {
			name = ""
		} else if last := component.NameParts[len(component.NameParts)-1]; last.Signature != "" {
			name += " " + last.Signature
		}
		kind := ir.BlockKind(component.Kind)
		if kind == "scope" {
			kind = ir.KindNamespace
		}
		block := ir.SemanticBlock{ID: ids[component.QualifiedName], Name: name, Kind: kind, Parent: ids[component.ParentName]}
		if span := component.Span; span != nil {
			block.Span = ir.SourceSpan{
				StartByte: span.StartByte, EndByte: span.EndByte, StartLine: span.StartLine,
				StartCol: span.StartCol, EndLine: span.EndLine, EndCol: span.EndCol,
			}
			block.Source = string(snapshot.source[span.StartByte:span.EndByte])
		}
		blocks = append(blocks, block)
	}
	return blocks, by_id
}

func propagate_uncertainty(pairs []component_pair) {
	by_name := map[string]int{}
	for i, pair := range pairs {
		if pair.before != nil {
			by_name[pair.before.QualifiedName] = i
		}
	}
	for i := range pairs {
		component := pairs[i].before
		if component == nil || pairs[i].after == nil {
			continue
		}
		for parent := component.ParentName; parent != ""; {
			index, exists := by_name[parent]
			if !exists {
				break
			}
			status := pairs[index].change.Match.Status
			if status == "ambiguous" || status == "unavailable" {
				pairs[i].change.Match.Status = status
				pairs[i].change.Delta = MetricDelta{Status: status}
				break
			}
			parent = pairs[index].before.ParentName
		}
	}
}

func tied_candidates(source, selected *ir.SemanticBlock, candidates []ir.SemanticBlock, method ir.MatchType) []string {
	result := []string{}
	source_text := matching.DistanceText(source.Source)
	selected_score := matching.BlockSimilarity(source, selected, source_text, matching.DistanceText(selected.Source))
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Parent != selected.Parent || !matching.Compatible(candidate.Kind, selected.Kind) {
			continue
		}
		if method == ir.MatchExactName && (candidate.Name != selected.Name || candidate.Kind != selected.Kind) {
			continue
		}
		score := matching.BlockSimilarity(source, candidate, source_text, matching.DistanceText(candidate.Source))
		if math.Abs(score-selected_score) < 1e-12 {
			result = append(result, candidate.ID)
		}
	}
	if len(result) < 2 {
		return nil
	}
	return result
}

func mark_component_moves(pairs []component_pair, before, after *Snapshot, parent_pairs map[string]string) {
	old_parents, new_parents := physical_parents(before), physical_parents(after)
	for i := range pairs {
		a := &pairs[i]
		if a.before == nil || a.after == nil || a.before.Span == nil || a.after.Span == nil {
			continue
		}
		old_parent, new_parent := old_parents[a.before.QualifiedName], new_parents[a.after.QualifiedName]
		if old_parent != "" && parent_pairs[old_parent] != new_parent {
			a.change.Moved = true
		}
		for j := i + 1; j < len(pairs); j++ {
			b := &pairs[j]
			if b.before == nil || b.after == nil || b.before.Span == nil || b.after.Span == nil ||
				old_parent != old_parents[b.before.QualifiedName] || new_parent != new_parents[b.after.QualifiedName] {
				continue
			}
			if (a.before.Span.StartByte < b.before.Span.StartByte) != (a.after.Span.StartByte < b.after.Span.StartByte) {
				a.change.Moved, b.change.Moved = true, true
			}
		}
	}
}

func physical_parents(snapshot *Snapshot) map[string]string {
	parents := map[string]string{}
	for _, component := range snapshot.Components {
		if component.Kind == "file" || component.Span == nil {
			continue
		}
		parent := &snapshot.Components[0]
		for i := range snapshot.Components {
			candidate := &snapshot.Components[i]
			if candidate.QualifiedName == component.QualifiedName || !contains_span(candidate.Span, component.Span) {
				continue
			}
			if candidate.Span.EndByte-candidate.Span.StartByte < parent.Span.EndByte-parent.Span.StartByte {
				parent = candidate
			}
		}
		parents[component.QualifiedName] = parent.QualifiedName
	}
	return parents
}

func propagate_changes(pairs []component_pair, before, after *Snapshot) {
	for _, snapshot := range []*Snapshot{before, after} {
		parents := map[string]string{}
		for _, component := range snapshot.Components {
			parents[component.QualifiedName] = component.ParentName
		}
		by_name := map[string]int{}
		for i, pair := range pairs {
			component := pair.before
			if snapshot == after {
				component = pair.after
			}
			if component != nil {
				by_name[component.QualifiedName] = i
			}
		}
		for i := range pairs {
			component := pairs[i].before
			if snapshot == after {
				component = pairs[i].after
			}
			if component == nil || !pairs[i].change.touched() {
				continue
			}
			for parent := component.ParentName; parent != ""; parent = parents[parent] {
				if index, exists := by_name[parent]; exists {
					pairs[index].change.DescendantChanged = true
				}
			}
		}
	}
}
