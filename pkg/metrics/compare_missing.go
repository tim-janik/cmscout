package metrics

import "sort"

func compare_missing(before, after *Snapshot) *Comparison {
	snapshot := after
	if snapshot == nil {
		snapshot = before
	}
	result := &Comparison{
		SchemaVersion: "1", Status: snapshot.Status, RangePrecision: "original source lines",
		Before: before, After: after, Changes: []Change{}, Diagnostics: []Diagnostic{},
		BeforeRanges: []Span{}, AfterRanges: []Span{},
	}
	var old_source, new_source []byte
	if before != nil {
		old_source = before.source
	}
	if after != nil {
		new_source = after.source
	}
	result.BeforeRanges, result.AfterRanges, _ = changed_ranges(old_source, new_source)
	for i := range snapshot.Components {
		component := &snapshot.Components[i]
		change := Change{DirectChanged: true, BeforeRanges: []Span{}, AfterRanges: []Span{}}
		if before == nil {
			change.AfterName, change.AfterKind = &component.QualifiedName, &component.Kind
			change.AfterRanges = ranges_for(component, result.AfterRanges)
			change.Added, change.Match.Status = true, "added"
		} else {
			change.BeforeName, change.BeforeKind = &component.QualifiedName, &component.Kind
			change.BeforeRanges = ranges_for(component, result.BeforeRanges)
			change.Removed, change.Match.Status = true, "removed"
		}
		if snapshot.Status != "complete" {
			change.Added, change.Removed, change.Match.Status = false, false, "unavailable"
		}
		change.Delta.Status = change.Match.Status
		result.Changes = append(result.Changes, change)
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		if before == nil {
			return *result.Changes[i].AfterName < *result.Changes[j].AfterName
		}
		return *result.Changes[i].BeforeName < *result.Changes[j].BeforeName
	})
	return result
}
