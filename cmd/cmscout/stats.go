// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cmscout/pkg/extract"
	"cmscout/pkg/lang"
	"cmscout/pkg/parser"
	"cmscout/pkg/report"
	"cmscout/pkg/source"
	"cmscout/pkg/stats"
)

func run_stats_flags(fs *flag.FlagSet, cf compareFlags, sf scan_flags, limit int, stdin io.Reader, stdout io.Writer) error {
	if limit < 0 {
		return fmt.Errorf("--max-comment-lines must be nonnegative")
	}
	scanning := sf.selected(fs.Args(), cf.oldName != "" || cf.beforeFile != "")
	var invalid string
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "stats", "max-comment-lines", "no-color":
		case "old", "B", "before-contents":
			if scanning {
				invalid = f.Name
			}
		case "scan", "root", "include", "exclude":
			if !scanning {
				invalid = f.Name
			}
		default:
			invalid = f.Name
		}
	})
	if invalid != "" {
		return fmt.Errorf("--%s is not supported with --stats", invalid)
	}
	if scanning {
		return scan_stats(sf, fs.Args(), limit, stdout)
	}
	name, content := "", ""
	if len(fs.Args()) == 1 {
		name, content = fs.Args()[0], fs.Args()[0]
	} else if len(fs.Args()) > 1 {
		return fmt.Errorf("usage: cmscout --stats [flags] <file>")
	}
	if cf.oldName != "" {
		name = cf.oldName
	}
	if cf.beforeFile != "" {
		content = cf.beforeFile
	}
	if name == "" {
		return fmt.Errorf("usage: cmscout --stats [flags] <file>")
	}
	if content == "" {
		content = "-"
	}
	if name == "-" {
		return fmt.Errorf("--stats: stdin needs a language hint: use --old <name>")
	}
	src, err := loadFile(content, stdin)
	if err != nil {
		return err
	}
	return run_stats(src, name, limit, stdout)
}

func run_stats(src, path string, max_comment_lines int, stdout io.Writer) error {
	r, err := analyze_stats(src, path, max_comment_lines)
	if err != nil {
		return err
	}
	if err := stats.Render(stdout, r); err != nil {
		return err
	}
	if r.Status != "complete" {
		return metrics_error{fmt.Errorf("statistics incomplete for %s; see report diagnostics", path)}
	}
	return nil
}

func scan_stats(sf scan_flags, paths []string, limit int, stdout io.Writer) error {
	files, err := sf.files(paths)
	if err != nil {
		return metrics_error{err}
	}
	partial := len(files.Diagnostics) > 0
	if _, err := fmt.Fprintf(stdout, "# cmscout stats scan: %s\n", report.DisplayPath(files.Root)); err != nil {
		return err
	}
	for _, name := range files.Files {
		content, err := os.ReadFile(filepath.Join(files.Root, filepath.FromSlash(name)))
		if err == nil {
			err = source.CheckText(content)
		}
		var r *stats.Report
		if err == nil {
			r, err = analyze_stats(string(content), name, limit)
		}
		if err != nil {
			partial = true
			if _, write_err := fmt.Fprintf(stdout, "# diagnostic analysis_error %s: %s\n", report.DisplayPath(name), report.DisplayPath(err.Error())); write_err != nil {
				return write_err
			}
			continue
		}
		if err := stats.Render(stdout, r); err != nil {
			return err
		}
		if r.Status != "complete" {
			partial = true
		}
	}
	for _, notice := range files.Skipped {
		if _, err := fmt.Fprintf(stdout, "# skipped %s %s: %s\n", notice.Kind, report.DisplayPath(notice.Path), report.DisplayPath(notice.Message)); err != nil {
			return err
		}
	}
	for _, notice := range files.Diagnostics {
		if _, err := fmt.Fprintf(stdout, "# diagnostic %s %s: %s\n", notice.Kind, report.DisplayPath(notice.Path), report.DisplayPath(notice.Message)); err != nil {
			return err
		}
	}
	if partial {
		return metrics_error{fmt.Errorf("statistics scan incomplete; see report diagnostics")}
	}
	return nil
}

func analyze_stats(src, path string, max_comment_lines int) (*stats.Report, error) {
	ast, err := parser.ParsePath([]byte(src), path)
	if err != nil {
		return nil, err
	}
	defer ast.Close()
	doc, err := extract.NewWithOptions(path, extract.Options{SeparateMacroFunctions: lang.HasMacroFunctions(ast.Language())}).Extract(ast)
	if err != nil {
		return nil, err
	}
	return stats.Analyze(doc, ast, stats.Options{MaxCommentLines: max_comment_lines})
}
