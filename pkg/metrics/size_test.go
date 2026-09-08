package metrics

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func TestSize_original_ranges(t *testing.T) {
	for _, source := range []string{"", "a", "a\n", "\r\n", "\tα🦊\r\n日本語\nlast", "a\xffb\n"} {
		index := index_source([]byte(source))
		for start := 0; start <= len(source); start++ {
			for end := start; end <= len(source); end++ {
				part := []byte(source[start:end])
				size := index.size(index.span(uint(start), uint(end)))
				lines := bytes.Count(part, []byte{'\n'})
				if len(part) > 0 && part[len(part)-1] != '\n' {
					lines++
				}
				if size.Bytes != uint(len(part)) || size.Lines != lines {
					t.Fatalf("range %q: %+v want bytes=%d lines=%d", part, size, len(part), lines)
				}
				if utf8.Valid(part) {
					if size.Chars == nil || *size.Chars != utf8.RuneCount(part) {
						t.Fatalf("range %q: chars=%v want=%d", part, size.Chars, utf8.RuneCount(part))
					}
				} else if size.Chars != nil {
					t.Fatalf("invalid UTF-8 %q has count %d", part, *size.Chars)
				}
			}
		}
	}
}
