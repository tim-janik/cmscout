// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cmscout/pkg/analysis"
	"cmscout/pkg/metrics"
	"cmscout/pkg/report"
	"cmscout/pkg/source"
)

func (mf metrics_flags) scan_files(paths []string, stdout io.Writer) error {
	files, err := source.Scan(paths, mf.root, source.Filter{Include: mf.include, Exclude: mf.exclude})
	if err != nil {
		return err
	}
	result := &metrics.ScanReport{
		SchemaVersion: "1", Kind: "scan", Status: "complete", Root: files.Root, Population: "selected_files",
		Files: []*metrics.Snapshot{}, Skipped: files.Skipped, Diagnostics: files.Diagnostics,
	}
	for _, name := range files.Files {
		options, err := metric_context(name, files.Root, true)
		if err != nil {
			return err
		}
		options.Explain = mf.explain
		content, err := os.ReadFile(filepath.Join(files.Root, filepath.FromSlash(name)))
		if err == nil {
			err = source.CheckText(content)
		}
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, source.Notice{Path: name, Kind: "read_error", Message: err.Error()})
			continue
		}
		snapshot, err := measure_source(content, options)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, source.Notice{Path: name, Kind: "analysis_error", Message: err.Error()})
			continue
		}
		snapshot.DiscardComparisonData()
		result.Files = append(result.Files, snapshot)
		if snapshot.Status != "complete" {
			result.Status = "partial"
		}
	}
	if len(result.Diagnostics) > 0 {
		result.Status = "partial"
	}
	if mf.format == "json" {
		err = report.WriteScanJSON(stdout, result)
	} else {
		err = report.WriteScanText(stdout, result)
	}
	if err != nil {
		return err
	}
	if result.Status != "complete" {
		return fmt.Errorf("metrics scan incomplete; see report diagnostics")
	}
	return nil
}

func measure_source(content []byte, options metrics.Options) (*metrics.Snapshot, error) {
	ast, err := analysis.Parse(content, options.Path)
	if err != nil {
		return nil, err
	}
	defer ast.Close()
	return metrics.Measure(ast, options)
}
