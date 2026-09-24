// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package stats

import (
	"fmt"
	"io"
)

func Render(w io.Writer, r *Report) error {
	if r == nil {
		return fmt.Errorf("stats: nil report")
	}
	if _, err := fmt.Fprintf(w, "# cmscout stats: %s  (%s, %d parse errors)\n",
		r.FilePath, r.Language, r.ParseErrors); err != nil {
		return err
	}

	ci := 0 // index into r.Comments (file-level comments, in source order)
	for i := range r.Blocks {
		b := &r.Blocks[i]
		for ci < len(r.Comments) && r.Comments[ci].StartLine < b.StartLine {
			if err := renderComment(w, r.Comments[ci], "", r.FilePath); err != nil {
				return err
			}
			ci++
		}
		for j := range b.PrefixComments {
			if err := renderComment(w, b.PrefixComments[j], "  ", r.FilePath); err != nil {
				return err
			}
		}
		line := fmt.Sprintf("block %s %s  at %s:%d-%d  lines=%d chars=%d  prefix_lines=%d prefix_chars=%d",
			b.Kind, b.Qualified, r.FilePath, b.StartLine, b.EndLine, b.Lines, b.Chars,
			b.PrefixLines, b.PrefixChars)
		if isFunctionLike(b.Kind) {
			if b.Complexity > 0 {
				line += fmt.Sprintf("  branches=%d complexity=%d", b.Branches, b.Complexity)
			} else {
				line += "  complexity=unknown"
			}
		}
		if showsMethodCount(b.Kind) {
			line += fmt.Sprintf("  methods=%d", b.Methods)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		for j := range b.Comments {
			if err := renderComment(w, b.Comments[j], "  ", r.FilePath); err != nil {
				return err
			}
		}
	}
	for ; ci < len(r.Comments); ci++ {
		if err := renderComment(w, r.Comments[ci], "", r.FilePath); err != nil {
			return err
		}
	}
	return nil
}

// renderComment writes one comment record; indent distinguishes owned/prefix
// comments (block-scoped) from file-level comments.
func renderComment(w io.Writer, c Comment, indent, filePath string) error {
	line := indent + "comment"
	if c.PrefixOf != "" {
		line += " prefix_of=" + c.PrefixOf
	}
	if c.Owner != "" {
		line += " inside=" + c.Owner
	}
	line += "  at " + commentLoc(c, filePath)
	line += fmt.Sprintf("  lines=%d chars=%d", c.Lines, c.Chars)
	if c.ExceedsLimit {
		line += fmt.Sprintf("  exceeds=%d", c.Lines)
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

// commentLoc renders "file:line" or "file:line-end" for multi-line comments.
func commentLoc(c Comment, filePath string) string {
	loc := fmt.Sprintf("%s:%d", filePath, c.StartLine)
	if c.EndLine > c.StartLine {
		loc += fmt.Sprintf("-%d", c.EndLine)
	}
	return loc
}
