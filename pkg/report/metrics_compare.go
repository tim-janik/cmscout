package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"cmscout/pkg/metrics"
)

func WriteComparisonJSON(w io.Writer, comparison *metrics.Comparison) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(comparison)
}

func WriteComparisonText(w io.Writer, comparison *metrics.Comparison) error {
	output := bufio.NewWriter(w)
	fmt.Fprintln(output, "Before")
	if err := WriteMetricsText(output, comparison.Before); err != nil {
		return err
	}
	fmt.Fprintln(output, "\nAfter")
	if err := WriteMetricsText(output, comparison.After); err != nil {
		return err
	}
	fmt.Fprintf(output, "\nTouched components (%s)\n", comparison.Status)
	for _, change := range comparison.Changes {
		before, after := "absent", "absent"
		if change.BeforeName != nil {
			before = *change.BeforeName
		}
		if change.AfterName != nil {
			after = *change.AfterName
		}
		fmt.Fprintf(output, "  %s -> %s\n", before, after)
		flags := []string{}
		for _, flag := range []struct {
			name string
			set  bool
		}{
			{"added", change.Added}, {"removed", change.Removed}, {"renamed", change.Renamed}, {"moved", change.Moved},
			{"kind", change.KindChanged}, {"name", change.NameChanged}, {"direct", change.DirectChanged},
			{"descendant", change.DescendantChanged}, {"code", change.CodeChanged}, {"signature", change.SignatureChanged},
			{"prefix", change.PrefixChanged}, {"inline", change.InlineChanged}, {"formatting", change.FormattingChanged},
		} {
			if flag.set {
				flags = append(flags, flag.name)
			}
		}
		fmt.Fprintf(output, "    %s; match=%s; delta=%s\n", strings.Join(flags, ", "), change.Match.Status, change.Delta.Status)
		fmt.Fprintf(output, "    delta cyclomatic=%s lines=%s chars=%s prefix_chars=%s inline_chars=%s\n",
			metric_number(change.Delta.Cyclomatic), metric_number(change.Delta.Lines), metric_number(change.Delta.Chars),
			metric_number(change.Delta.PrefixChars), metric_number(change.Delta.InlineChars))
	}
	for _, diagnostic := range comparison.Diagnostics {
		fmt.Fprintf(output, "%s: %s\n", diagnostic.Kind, diagnostic.Message)
	}
	if comparison.Diff != nil {
		fmt.Fprintln(output)
		fmt.Fprint(output, comparison.Diff.Text)
	}
	return output.Flush()
}
