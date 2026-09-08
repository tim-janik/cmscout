package main

import (
	"bytes"
	"fmt"
	"io"

	"cmscout/pkg/ir"
	"cmscout/pkg/lang"
	"cmscout/pkg/metrics"
	"cmscout/pkg/report"
	"cmscout/pkg/source"
)

func (mf metrics_flags) git_files(cf compareFlags, stdout io.Writer) error {
	mode := "revision"
	if mf.staged {
		mode = "staged"
	} else if mf.worktree {
		mode = "worktree"
	}
	files, err := source.Git(source.GitOptions{Directory: mf.root, Mode: mode, Revision: mf.revision,
		Base: mf.base, Parent: mf.parent, Filter: source.Filter{Include: mf.include, Exclude: mf.exclude}})
	if err != nil {
		return err
	}
	result := &metrics.ChangeSet{SchemaVersion: "1", Kind: "change_set", Status: "complete", Root: files.Root,
		Population: "changed_files", Input: files.Input, Files: []metrics.FileComparison{}, Skipped: files.Skipped, Diagnostics: files.Diagnostics}
	for _, file := range files.Files {
		comparison, err := mf.compare_git_file(cf, files.Root, file)
		if err != nil {
			path := file.Before
			if file.After != nil {
				path = file.After
			}
			result.Diagnostics = append(result.Diagnostics, source.Notice{Path: path.Path, Kind: "analysis_error", Message: err.Error()})
			continue
		}
		result.Files = append(result.Files, metrics.FileComparison{Change: file.Change, Comparison: comparison})
		if comparison.Status != "complete" {
			result.Status = "partial"
		}
	}
	if len(result.Diagnostics) > 0 {
		result.Status = "partial"
	}
	if mf.format == "json" {
		err = report.WriteChangeSetJSON(stdout, result)
	} else {
		err = report.WriteChangeSetText(stdout, result)
	}
	if err != nil {
		return err
	}
	if result.Status != "complete" {
		return fmt.Errorf("metrics change set incomplete; see report diagnostics")
	}
	return nil
}

func (mf metrics_flags) compare_git_file(cf compareFlags, root string, pair source.FilePair) (*metrics.Comparison, error) {
	separate_macros := true
	for _, file := range []*source.File{pair.Before, pair.After} {
		if file != nil {
			language, known := lang.Detect(file.Path)
			separate_macros = separate_macros && known && lang.HasMacroFunctions(language)
		}
	}
	var snapshots [2]*metrics.Snapshot
	var documents [2]*ir.SemanticDocument
	var contents [2]string
	names := [2]string{"/dev/null", "/dev/null"}
	for i, file := range []*source.File{pair.Before, pair.After} {
		if file == nil {
			continue
		}
		options, err := metric_context(file.Path, root, true)
		if err != nil {
			return nil, err
		}
		options.Explain = mf.explain
		contents[i], names[i] = string(file.Content), file.Path
		snapshots[i], documents[i], err = measure_for_diff(contents[i], options, separate_macros)
		if err != nil {
			return nil, err
		}
	}
	comparison, err := metrics.CompareFiles(snapshots[0], snapshots[1])
	if err != nil {
		return nil, err
	}
	var rendered bytes.Buffer
	cf.oldName, cf.newName, cf.noColor = names[0], names[1], true
	err = runSemanticReviewDocuments(cf, false, false, "white", "white", contents[0], contents[1], documents[0], documents[1], &rendered)
	if err != nil {
		return nil, err
	}
	comparison.Diff = &metrics.RenderedDiff{Format: "cmscout-text-v1", Text: rendered.String()}
	return comparison, nil
}
