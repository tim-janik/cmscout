package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"cmscout/pkg/metrics"
)

func WriteMetricsJSON(w io.Writer, snapshot *metrics.Snapshot) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func WriteMetricsText(w io.Writer, snapshot *metrics.Snapshot) error {
	output := bufio.NewWriter(w)
	fmt.Fprintf(output, "metrics %s (%s; functions; %s)\n", snapshot.Path, snapshot.Language, snapshot.Status)
	fmt.Fprintf(output, "profiles: %s\n", strings.Join(snapshot.Profiles, ", "))
	for _, limitation := range snapshot.Limitations {
		fmt.Fprintf(output, "scope: %s\n", limitation)
	}
	count := 0
	for _, component := range snapshot.Components {
		if component.FunctionMetrics == nil {
			continue
		}
		count++
		fmt.Fprintf(output, "\n%s\n", component.QualifiedName)
		fmt.Fprintf(output, "  %s:%d:%d  cyclomatic=%s (%s)  lines=%d chars=%s\n",
			snapshot.Path, component.Span.StartLine+1, component.Span.StartCol+1,
			metric_number(component.Cyclomatic.Value), component.Cyclomatic.Status, component.Size.Lines, metric_number(component.Size.Chars))
		fmt.Fprintf(output, "  prefix_comment: %s\n", comment_size(component.PrefixComment))
		if component.InlineComments == nil {
			fmt.Fprintln(output, "  inline_comments: unknown")
		} else {
			entries := make([]string, 0, len(component.InlineComments))
			for _, comment := range component.InlineComments {
				entries = append(entries, comment_size(&comment))
			}
			fmt.Fprintf(output, "  inline_comments: [%s]\n", strings.Join(entries, ", "))
		}
		for _, name := range component.InnerFunctions {
			fmt.Fprintf(output, "  inner_function: %s\n", name)
		}
		for _, event := range component.Cyclomatic.Decisions {
			fmt.Fprintf(output, "  +%d %s at %s:%d:%d\n", event.Contribution, event.Rule,
				snapshot.Path, event.Span.StartLine+1, event.Span.StartCol+1)
		}
	}
	fmt.Fprintf(output, "\n%d callable definitions\n", count)
	for _, diagnostic := range snapshot.Diagnostics {
		if diagnostic.Span == nil {
			fmt.Fprintf(output, "%s: %s\n", diagnostic.Kind, diagnostic.Message)
		} else {
			fmt.Fprintf(output, "%s:%d:%d: %s: %s\n", snapshot.Path, diagnostic.Span.StartLine+1,
				diagnostic.Span.StartCol+1, diagnostic.Kind, diagnostic.Message)
		}
	}
	return output.Flush()
}

func metric_number(value *int) string {
	if value == nil {
		return "unknown"
	}
	return strconv.Itoa(*value)
}

func comment_size(comment *metrics.Comment) string {
	if comment == nil {
		return "unknown"
	}
	return fmt.Sprintf("(chars=%s, lines=%d)", metric_number(comment.Chars), comment.Lines)
}
