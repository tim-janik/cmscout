// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package report

import (
	"bufio"
	"fmt"
	"io"

	"cmscout/pkg/metrics"
)

func WriteScanText(w io.Writer, scan *metrics.ScanReport) error {
	output := bufio.NewWriter(w)
	fmt.Fprintf(output, "Source scan (%s): %d files, root %s\n", scan.Status, len(scan.Files), DisplayPath(scan.Root))
	for _, snapshot := range scan.Files {
		fmt.Fprintln(output)
		if err := WriteMetricsText(output, snapshot); err != nil {
			return err
		}
	}
	for _, skipped := range scan.Skipped {
		fmt.Fprintf(output, "skipped %s: %s\n", DisplayPath(skipped.Path), DisplayPath(skipped.Message))
	}
	for _, diagnostic := range scan.Diagnostics {
		fmt.Fprintf(output, "%s %s: %s\n", diagnostic.Kind, DisplayPath(diagnostic.Path), DisplayPath(diagnostic.Message))
	}
	return output.Flush()
}
