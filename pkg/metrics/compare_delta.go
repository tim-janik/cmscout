package metrics

import "slices"

func difference(before, after *int) *int {
	if before == nil || after == nil {
		return nil
	}
	value := *after - *before
	return &value
}

func metric_delta(old, new *Component, before, after *Snapshot, status string) MetricDelta {
	delta := MetricDelta{Status: status}
	if status != "matched" {
		return delta
	}
	if old == nil || new == nil {
		delta.Status = "unavailable"
		return delta
	}
	if before.Language != after.Language || !slices.Equal(before.Profiles, after.Profiles) {
		delta.Status = "incomparable"
		return delta
	}
	delta.Status = "complete"
	if old.Size != nil && new.Size != nil {
		delta.Chars, delta.Lines = difference(old.Size.Chars, new.Size.Chars), difference(&old.Size.Lines, &new.Size.Lines)
	}
	if old.FunctionMetrics != nil && new.FunctionMetrics != nil {
		delta.Cyclomatic = difference(old.Cyclomatic.Value, new.Cyclomatic.Value)
		if old.PrefixComment != nil && new.PrefixComment != nil {
			delta.PrefixChars = difference(old.PrefixComment.Chars, new.PrefixComment.Chars)
			delta.PrefixLines = difference(&old.PrefixComment.Lines, &new.PrefixComment.Lines)
		}
		if old.InlineComments != nil && new.InlineComments != nil {
			old_count, new_count := len(old.InlineComments), len(new.InlineComments)
			delta.InlineCommentCount = difference(&old_count, &new_count)
			delta.InlineChars = difference(comment_chars(old.InlineComments), comment_chars(new.InlineComments))
		}
	}
	return delta
}

func comment_chars(comments []Comment) *int {
	chars := 0
	for _, comment := range comments {
		if comment.Chars == nil {
			return nil
		}
		chars += *comment.Chars
	}
	return &chars
}
