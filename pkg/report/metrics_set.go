package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"cmscout/pkg/metrics"
)

func WriteChangeSetJSON(w io.Writer, set *metrics.ChangeSet) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(set)
}

func WriteChangeSetText(w io.Writer, set *metrics.ChangeSet) error {
	output := bufio.NewWriter(w)
	fmt.Fprintf(output, "Changed files (%s): %s -> %s, %d file pairs\n", set.Status, set.Input.Before, set.Input.After, len(set.Files))
	fmt.Fprintln(output, "Population: changed files only")
	for _, file := range set.Files {
		fmt.Fprintf(output, "\nFile change: %s\n", file.Change)
		if err := WriteComparisonText(output, file.Comparison); err != nil {
			return err
		}
	}
	for _, skipped := range set.Skipped {
		fmt.Fprintf(output, "skipped %q: %s\n", skipped.Path, skipped.Message)
	}
	for _, diagnostic := range set.Diagnostics {
		fmt.Fprintf(output, "%s %q: %s\n", diagnostic.Kind, diagnostic.Path, diagnostic.Message)
	}
	return output.Flush()
}
