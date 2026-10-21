// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cmscout/pkg/analysis"
	"cmscout/pkg/extract"
	"cmscout/pkg/ir"
	"cmscout/pkg/metrics"
	"cmscout/pkg/report"
	"cmscout/pkg/source"
)

type metrics_flags struct {
	scan_flags
	flags      *flag.FlagSet
	enabled    bool
	format     string
	name       string
	stdin_name string
	explain    bool
	staged     bool
	worktree   bool
	revision   string
	base       string
	parent     int
}

// register also keeps the metrics flags in their own set, so callers can reject them outside --metrics.
func (mf *metrics_flags) register(fs *flag.FlagSet) {
	mf.flags = flag.NewFlagSet("metrics", flag.ContinueOnError)
	mf.flags.BoolVar(&mf.enabled, "metrics", false, "measure one source file, or compare two files")
	mf.flags.StringVar(&mf.format, "format", "text", "metrics output format: text or json")
	mf.scan_flags.register(mf.flags)
	mf.flags.StringVar(&mf.name, "name", "", "logical name for a single metrics input")
	mf.flags.StringVar(&mf.stdin_name, "stdin-name", "", "logical filename and language for metrics read from stdin")
	mf.flags.BoolVar(&mf.explain, "explain", false, "include the decisions contributing to complexity")
	mf.flags.BoolVar(&mf.staged, "staged", false, "compare HEAD with the index")
	mf.flags.BoolVar(&mf.worktree, "worktree", false, "compare the index with tracked working files")
	mf.flags.StringVar(&mf.revision, "revision", "", "compare a commit with its parent or --base")
	mf.flags.StringVar(&mf.base, "base", "", "base commit for --revision")
	mf.flags.IntVar(&mf.parent, "parent", 0, "parent number for --revision, starting at 1")
	mf.flags.VisitAll(func(option *flag.Flag) { fs.Var(option.Value, option.Name, option.Usage) })
}

type metrics_error struct{ cause error }

func (err metrics_error) Error() string { return err.cause.Error() }
func (err metrics_error) Unwrap() error { return err.cause }

func exit_code(err error) int {
	if err == nil {
		return 0
	}
	var metric_error metrics_error
	if errors.As(err, &metric_error) {
		return 2
	}
	return 1
}

func (mf metrics_flags) run(fs *flag.FlagSet, cf compareFlags, stdin io.Reader, stdout io.Writer) error {
	paths := fs.Args()
	mode, err := mf.input_mode(paths, cf)
	if err != nil {
		return err
	}
	if err := mf.validate_flags(fs, mode); err != nil {
		return err
	}
	switch mode {
	case "staged", "worktree", "revision":
		if len(paths) != 0 {
			return fmt.Errorf("Git metrics use repository paths; use --root to select a repository and --include/--exclude to select files")
		}
		return mf.git_files(mode, cf, stdout)
	case "scan":
		return mf.scan_files(paths, stdout)
	case "pair":
		return mf.compare(cf, paths, stdin, stdout)
	}
	return mf.single_file(paths, stdin, stdout)
}

func (mf metrics_flags) single_file(paths []string, stdin io.Reader, stdout io.Writer) error {
	if len(paths) != 1 {
		return fmt.Errorf("usage: cmscout --metrics [flags] <file>")
	}
	path, logical_name := paths[0], paths[0]
	if mf.name != "" {
		logical_name = mf.name
	}
	if path == "-" {
		if mf.stdin_name == "" || mf.name != "" {
			return fmt.Errorf("stdin metrics require --stdin-name <file> and no --name")
		}
		logical_name = mf.stdin_name
	} else {
		if mf.stdin_name != "" {
			return fmt.Errorf("--stdin-name requires '-' as the input")
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("directory metrics require --scan without a logical filename override: %s", path)
		}
	}
	options, err := metric_context(logical_name, mf.root, mf.name != "" || path == "-")
	if err != nil {
		return err
	}
	options.Explain = mf.explain
	source, err := loadFile(path, stdin)
	if err != nil {
		return err
	}
	snapshot, err := measure_source([]byte(source), options)
	if err != nil {
		return err
	}
	if mf.format == "json" {
		err = report.WriteJSON(stdout, snapshot)
	} else {
		err = report.WriteMetricsText(stdout, snapshot)
	}
	if err != nil {
		return err
	}
	if snapshot.Status != "complete" {
		return fmt.Errorf("metrics incomplete for %s; see report diagnostics", options.Path)
	}
	return nil
}

