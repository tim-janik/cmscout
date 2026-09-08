package metrics

import (
	"sort"
	"strings"

	"cmscout/pkg/diff"
	"cmscout/pkg/ir"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type source_token struct {
	span Span
	text string
}

func source_tokens(root *tree_sitter.Node, source []byte) []source_token {
	tokens := []source_token{}
	var visit func(*tree_sitter.Node)
	visit = func(node *tree_sitter.Node) {
		if node.Kind() == "comment" || node.Kind() == "hash_bang_line" {
			return
		}
		if node.ChildCount() == 0 {
			text := node_text(node, source)
			if node.IsNamed() || strings.TrimSpace(text) != "" {
				tokens = append(tokens, source_token{*node_span(node), text})
			}
			return
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			visit(node.Child(i))
		}
	}
	visit(root)
	return tokens
}

func contains_span(outer, inner *Span) bool {
	return outer != nil && inner != nil && outer.StartByte <= inner.StartByte && inner.EndByte <= outer.EndByte
}

func (snapshot *Snapshot) component_text(component *Component, tokens, signature bool) string {
	if component == nil || component.Span == nil {
		return ""
	}
	span := component.Span
	var excluded []Span
	parents := map[string]string{}
	for _, child := range snapshot.Components {
		parents[child.QualifiedName] = child.ParentName
	}
	for _, child := range snapshot.Components {
		descendant := false
		for parent := child.ParentName; parent != ""; parent = parents[parent] {
			if parent == component.QualifiedName {
				descendant = true
				break
			}
		}
		if descendant && contains_span(span, child.Span) {
			excluded = append(excluded, *child.Span)
			if child.FunctionMetrics != nil && child.PrefixComment != nil && contains_span(span, child.PrefixComment.Span) {
				excluded = append(excluded, *child.PrefixComment.Span)
			}
		}
	}
	if signature && component.Body != nil {
		excluded = append(excluded, *component.Body)
	}
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].StartByte < excluded[j].StartByte })
	var result strings.Builder
	if tokens {
		i := sort.Search(len(snapshot.tokens), func(i int) bool { return snapshot.tokens[i].span.StartByte >= span.StartByte })
		exclusion := 0
		for ; i < len(snapshot.tokens) && snapshot.tokens[i].span.EndByte <= span.EndByte; i++ {
			current := snapshot.tokens[i]
			for exclusion < len(excluded) && excluded[exclusion].EndByte <= current.span.StartByte {
				exclusion++
			}
			if exclusion < len(excluded) && excluded[exclusion].StartByte < current.span.EndByte {
				continue
			}
			result.WriteString(current.text)
			result.WriteByte(0)
		}
		return result.String()
	}
	offset := span.StartByte
	for _, child := range excluded {
		if child.StartByte > offset {
			result.Write(snapshot.source[offset:child.StartByte])
		}
		offset = max(offset, child.EndByte)
	}
	result.Write(snapshot.source[offset:span.EndByte])
	return result.String()
}

func (snapshot *Snapshot) comment_text(component *Component, prefix bool) string {
	if component == nil {
		return ""
	}
	var result strings.Builder
	for _, comment := range snapshot.Comments {
		role_matches := comment.Role == "prefix"
		if !prefix {
			role_matches = comment.Role == "inline"
		}
		if comment.OwnerName == component.QualifiedName && role_matches && comment.Span != nil {
			result.Write(snapshot.source[comment.Span.StartByte:comment.Span.EndByte])
			result.WriteByte(0)
		}
	}
	return result.String()
}

func (snapshot *Snapshot) directive_text(component *Component) string {
	if component == nil {
		return ""
	}
	var result strings.Builder
	for _, comment := range snapshot.Comments {
		if comment.OwnerName == component.QualifiedName && comment.Role == "directive" && comment.Span != nil {
			result.Write(snapshot.source[comment.Span.StartByte:comment.Span.EndByte])
			result.WriteByte(0)
		}
	}
	return result.String()
}

func changed_ranges(before, after []byte) ([]Span, []Span, map[uint]uint) {
	old_index, new_index := index_source(before), index_source(after)
	old_ranges, new_ranges := []Span{}, []Span{}
	unchanged := map[uint]uint{}
	result := diff.New().DiffFull(string(before), string(after))
	for _, hunk := range result.Hunks {
		for _, line := range hunk.Lines {
			if line.Type == ir.DiffLineRemoved {
				old_ranges = append(old_ranges, *old_index.line_span(line.OldNo))
			} else if line.Type == ir.DiffLineAdded {
				new_ranges = append(new_ranges, *new_index.line_span(line.NewNo))
			} else if line.OldNo > 0 && line.NewNo > 0 {
				unchanged[uint(line.OldNo-1)] = uint(line.NewNo - 1)
			}
		}
	}
	if strings.HasSuffix(string(before), "\n") != strings.HasSuffix(string(after), "\n") {
		old_ranges = append(old_ranges, *old_index.last_line_span())
		new_ranges = append(new_ranges, *new_index.last_line_span())
	}
	return merge_ranges(old_ranges), merge_ranges(new_ranges), unchanged
}

func unchanged_component(old, new *Component, lines map[uint]uint) bool {
	if old.Span == nil || new.Span == nil || old.Span.StartCol != new.Span.StartCol || old.Span.EndCol != new.Span.EndCol ||
		old.Span.EndLine-old.Span.StartLine != new.Span.EndLine-new.Span.StartLine {
		return false
	}
	end := old.Span.EndLine
	if old.Span.EndCol == 0 && end > old.Span.StartLine {
		end--
	}
	for line := old.Span.StartLine; line <= end; line++ {
		mapped, exists := lines[line]
		if !exists || mapped != new.Span.StartLine+line-old.Span.StartLine {
			return false
		}
	}
	return true
}

func (index *source_index) last_line_span() *Span {
	line := len(index.lines)
	if len(index.source) > 0 && index.source[len(index.source)-1] == '\n' {
		line--
	}
	return index.line_span(line)
}

func merge_ranges(ranges []Span) []Span {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].StartByte < ranges[j].StartByte })
	result := []Span{}
	for _, span := range ranges {
		if len(result) > 0 && result[len(result)-1].EndByte >= span.StartByte {
			previous := &result[len(result)-1]
			if span.EndByte > previous.EndByte {
				previous.EndByte, previous.EndLine, previous.EndCol = span.EndByte, span.EndLine, span.EndCol
			}
		} else {
			result = append(result, span)
		}
	}
	return result
}

func (index *source_index) line_span(number int) *Span {
	line := max(0, min(number-1, len(index.lines)-1))
	start := index.lines[line]
	end := uint(len(index.source))
	if line+1 < len(index.lines) {
		end = index.lines[line+1]
	}
	return index.span(start, end)
}

func ranges_for(component *Component, ranges []Span) []Span {
	result := []Span{}
	if component == nil {
		return result
	}
	for _, span := range ranges {
		if intersects(component.Span, &span) || component.FunctionMetrics != nil && component.PrefixComment != nil &&
			intersects(component.PrefixComment.Span, &span) {
			result = append(result, span)
		}
	}
	return result
}

func intersects(a, b *Span) bool {
	return a != nil && b != nil && a.StartByte <= b.EndByte && b.StartByte <= a.EndByte
}
