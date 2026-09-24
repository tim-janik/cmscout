package metrics

import (
	"bytes"
	"sort"
	"strings"
)

func (inv *inventory) measure_comments(index *source_index) ([]Comment, []Diagnostic) {
	comments := []Comment{}
	diagnostics := []Diagnostic{}
	consumed := make([]bool, len(inv.comments))
	anchors := map[uint][]int{}
	for i, component := range inv.components {
		if component.FunctionMetrics != nil {
			zero := 0
			component.CommentStatus = "complete"
			component.PrefixComment = &Comment{Size: Size{Chars: &zero}}
			component.InlineComments = []Comment{}
		}
		if component.Declaration != nil {
			anchors[component.Declaration.StartByte] = append(anchors[component.Declaration.StartByte], i)
		}
	}
	var offsets []uint
	for offset := range anchors {
		offsets = append(offsets, offset)
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	for _, offset := range offsets {
		last := sort.Search(len(inv.comments), func(i int) bool { return inv.comments[i].EndByte() > offset }) - 1
		if last < 0 || consumed[last] {
			continue
		}
		end := node_span(inv.comments[last])
		if !inv.prefix_gap(index, end.EndByte, offset) || !inv.own_line(index, end) || inv.directive(last) {
			continue
		}
		first := last
		for first > 0 {
			previous := node_span(inv.comments[first-1])
			if consumed[first-1] || inv.directive(first-1) || !inv.own_line(index, previous) ||
				!inv.prefix_gap(index, previous.EndByte, inv.comments[first].StartByte()) {
				break
			}
			first--
		}
		span := index.span(inv.comments[first].StartByte(), end.EndByte)
		comment := Comment{Size: *index.size(span), Span: span, Role: "prefix", Ownership: "complete"}
		owners := anchors[offset]
		if len(owners) == 1 {
			component := &inv.components[owners[0]]
			comment.OwnerName = component.QualifiedName
			if component.FunctionMetrics != nil {
				component.PrefixComment = &comment
			}
		} else {
			comment.Ownership = "ambiguous"
			for _, owner := range owners {
				if component := &inv.components[owner]; component.FunctionMetrics != nil {
					component.CommentStatus = "ambiguous"
					component.PrefixComment = nil
				}
			}
			diagnostics = append(diagnostics, Diagnostic{"ambiguous_comment", "prefix has more than one possible owner", span})
		}
		comments = append(comments, comment)
		for i := first; i <= last; i++ {
			consumed[i] = true
		}
	}
	for i := 0; i < len(inv.comments); i++ {
		if consumed[i] {
			continue
		}
		span := node_span(inv.comments[i])
		owner := inv.comment_owners[i]
		role := "container"
		if inv.components[owner].FunctionMetrics != nil {
			role = "inline"
		} else if owner == 0 {
			role = "file"
		}
		if inv.directive(i) {
			role = "directive"
		}
		if role != "directive" && inv.single_own_line(index, span) {
			for i+1 < len(inv.comments) && !consumed[i+1] && inv.comment_owners[i+1] == owner && !inv.directive(i+1) {
				next := node_span(inv.comments[i+1])
				if !inv.single_own_line(index, next) || !inv.prefix_gap(index, span.EndByte, next.StartByte) {
					break
				}
				span = index.span(span.StartByte, next.EndByte)
				i++
			}
		}
		comment := Comment{
			Size: *index.size(span), Span: span, OwnerName: inv.components[owner].QualifiedName, Role: role, Ownership: "complete",
		}
		comments = append(comments, comment)
		if role == "inline" {
			inv.components[owner].InlineComments = append(inv.components[owner].InlineComments, comment)
		}
	}
	sort.Slice(comments, func(i, j int) bool { return comments[i].Span.StartByte < comments[j].Span.StartByte })
	return comments, diagnostics
}

func (inv *inventory) prefix_gap(index *source_index, end, start uint) bool {
	if end > start {
		return false
	}
	gap := inv.source[end:start]
	newlines := bytes.Count(gap, []byte{'\n'})
	if end > 0 && inv.source[end-1] == '\n' {
		newlines++
	}
	return newlines == 1 && len(bytes.TrimSpace(gap)) == 0
}

func (inv *inventory) own_line(index *source_index, span *Span) bool {
	return len(bytes.TrimSpace(inv.source[index.lines[span.StartLine]:span.StartByte])) == 0
}

func (inv *inventory) single_own_line(index *source_index, span *Span) bool {
	return index.size(span).Lines == 1 && inv.own_line(index, span)
}

func (inv *inventory) directive(i int) bool {
	text := strings.TrimSpace(node_text(inv.comments[i], inv.source))
	return strings.HasPrefix(text, "#!") || strings.HasPrefix(text, "//go:") || strings.HasPrefix(text, "// +build") ||
		strings.HasPrefix(text, "/// <reference") || strings.HasPrefix(text, "// @ts-") || strings.HasPrefix(text, "# shellcheck")
}