func (mf metrics_flags) compare(cf compareFlags, paths []string, stdin io.Reader, stdout io.Writer) error {
	old_virtual, new_virtual := cf.oldName != "", cf.newName != ""
	old_path, new_path, err := cf.resolve(paths)
	if err != nil {
		return err
	}
	old_options, err := metric_context(cf.oldName, mf.root, old_virtual)
	if err != nil {
		return err
	}
	new_options, err := metric_context(cf.newName, mf.root, new_virtual)
	if err != nil {
		return err
	}
	old_options.Explain, new_options.Explain = mf.explain, mf.explain
	old_source, new_source, err := loadPair(old_path, new_path, stdin)
	if err != nil {
		return err
	}
	separate_macros := separate_macro_functions(old_options.Path, new_options.Path)
	before, old_document, err := measure_for_diff(old_source, old_options, separate_macros)
	if err != nil {
		return fmt.Errorf("before metrics: %w", err)
	}
	after, new_document, err := measure_for_diff(new_source, new_options, separate_macros)
	if err != nil {
		return fmt.Errorf("after metrics: %w", err)
	}
	comparison, err := metrics.Compare(before, after)
	if err != nil {
		return err
	}
	if comparison.RangePrecision != "unavailable" {
		comparison.Diff, err = render_metrics_diff(cf, [2]string{old_options.Path, new_options.Path},
			[2]string{old_source, new_source}, [2]*ir.SemanticDocument{old_document, new_document})
		if err != nil {
			return err
		}
	}
	if mf.format == "json" {
		err = report.WriteJSON(stdout, comparison)
	} else {
		err = report.WriteComparisonText(stdout, comparison)
	}
	if err != nil {
		return err
	}
	if comparison.Status != "complete" {
		return fmt.Errorf("metrics comparison incomplete; see report diagnostics")
	}
	return nil
}

func render_metrics_diff(cf compareFlags, names, contents [2]string, documents [2]*ir.SemanticDocument) (*metrics.RenderedDiff, error) {
	var rendered bytes.Buffer
	cf.oldName, cf.newName = names[0], names[1]
	err := runSemanticReviewDocuments(cf, report.Options{NoColor: true, IgnoreSpace: cf.ignoreSpace},
		contents[0], contents[1], documents[0], documents[1], &rendered)
	if err != nil {
		return nil, err
	}
	return &metrics.RenderedDiff{Format: "cmscout-text-v1", Text: rendered.String()}, nil
}

func measure_for_diff(source string, options metrics.Options, separate_macros bool) (*metrics.Snapshot, *ir.SemanticDocument, error) {
	ast, err := analysis.Parse([]byte(source), options.Path)
	if err != nil {
		return nil, nil, err
	}
	defer ast.Close()
	snapshot, err := metrics.Measure(ast, options)
	if err != nil {
		return nil, nil, err
	}
	document, err := extract.NewWithOptions(options.Path, extract.Options{SeparateMacroFunctions: separate_macros}).Extract(ast)
	return snapshot, document, err
}

func metric_context(name, root string, virtual bool) (metrics.Options, error) {
	if root != "" {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return metrics.Options{}, err
		}
		root = absolute
		info, err := os.Stat(root)
		if err != nil {
			return metrics.Options{}, err
		}
		if !info.IsDir() {
			return metrics.Options{}, fmt.Errorf("metrics root must be a directory: %s", root)
		}
		if virtual && !filepath.IsAbs(name) {
			name = filepath.Join(root, name)
		}
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return metrics.Options{}, err
	}
	if root == "" {
		root = source.InferRoot(filepath.Dir(absolute))
	}
	logical, err := filepath.Rel(root, absolute)
	if err != nil {
		return metrics.Options{}, err
	}
	if logical == ".." || strings.HasPrefix(logical, ".."+string(filepath.Separator)) || logical == "." {
		return metrics.Options{}, fmt.Errorf("logical metrics input %s is outside source root %s", name, root)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(root)))
	namespace := fmt.Sprintf("%s@%x", filepath.Base(root), digest[:8])
	return metrics.Options{Namespace: namespace, Path: filepath.ToSlash(logical)}, nil
}
