// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package stats

import (
	"fmt"
	"io"

	"cmscout/pkg/report"
)

func Render(w io.Writer, r *Report) error {
	if r == nil {
		return fmt.Errorf("stats: nil report")
	}
	path := report.DisplayPath(r.FilePath)
	if _, err := fmt.Fprintf(w, "# cmscout stats: %s  (%s, %d parse errors)\n",
		path, r.Language, r.ParseErrors); err != nil {
		return err
	}

	if r.Status == "partial" {
		if _, err := fmt.Fprintln(w, "# status: partial"); err != nil {
			return err
		}
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintf(w, "# diagnostic %s: %s\n", d.Kind, d.Message); err != nil {
			return err
		}
	}
	ci := 0 // index into r.Comments (file-level comments, in source order)
	for i := range r.Blocks {
		b := &r.Blocks[i]
		for ci < len(r.Comments) && r.Comments[ci].StartLine < b.StartLine {
			if err := render_comment(w, r.Comments[ci], "", path); err != nil {
				return err
			}
			ci++
		}
		for j := range b.PrefixComments {
			if err := render_comment(w, b.PrefixComments[j], "  ", path); err != nil {
				return err
			}
		}
		line := fmt.Sprintf("block %s %s  at %s:%d-%d  lines=%d chars=%d  prefix_lines=%d prefix_chars=%d",
			b.Kind, b.Qualified, path, b.StartLine, b.EndLine, b.Lines, b.Chars,
			b.PrefixLines, b.PrefixChars)
		if is_function_like(b.Kind) {
			if b.Complexity > 0 {
				line += fmt.Sprintf("  branches=%d complexity=%d", b.Branches, b.Complexity)
			} else {
				line += "  complexity=unknown"
			}
		}
		if shows_method_count(b.Kind) {
			line += fmt.Sprintf("  methods=%d", b.Methods)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		for j := range b.Comments {
			if err := render_comment(w, b.Comments[j], "  ", path); err != nil {
				return err
			}
		}
	}
	for ; ci < len(r.Comments); ci++ {
		if err := render_comment(w, r.Comments[ci], "", path); err != nil {
			return err
		}
	}
	return nil
}

// render_comment writes one comment record; indent distinguishes owned/prefix
// comments (block-scoped) from file-level comments.
func render_comment(w io.Writer, c Comment, indent, file_path string) error {
	line := indent + "comment"
	if c.PrefixOf != "" {
		line += " prefix_of=" + c.PrefixOf
	}
	if c.Owner != "" {
		line += " inside=" + c.Owner
	}
	line += "  at " + comment_location(c, file_path)
	line += fmt.Sprintf("  lines=%d chars=%d", c.Lines, c.Chars)
	if c.ExceedsLimit {
		line += fmt.Sprintf("  exceeds=%d", c.Lines)
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

// comment_location renders "file:line" or "file:line-end" for multi-line comments.
func comment_location(c Comment, file_path string) string {
	loc := fmt.Sprintf("%s:%d", file_path, c.StartLine)
	if c.EndLine > c.StartLine {
		loc += fmt.Sprintf("-%d", c.EndLine)
	}
	return loc
}
