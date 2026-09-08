package metrics

import (
	"sort"
	"unicode/utf8"
)

type char_adjustment struct {
	end   uint
	extra int
}

type source_index struct {
	source      []byte
	lines       []uint
	adjustments []char_adjustment
	invalid     []uint
}

func index_source(source []byte) *source_index {
	index := &source_index{source: source, lines: []uint{0}}
	extra := 0
	for offset := 0; offset < len(source); {
		value, size := utf8.DecodeRune(source[offset:])
		if value == utf8.RuneError && size == 1 {
			index.invalid = append(index.invalid, uint(offset))
		}
		if source[offset] == '\n' {
			index.lines = append(index.lines, uint(offset+1))
		}
		offset += size
		if size > 1 {
			extra += size - 1
			index.adjustments = append(index.adjustments, char_adjustment{uint(offset), extra})
		}
	}
	return index
}

func (index *source_index) span(start, end uint) *Span {
	start_line := sort.Search(len(index.lines), func(i int) bool { return index.lines[i] > start }) - 1
	end_line := sort.Search(len(index.lines), func(i int) bool { return index.lines[i] > end }) - 1
	return &Span{start, end, uint(start_line), start - index.lines[start_line], uint(end_line), end - index.lines[end_line]}
}

func (index *source_index) extra(offset uint) int {
	i := sort.Search(len(index.adjustments), func(i int) bool { return index.adjustments[i].end > offset })
	if i == 0 {
		return 0
	}
	return index.adjustments[i-1].extra
}

func (index *source_index) size(span *Span) *Size {
	if span == nil {
		return nil
	}
	start, end := span.StartByte, span.EndByte
	chars := int(end-start) - index.extra(end) + index.extra(start)
	size := &Size{Bytes: end - start, Chars: &chars}
	if start == end {
		return size
	}
	if start != end {
		size.Lines = int(span.EndLine-span.StartLine) + 1
		if span.EndCol == 0 {
			size.Lines--
		}
	}
	invalid := sort.Search(len(index.invalid), func(i int) bool { return index.invalid[i] >= start })
	if invalid < len(index.invalid) && index.invalid[invalid] < end ||
		start < uint(len(index.source)) && !utf8.RuneStart(index.source[start]) ||
		end < uint(len(index.source)) && !utf8.RuneStart(index.source[end]) {
		size.Chars = nil
	}
	return size
}
