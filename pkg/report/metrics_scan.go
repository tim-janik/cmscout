package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"cmscout/pkg/metrics"
)

func WriteScanJSON(w io.Writer, scan *metrics.ScanReport) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(scan)
}

func WriteScanText(w io.Writer, scan *metrics.ScanReport) error {
	output := bufio.NewWriter(w)
	fmt.Fprintf(output, "Source scan (%s): %d files, root %q\n", scan.Status, len(scan.Files), scan.Root)
	for _, snapshot := range scan.Files {
		fmt.Fprintln(output)
		if err := WriteMetricsText(output, snapshot); err != nil {
			return err
		}
	}
	for _, skipped := range scan.Skipped {
		fmt.Fprintf(output, "skipped %q: %s\n", skipped.Path, skipped.Message)
	}
	for _, diagnostic := range scan.Diagnostics {
		fmt.Fprintf(output, "%s %q: %s\n", diagnostic.Kind, diagnostic.Path, diagnostic.Message)
	}
	return output.Flush()
}
